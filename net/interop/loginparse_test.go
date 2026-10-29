package interop

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/dm-vev/adamant/net/bedrock/login"
	gtlogin "github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// serverLogin reads a connection request as netconn reads it: package
// login checks it, then the identity and the client data pass the rules of
// gophertunnel's types.
func serverLogin(req []byte, keys *login.Keys) (gtlogin.IdentityData, gtlogin.ClientData, gtlogin.AuthResult, error) {
	l, err := login.Parse(context.Background(), req, keys, time.Now())
	if err != nil {
		return gtlogin.IdentityData{}, gtlogin.ClientData{}, gtlogin.AuthResult{}, err
	}
	id := gtlogin.IdentityData{XUID: l.Identity.XUID, Identity: l.Identity.Identity, DisplayName: l.Identity.DisplayName,
		TitleID: l.Identity.TitleID, PlayFabID: l.Identity.PlayFabID, PlayFabTitleID: l.Identity.PlayFabTitleID}
	if err := id.Validate(); err != nil {
		return gtlogin.IdentityData{}, gtlogin.ClientData{}, gtlogin.AuthResult{}, err
	}
	var c gtlogin.ClientData
	if err := json.Unmarshal(l.ClientDataJSON, &c); err != nil {
		return gtlogin.IdentityData{}, gtlogin.ClientData{}, gtlogin.AuthResult{}, err
	}
	c.ServerAddress = l.ClientData.ServerAddress
	if err := c.Validate(); err != nil {
		return gtlogin.IdentityData{}, gtlogin.ClientData{}, gtlogin.AuthResult{}, err
	}
	return id, c, gtlogin.AuthResult{PublicKey: l.Key, XBOXLiveAuthenticated: l.XboxLive}, nil
}

// sameLogin compares what both read of a request: whether they took it,
// and if so what they read.
func sameLogin(t *testing.T, name string, req []byte, keys *login.Keys, verifier *oidc.IDTokenVerifier, randomUUID bool) bool {
	t.Helper()
	gtID, gtData, gtAuth, gtErr := gtlogin.Parse(req, verifier)
	id, data, auth, err := serverLogin(req, keys)
	if (gtErr == nil) != (err == nil) {
		t.Errorf("%s: gophertunnel: %v; ours: %v", name, gtErr, err)
		return false
	}
	if err != nil {
		return false
	}
	if gtID.DisplayName != id.DisplayName || gtID.XUID != id.XUID || gtID.TitleID != id.TitleID ||
		gtID.PlayFabID != id.PlayFabID || gtID.PlayFabTitleID != id.PlayFabTitleID {
		t.Errorf("%s: identity %+v, gophertunnel %+v", name, id, gtID)
	}
	// Both make a random UUID for a token that names neither a UUID nor
	// an XUID.
	if gtID.Identity != id.Identity && !randomUUID {
		t.Errorf("%s: UUID %s, gophertunnel %s", name, id.Identity, gtID.Identity)
	}
	if gtAuth.XBOXLiveAuthenticated != auth.XBOXLiveAuthenticated || !gtAuth.PublicKey.Equal(auth.PublicKey) {
		t.Errorf("%s: authentication %+v, gophertunnel %+v", name, auth, gtAuth)
	}
	if !reflect.DeepEqual(gtData, data) {
		t.Errorf("%s: client data differs:\n ours %+v\n   gt %+v", name, data, gtData)
	}
	return true
}

