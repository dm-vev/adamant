package login

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// authService is an authorization service of the game run by a test: it
// publishes an OpenID configuration and its keys, and signs tokens.
type authService struct {
	*httptest.Server
	key     *rsa.PrivateKey
	kid     string
	fetches atomic.Int32
}

func newAuthService(t *testing.T) *authService {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &authService{key: key, kid: "k1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                s.URL + "/",
			"jwks_uri":                              s.URL + "/.well-known/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/.well-known/keys", func(w http.ResponseWriter, r *http.Request) {
		s.fetches.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "use": "sig", "kid": s.kid, "x5t": strings.Repeat("A", 40),
			"n": b64.EncodeToString(s.key.N.Bytes()), "e": b64.EncodeToString(big.NewInt(int64(s.key.E)).Bytes()),
		}}})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// signRS256 signs claims with RS256 under a header with a key ID.
func signRS256(t *testing.T, key *rsa.PrivateKey, kid string, claims any) string {
	t.Helper()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"})
	payload, _ := json.Marshal(claims)
	signed := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + b64.EncodeToString(sig)
}

// connectionRequest joins a token, a chain and client data into a
// connection request as the game sends it.
func joinRequest(t *testing.T, token string, chain []string, clientData string) []byte {
	t.Helper()
	cert, _ := json.Marshal(map[string][]string{"chain": chain})
	head, _ := json.Marshal(map[string]any{"Certificate": string(cert), "AuthenticationType": 0, "Token": token})
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(head)))
	b = append(b, head...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(clientData)))
	return append(b, clientData...)
}

// xboxClaims returns the claims of a token the service signs for a player.
func (s *authService) claims(now time.Time, cpk string) map[string]any {
	return map[string]any{
		"iss": s.URL + "/", "aud": multiplayerAudience, "sub": "player",
		"exp": now.Add(time.Hour).Unix(), "nbf": now.Add(-time.Minute).Unix(), "iat": now.Unix(),
		"ipt": "PlayFab", "mid": "ABCDEF0123", "tid": "20CA2", "cpk": cpk, "xid": "2535400000000001", "xname": "Steve",
	}
}

func TestParseOffline(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	id := Identity{DisplayName: "Bot", Identity: NewUUID(), PlayFabID: "abc", PlayFabTitleID: "20CA2"}
	data := DefaultClientData("::1:19132", "Bot", "1.26.50")
	now := time.Now()
	req, err := EncodeOffline(id, data, key, now)
	if err != nil {
		t.Fatal(err)
	}
	l, err := Parse(context.Background(), req, nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if l.Identity != id || l.XboxLive || !l.Key.Equal(&key.PublicKey) {
		t.Fatalf("login %+v", l)
	}
	data.ServerAddress = "[::1]:19132"
	if l.ClientData.SkinID != data.SkinID || l.ClientData.ServerAddress != data.ServerAddress || l.ClientData.DeviceID != data.DeviceID {
		t.Fatalf("client data %+v", l.ClientData)
	}
	// With keys, a token the client signed is refused.
	s := newAuthService(t)
	keys, err := NewKeys(context.Background(), nil, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(context.Background(), req, keys, now); err == nil {
		t.Fatal("keys took a token the client signed")
	}
}

func TestParseXboxLive(t *testing.T) {
	s := newAuthService(t)
	keys, err := NewKeys(context.Background(), nil, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	cpk, _ := MarshalPublicKey(&client.PublicKey)
	now := time.Now()
	data, err := sign(client, cpk, DefaultClientData("127.0.0.1:19132", "Steve", "1.26.50"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := Parse(context.Background(), joinRequest(t, signRS256(t, s.key, s.kid, s.claims(now, cpk)), nil, data), keys, now)
	if err != nil {
		t.Fatal(err)
	}
	want := Identity{XUID: "2535400000000001", DisplayName: "Steve", Identity: xuidUUID("2535400000000001"), PlayFabID: "ABCDEF0123", PlayFabTitleID: "20CA2"}
	if !l.XboxLive || l.Identity != want || !l.Key.Equal(&client.PublicKey) {
		t.Fatalf("login %+v", l)
	}
	if s.fetches.Load() != 1 {
		t.Fatalf("keys fetched %d times", s.fetches.Load())
	}

	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	otherData, _ := sign(ecKey, cpk, DefaultClientData("127.0.0.1:19132", "Steve", "1.26.50"))
	with := func(change func(map[string]any)) string {
		c := s.claims(now, cpk)
		change(c)
		return signRS256(t, s.key, s.kid, c)
	}
	// none returns a token signed with no algorithm; hmacToken one signed
	// with the RSA key as a secret, as if RS256 were HS256.
	none := func() string {
		header, _ := json.Marshal(map[string]string{"alg": "none"})
		payload, _ := json.Marshal(s.claims(now, cpk))
		return b64.EncodeToString(header) + "." + b64.EncodeToString(payload) + "."
	}
	hmacToken := func() string {
		header, _ := json.Marshal(map[string]string{"alg": "HS256", "kid": s.kid})
		payload, _ := json.Marshal(s.claims(now, cpk))
		signed := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
		m := hmac.New(sha256.New, s.key.N.Bytes())
		m.Write([]byte(signed))
		return signed + "." + b64.EncodeToString(m.Sum(nil))
	}
	tampered := func() string {
		parts := strings.Split(signRS256(t, s.key, s.kid, s.claims(now, cpk)), ".")
		c := s.claims(now, cpk)
		c["xid"] = "2535400000000002"
		payload, _ := json.Marshal(c)
		return parts[0] + "." + b64.EncodeToString(payload) + "." + parts[2]
	}
	duplicate := func() string {
		c := s.claims(now, cpk)
		delete(c, "xid")
		payload, _ := json.Marshal(c)
		payload = append(payload[:len(payload)-1], `,"xid":"1","xid":"2"}`...)
		header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": s.kid})
		signed := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
		sum := sha256.Sum256([]byte(signed))
		sig, _ := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, sum[:])
		return signed + "." + b64.EncodeToString(sig)
	}
	for name, tc := range map[string]struct{ token, data string }{
		"other issuer":            {with(func(c map[string]any) { c["iss"] = "https://evil.example/" }), data},
		"issuer without slash":    {with(func(c map[string]any) { c["iss"] = s.URL }), data},
		"other audience":          {with(func(c map[string]any) { c["aud"] = "api://other" }), data},
		"expired":                 {with(func(c map[string]any) { c["exp"] = now.Add(-time.Second).Unix() }), data},
		"no expiry":               {with(func(c map[string]any) { delete(c, "exp") }), data},
		"expiry in a string":      {with(func(c map[string]any) { c["exp"] = "99999999999" }), data},
		"not valid yet":           {with(func(c map[string]any) { c["nbf"] = now.Add(2 * time.Minute).Unix() }), data},
		"issued in the future":    {with(func(c map[string]any) { c["iat"] = now.Add(2 * time.Minute).Unix() }), data},
		"signed by another key":   {signRS256(t, other, s.kid, s.claims(now, cpk)), data},
		"unknown key ID":          {signRS256(t, s.key, "k9", s.claims(now, cpk)), data},
		"no algorithm":            {none(), data},
		"HMAC with the RSA key":   {hmacToken(), data},
		"tampered payload":        {tampered(), data},
		"XUID named twice":        {duplicate(), data},
		"client data of other":    {signRS256(t, s.key, s.kid, s.claims(now, cpk)), otherData},
		"client key not ECDSA":    {with(func(c map[string]any) { c["cpk"] = "AAAA" }), data},
		"client data unsigned":    {signRS256(t, s.key, s.kid, s.claims(now, cpk)), strings.Join(strings.Split(data, ".")[:2], ".") + "."},
		"client data as RS256":    {signRS256(t, s.key, s.kid, s.claims(now, cpk)), signRS256(t, s.key, s.kid, map[string]any{})},
		"token of three segments": {"a.b", data},
	} {
		if _, err := Parse(context.Background(), joinRequest(t, tc.token, nil, tc.data), keys, now); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	// A clock behind the service by less than the leeway takes the token.
	for name, change := range map[string]func(map[string]any){
		"not valid for 30s": func(c map[string]any) { c["nbf"] = now.Add(30 * time.Second).Unix() },
		"issued in 30s":     func(c map[string]any) { c["iat"] = now.Add(30 * time.Second).Unix() },
		"audience list":     func(c map[string]any) { c["aud"] = []string{"api://other", multiplayerAudience} },
		"name in other case": func(c map[string]any) {
			// Names match only themselves: XID is not the XUID.
			c["XID"] = "1"
		},
	} {
		l, err := Parse(context.Background(), joinRequest(t, with(change), nil, data), keys, now)
		if err != nil || l.Identity.XUID != "2535400000000001" {
			t.Errorf("%s: %v, XUID %q", name, err, l.Identity.XUID)
		}
	}
	// Unknown key IDs do not fetch the keys while they are fresh.
	if n := s.fetches.Load(); n != 1 {
		t.Fatalf("keys fetched %d times", n)
	}
}

// TestKeysRotate signs with a key the service published after the keys
// were fetched: a token naming it fetches the keys again once they are
// old enough.
func TestKeysRotate(t *testing.T) {
	s := newAuthService(t)
	keys, err := NewKeys(context.Background(), nil, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	client, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	cpk, _ := MarshalPublicKey(&client.PublicKey)
	now := time.Now()
	data, _ := sign(client, cpk, DefaultClientData("127.0.0.1:19132", "Steve", "1.26.50"))
	parse := func() error {
		_, err := Parse(context.Background(), joinRequest(t, signRS256(t, s.key, s.kid, s.claims(now, cpk)), nil, data), keys, now)
		return err
	}
	if err := parse(); err != nil {
		t.Fatal(err)
	}
	s.key, _ = rsa.GenerateKey(rand.Reader, 2048)
	s.kid = "k2"
	if err := parse(); err == nil {
		t.Fatal("took a key fetched no more than a moment ago")
	}
	keys.mu.Lock()
	keys.fetched = keys.fetched.Add(-keyRefresh)
	keys.mu.Unlock()
	if err := parse(); err != nil {
		t.Fatalf("rotated key: %v", err)
	}
	if n := s.fetches.Load(); n != 2 {
		t.Fatalf("keys fetched %d times, want 2", n)
	}
}

func TestDiscover(t *testing.T) {
	s := newAuthService(t)
	mux := http.NewServeMux()
	mux.HandleFunc("/builds/1.26.50", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":{"serviceEnvironments":{"auth":{"prod":{"serviceUri":"` + s.URL + `","issuer":"` + s.URL + `","playfabTitleId":"20CA2"}}}}}`))
	})
	d := httptest.NewServer(mux)
	defer d.Close()
	old := discoveryURL
	discoveryURL = d.URL + "/builds/"
	defer func() { discoveryURL = old }()
	keys, err := Discover(context.Background(), nil, "1.26.50")
	if err != nil {
		t.Fatal(err)
	}
	if keys.issuer != s.URL+"/" || keys.jwksURL != s.URL+"/.well-known/keys" || len(keys.algs) != 1 || keys.algs[0] != rs256 {
		t.Fatalf("keys %+v", keys)
	}
	if _, err := Discover(context.Background(), nil, "0.0.0"); err == nil {
		t.Fatal("discovered a version the service does not know")
	}
}

// TestParseChain logs in with the certificate chains of older clients: one
// the client signed itself, and one of Xbox Live, which goes through the
// key of Mojang, played here by a key of the test.
func TestParseChain(t *testing.T) {
	client, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	mojang, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	xbox, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	old := mojangKey
	mojangKey = &mojang.PublicKey
	defer func() { mojangKey = old }()
	pub := func(k *ecdsa.PrivateKey) string { s, _ := MarshalPublicKey(&k.PublicKey); return s }
	now := time.Now()
	times := map[string]any{"exp": now.Add(time.Hour).Unix(), "nbf": now.Add(-time.Hour).Unix()}
	token := func(signer *ecdsa.PrivateKey, claims map[string]any) string {
		for k, v := range times {
			if _, ok := claims[k]; !ok {
				claims[k] = v
			}
		}
		s, err := sign(signer, pub(signer), claims)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	extra := map[string]any{"XUID": "2535400000000001", "identity": NewUUID(), "displayName": "Steve", "titleId": "896928775"}
	data, _ := sign(client, pub(client), DefaultClientData("127.0.0.1:19132", "Steve", "1.26.50"))
	chain := func(extra map[string]any, handOver *ecdsa.PrivateKey, issuer string) []string {
		return []string{
			token(client, map[string]any{"identityPublicKey": pub(handOver), "certificateAuthority": true}),
			token(mojang, map[string]any{"identityPublicKey": pub(xbox), "iss": issuer}),
			token(xbox, map[string]any{"extraData": extra, "identityPublicKey": pub(client), "iss": issuer}),
		}
	}

	l, err := Parse(context.Background(), joinRequest(t, "", chain(extra, mojang, "Mojang"), data), nil, now)
	if err != nil {
		t.Fatal(err)
	}
	if !l.XboxLive || l.Identity.XUID != "2535400000000001" || l.Identity.TitleID != "896928775" || !l.Key.Equal(&client.PublicKey) {
		t.Fatalf("login %+v", l)
	}
	self := []string{token(client, map[string]any{"extraData": map[string]any{"identity": NewUUID(), "displayName": "Bot"}, "identityPublicKey": pub(client)})}
	if l, err := Parse(context.Background(), joinRequest(t, "", self, data), nil, now); err != nil || l.XboxLive || l.Identity.DisplayName != "Bot" {
		t.Fatalf("self-signed chain: %+v, %v", l, err)
	}
	noXUID := map[string]any{"identity": NewUUID(), "displayName": "Steve"}
	expired := []string{token(client, map[string]any{"extraData": noXUID, "exp": now.Add(-2 * time.Minute).Unix()})}
	for name, c := range map[string][]string{
		"hands over to another key": chain(extra, xbox, "Mojang"),
		"Mojang without an XUID":    chain(noXUID, mojang, "Mojang"),
		"issued by another":         chain(extra, mojang, "Notch"),
		"of two tokens":             chain(extra, mojang, "Mojang")[:2],
		"expired":                   expired,
	} {
		if _, err := Parse(context.Background(), joinRequest(t, "", c, data), nil, now); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}

	// The token of Xbox Live does not name the title, the chain beside it
	// does.
	s := newAuthService(t)
	keys, err := NewKeys(context.Background(), nil, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	tok := signRS256(t, s.key, s.kid, s.claims(now, pub(client)))
	if l, err := Parse(context.Background(), joinRequest(t, tok, chain(extra, mojang, "Mojang"), data), keys, now); err != nil || l.Identity.TitleID != "896928775" {
		t.Fatalf("title of the chain beside the token: %+v, %v", l.Identity, err)
	}
	extra["XUID"] = "2535400000000009"
	if l, err := Parse(context.Background(), joinRequest(t, tok, chain(extra, mojang, "Mojang"), data), keys, now); err != nil || l.Identity.TitleID != "" {
		t.Fatalf("title of a chain of another player: %+v, %v", l.Identity, err)
	}
}

func TestParseRequest(t *testing.T) {
	head := []byte(`{"Certificate":"{\"chain\":[\"\"]}","AuthenticationType":2,"Token":"x"}`)
	part := func(b []byte, n uint32, data []byte) []byte {
		return append(binary.LittleEndian.AppendUint32(b, n), data...)
	}
	for name, b := range map[string][]byte{
		"negative client data length": part(part(nil, uint32(len(head)), head), 0xffffffff, nil),
		"client data past the end":    part(part(nil, uint32(len(head)), head), 10, []byte("x")),
		"bytes after the client data": append(part(part(nil, uint32(len(head)), head), 1, []byte("x")), 0),
		"negative certificate length": part(nil, 0x80000000, head),
		"certificate of an object":    part(part(nil, 25, []byte(`{"Certificate":{"a":1}}  `)), 0, nil),
		"guest":                       part(part(nil, 29, []byte(`{"AuthenticationType":1}     `)), 0, nil),
		"empty":                       nil,
	} {
		if _, err := parseRequest(b); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	// Older versions put the chain at the top.
	req, err := parseRequest(part(part(nil, 14, []byte(`{"chain":["a"]}`)[:14]), 0, nil))
	if err == nil || req.chain != nil {
		t.Fatalf("parsed a truncated certificate: %+v", req)
	}
	legacy := []byte(`{"chain":["a"]}`)
	if req, err := parseRequest(part(part(nil, uint32(len(legacy)), legacy), 1, []byte("d"))); err != nil || len(req.chain) != 1 || req.chain[0] != "a" || req.clientData != "d" {
		t.Fatalf("chain at the top: %+v, %v", req, err)
	}
}

func FuzzParse(f *testing.F) {
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	req, _ := EncodeOffline(Identity{DisplayName: "Bot", Identity: NewUUID()}, DefaultClientData("127.0.0.1:19132", "Bot", "1.26.50"), key, time.Now())
	f.Add(req)
	f.Add([]byte{4, 0, 0, 0, '{', '}', ' ', ' ', 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		_, _ = Parse(context.Background(), b, nil, time.Now())
	})
}