// gtClientData returns client data of gophertunnel's type for a device and
// a skin of the game.
func gtClientData(t *testing.T, address, name string) gtlogin.ClientData {
	t.Helper()
	b, _ := json.Marshal(login.DefaultClientData(address, name, "1.26.50"))
	var c gtlogin.ClientData
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestLoginParseMatchesGophertunnel has both read the requests of offline
// clients, of gophertunnel's encoding in both forms and of ours.
func TestLoginParseMatchesGophertunnel(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	took := 0
	for _, id := range []gtlogin.IdentityData{
		{DisplayName: "Steve", Identity: login.NewUUID()},
		{DisplayName: "Steve Two", XUID: "2535400000000001", Identity: login.NewUUID()},
		{DisplayName: "Alex", XUID: "2535400000000002"},
		{DisplayName: "Alex"},
		{DisplayName: " Spaced", Identity: login.NewUUID()},
		{DisplayName: "1digit", Identity: login.NewUUID()},
		{DisplayName: "Steve", Identity: "not a UUID"},
		{DisplayName: "Steve", XUID: "x123", Identity: login.NewUUID()},
		{DisplayName: "", Identity: login.NewUUID()},
	} {
		for _, address := range []string{"127.0.0.1:19132", "::1:19132", "[::1]:19132", "example.invalid"} {
			data := gtClientData(t, address, id.DisplayName)
			for _, legacy := range []bool{false, true} {
				req := gtlogin.EncodeOffline(id, data, key, legacy)
				random := id.Identity == "" && id.XUID == "" && !legacy
				if sameLogin(t, fmt.Sprintf("%q at %s legacy=%v", id.DisplayName, address, legacy), req, nil, nil, random) {
					took++
				}
			}
			ours, err := login.EncodeOffline(login.Identity{DisplayName: id.DisplayName, Identity: id.Identity, XUID: id.XUID},
				login.DefaultClientData(address, id.DisplayName, "1.26.50"), key, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if sameLogin(t, fmt.Sprintf("%q at %s of ours", id.DisplayName, address), ours, nil, nil, id.Identity == "" && id.XUID == "") {
				took++
			}
		}
	}
	if took == 0 {
		t.Fatal("no request was taken")
	}
}

// TestVerifiedLoginMatchesGophertunnel has both verify tokens signed by an
// authorization service of the test: gophertunnel with an OpenID verifier
// of its key, ours with the keys the service publishes.
func TestVerifiedLoginMatchesGophertunnel(t *testing.T) {
	signer, _ := rsa.GenerateKey(rand.Reader, 2048)
	var srv *httptest.Server
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL + "/", "jwks_uri": srv.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{"kty": "RSA", "use": "sig", "kid": "k1",
			"n": base64.RawURLEncoding.EncodeToString(signer.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(signer.E)).Bytes())}}})
	})
	srv = httptest.NewServer(mux)
	defer srv.Close()
	keys, err := login.NewKeys(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	verifier := oidc.NewVerifier(srv.URL+"/", &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&signer.PublicKey}},
		&oidc.Config{ClientID: "api://auth-minecraft-services/multiplayer", SupportedSigningAlgs: []string{"RS256"}})

	client, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&client.PublicKey)
	cpk := base64.StdEncoding.EncodeToString(der)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	now := time.Now()
	claims := func(change func(map[string]any)) map[string]any {
		c := map[string]any{"iss": srv.URL + "/", "aud": "api://auth-minecraft-services/multiplayer", "exp": now.Add(time.Hour).Unix(),
			"nbf": now.Add(-time.Minute).Unix(), "iat": now.Unix(), "ipt": "PlayFab", "mid": "abcdef0123", "tid": "20CA2",
			"cpk": cpk, "xid": "2535400000000001", "xname": "Steve"}
		if change != nil {
			change(c)
		}
		return c
	}
	data := gtClientData(t, "127.0.0.1:19132", "Steve")
	took := 0
	for name, token := range map[string]string{
		"valid":                rs256(signer, "k1", claims(nil)),
		"expired":              rs256(signer, "k1", claims(func(c map[string]any) { c["exp"] = now.Add(-10 * time.Second).Unix() })),
		"no expiry":            rs256(signer, "k1", claims(func(c map[string]any) { delete(c, "exp") })),
		"valid in 30s":         rs256(signer, "k1", claims(func(c map[string]any) { c["nbf"] = now.Add(30 * time.Second).Unix() })),
		"valid in 2m":          rs256(signer, "k1", claims(func(c map[string]any) { c["nbf"] = now.Add(2 * time.Minute).Unix() })),
		"issued in 2m":         rs256(signer, "k1", claims(func(c map[string]any) { c["iat"] = now.Add(2 * time.Minute).Unix() })),
		"other audience":       rs256(signer, "k1", claims(func(c map[string]any) { c["aud"] = "api://other" })),
		"audiences":            rs256(signer, "k1", claims(func(c map[string]any) { c["aud"] = []string{"x", "api://auth-minecraft-services/multiplayer"} })),
		"other issuer":         rs256(signer, "k1", claims(func(c map[string]any) { c["iss"] = "https://evil.example/" })),
		"no XUID":              rs256(signer, "k1", claims(func(c map[string]any) { delete(c, "xid") })),
		"signed by another":    rs256(other, "k1", claims(nil)),
		"client key not ECDSA": rs256(signer, "k1", claims(func(c map[string]any) { c["cpk"] = "AAAA" })),
	} {
		clientData, _ := es384(client, cpk, data)
		cert, _ := json.Marshal(map[string][]string{"chain": {""}})
		head, _ := json.Marshal(map[string]any{"Certificate": string(cert), "AuthenticationType": 0, "Token": token})
		req := binary.LittleEndian.AppendUint32(nil, uint32(len(head)))
		req = append(req, head...)
		req = binary.LittleEndian.AppendUint32(req, uint32(len(clientData)))
		req = append(req, clientData...)
		// A token without an XUID names no UUID either: both make one.
		if sameLogin(t, name, req, keys, verifier, name == "no XUID") {
			took++
		}
	}
	if took < 3 {
		t.Fatalf("%d tokens taken", took)
	}
}

func rs256(key *rsa.PrivateKey, kid string, claims any) string {
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sum := sha256.Sum256([]byte(signed))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// es384 signs claims with ES384 under a header naming the key in x5u, as
// clients sign their client data.
func es384(key *ecdsa.PrivateKey, x5u string, claims any) (string, error) {
	header, _ := json.Marshal(map[string]string{"alg": "ES384", "x5u": x5u})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sum := sha512.Sum384([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 96)
	r.FillBytes(sig[:48])
	s.FillBytes(sig[48:])
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
