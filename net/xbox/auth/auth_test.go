package auth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// services stands in for the Microsoft account, Xbox, PlayFab and
// Minecraft services: it checks what the game would send and the signature
// of every request that needs one.
type services struct {
	t        *testing.T
	mu       sync.Mutex
	offset   time.Duration // the clock of the services runs this far ahead
	key      *ecdsa.PublicKey
	polls    int
	refresh  string
	refreshN int
	signed   int
	retries  int
}

func (sv *services) now() time.Time { return time.Now().Add(sv.offset) }

func (sv *services) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	w.Header().Set("Date", sv.now().UTC().Format(http.TimeFormat))
	body, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(body))
	reply := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	xbl := func(uhs string) map[string]any {
		return map[string]any{"Token": "token-" + r.URL.Path, "NotAfter": sv.now().Add(16 * time.Hour),
			"DisplayClaims": map[string]any{"xui": []map[string]any{{"uhs": uhs, "xid": "2535400000000001", "gtg": "Host"}}}}
	}
	switch r.URL.Path {
	case "/oauth20_connect.srf":
		if form.Get("client_id") != Android.ClientID || form.Get("scope") != scope || form.Get("response_type") != "device_code" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		reply(map[string]any{"user_code": "ABCD-EFGH", "device_code": "dc", "verification_uri": "https://www.microsoft.com/link", "expires_in": 900, "interval": 1})
	case "/oauth20_token.srf":
		switch form.Get("grant_type") {
		case "urn:ietf:params:oauth:grant-type:device_code":
			if sv.polls++; sv.polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				reply(map[string]any{"error": "authorization_pending"})
				return
			}
			sv.refresh = "refresh-1"
			reply(map[string]any{"access_token": "access", "refresh_token": sv.refresh, "expires_in": 3600})
		case "refresh_token":
			if form.Get("refresh_token") != sv.refresh {
				w.WriteHeader(http.StatusBadRequest)
				reply(map[string]any{"error": "invalid_grant"})
				return
			}
			sv.refreshN++
			sv.refresh = fmt.Sprintf("refresh-%d", sv.refreshN+1)
			reply(map[string]any{"access_token": "access", "refresh_token": sv.refresh, "expires_in": 3600})
		}
	case "/device/authenticate":
		var req struct{ Properties struct{ ProofKey proofKey } }
		_ = json.Unmarshal(body, &req)
		sv.key = publicKey(sv.t, req.Properties.ProofKey)
		if !sv.verify(w, r, body) {
			return
		}
		reply(map[string]any{"Token": "device", "NotAfter": sv.now().Add(14 * 24 * time.Hour)})
	case "/authorize":
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if !sv.verify(w, r, body) {
			return
		}
		if req["AccessToken"] != "t=access" || req["DeviceToken"] != "device" || req["RelyingParty"] != XboxLive {
			http.Error(w, "bad authorize", http.StatusBadRequest)
			return
		}
		reply(map[string]any{"UserToken": xbl("u"), "TitleToken": xbl("t"), "AuthorizationToken": xbl("uhs-xbl")})
	case "/xsts/authorize":
		var req struct {
			RelyingParty string
			Properties   struct {
				DeviceToken, TitleToken string
				UserTokens              []string
			}
		}
		_ = json.Unmarshal(body, &req)
		if !sv.verify(w, r, body) {
			return
		}
		if req.Properties.DeviceToken != "device" || len(req.Properties.UserTokens) != 1 || req.RelyingParty != PlayFab {
			http.Error(w, "bad xsts", http.StatusBadRequest)
			return
		}
		reply(xbl("uhs-pf"))
	case "/Client/LoginWithXbox":
		var req map[string]any
		_ = json.Unmarshal(body, &req)
		if req["XboxToken"] != "XBL3.0 x=uhs-pf;token-/xsts/authorize" || req["TitleId"] != "20CA2" {
			http.Error(w, "bad playfab", http.StatusBadRequest)
			return
		}
		reply(map[string]any{"code": 200, "status": "OK", "data": map[string]any{"SessionTicket": "ticket", "PlayFabId": "P1",
			"EntityToken": map[string]any{"TokenExpiration": sv.now().Add(24 * time.Hour)}}})
	case "/api/v1.0/session/start":
		var req struct {
			User struct{ Token, TokenType string }
		}
		_ = json.Unmarshal(body, &req)
		if req.User.Token != "ticket" || req.User.TokenType != "PlayFab" {
			http.Error(w, "bad session", http.StatusBadRequest)
			return
		}
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"pmid":"9f1c2b3a-0000-4000-8000-000000000001"}`))
		reply(map[string]any{"result": map[string]any{"authorizationHeader": "MCToken e30." + claims + ".c2ln", "validUntil": sv.now().Add(4 * time.Hour)}})
	case "/api/v1.0/multiplayer/session/start":
		var req struct{ PublicKey string }
		_ = json.Unmarshal(body, &req)
		der, err := base64.StdEncoding.DecodeString(req.PublicKey)
		if _, perr := x509.ParsePKIXPublicKey(der); err != nil || perr != nil || !strings.HasPrefix(r.Header.Get("Authorization"), "MCToken e30.") {
			http.Error(w, "bad multiplayer session", http.StatusBadRequest)
			return
		}
		claims := base64.RawURLEncoding.EncodeToString([]byte(`{"cpk":"` + req.PublicKey + `"}`))
		reply(map[string]any{"result": map[string]any{"signedToken": "e30." + claims + ".c2ln", "validUntil": sv.now().Add(time.Hour)}})
	default:
		http.NotFound(w, r)
	}
}

// verify checks the Signature of a request against the proof key, and that
// it was made at the time of the services.
func (sv *services) verify(w http.ResponseWriter, r *http.Request, body []byte) bool {
	if sv.key == nil {
		http.Error(w, "no proof key", http.StatusUnauthorized)
		return false
	}
	signedAt, err := VerifySignature(r, body, sv.key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return false
	}
	if d := sv.now().Sub(signedAt); d > 5*time.Minute || d < -5*time.Minute {
		sv.retries++
		http.Error(w, "clock skew", http.StatusUnauthorized)
		return false
	}
	sv.signed++
	return true
}

func publicKey(t *testing.T, k proofKey) *ecdsa.PublicKey {
	x, err1 := base64.RawURLEncoding.DecodeString(k.X)
	y, err2 := base64.RawURLEncoding.DecodeString(k.Y)
	if err1 != nil || err2 != nil || k.Crv != "P-256" || k.Alg != "ES256" || len(x) != 32 || len(y) != 32 {
		t.Errorf("proof key %+v", k)
		return nil
	}
	return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
}

func testConfig(srv *httptest.Server) Config {
	conf := Android
	conf.Endpoints = Endpoints{srv.URL, srv.URL, srv.URL, srv.URL, srv.URL, srv.URL}
	conf.HTTPClient = srv.Client()
	return conf
}

// TestSignIn signs an account in the way of the game, against services
// whose clock runs ten minutes ahead: the signed requests are retried at
// their time, the MCToken carries the player messaging ID, and a restored
// Snapshot signs in again with the refresh token, which rotates.
func TestSignIn(t *testing.T) {
	sv := &services{t: t, offset: 10 * time.Minute}
	srv := httptest.NewTLSServer(sv)
	defer srv.Close()
	conf := testConfig(srv)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dc, err := conf.StartLogin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if dc.UserCode != "ABCD-EFGH" || !strings.HasPrefix(dc.VerificationURI, "https://") {
		t.Fatalf("device code %+v", dc)
	}
	s, err := dc.Wait(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mc, err := s.MCToken(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if mc.PlayerMessagingID != "9f1c2b3a-0000-4000-8000-000000000001" || !strings.HasPrefix(mc.Header, "MCToken ") {
		t.Fatalf("MCToken %+v", mc)
	}
	client, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	mp, err := s.MultiplayerToken(ctx, &client.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&client.PublicKey)
	if claims, _ := base64.RawURLEncoding.DecodeString(strings.Split(mp, ".")[1]); !strings.Contains(string(claims), base64.StdEncoding.EncodeToString(der)) {
		t.Fatalf("multiplayer token %q does not name the key of the client", mp)
	}
	x, err := s.XSTS(ctx, XboxLive)
	if err != nil || x.Header() != "XBL3.0 x=uhs-xbl;token-/authorize" || x.Gamertag != "Host" {
		t.Fatalf("Xbox Live token %+v, %v", x, err)
	}
	sv.mu.Lock()
	if sv.retries != 1 || sv.signed != 3 {
		t.Errorf("%d requests refused for their time, %d signed ones taken; want 1 and 3", sv.retries, sv.signed)
	}
	sv.mu.Unlock()

	snap := s.Snapshot()
	again, err := conf.Restore(snap)
	if err != nil {
		t.Fatal(err)
	}
	var saved Snapshot
	again.OnRefresh(func(s Snapshot) { saved = s })
	if _, err := again.MCToken(ctx); err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "refresh-2" || saved.ProofKey != snap.ProofKey || saved.DeviceID != snap.DeviceID {
		t.Fatalf("snapshot after the refresh %+v", saved)
	}
	if _, err := conf.Restore(snap); err != nil {
		t.Fatal(err)
	}
}

func TestRestoreRefusesBrokenSnapshots(t *testing.T) {
	for _, s := range []Snapshot{
		{},
		{RefreshToken: "r", DeviceID: "d", ProofKey: "not base64!"},
		{RefreshToken: "r", DeviceID: "d", ProofKey: base64.StdEncoding.EncodeToString([]byte("not a key"))},
	} {
		if _, err := Android.Restore(s); err == nil {
			t.Fatalf("restored %+v", s)
		}
	}
}

func TestPlayerMessagingID(t *testing.T) {
	for _, h := range []string{"Bearer x", "MCToken a.b", "MCToken a.!!!.c", "MCToken a." + base64.RawURLEncoding.EncodeToString([]byte(`{}`)) + ".c"} {
		if _, err := playerMessagingID(h); err == nil {
			t.Fatalf("read a pmid from %q", h)
		}
	}
}

// TestSignatureCoversRequest signs a request with a query and an
// Authorization header: it verifies, and changing any signed part breaks it.
func TestSignatureCoversRequest(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	body := []byte(`{"type":"activity"}`)
	req := httptest.NewRequest(http.MethodPost, "https://sessiondirectory.xboxlive.com/handles?include=relatedInfo", nil)
	req.Header.Set("Authorization", "XBL3.0 x=1;t")
	req.Header.Set("Signature", signature(req, body, key, time.Now()))
	if _, err := VerifySignature(req, body, &key.PublicKey); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(r *http.Request) ([]byte, *http.Request){
		func(r *http.Request) ([]byte, *http.Request) { return []byte(`{"type":"other"}`), r },
		func(r *http.Request) ([]byte, *http.Request) {
			r2 := r.Clone(r.Context())
			r2.Header.Set("Authorization", "XBL3.0 x=2;t")
			return body, r2
		},
		func(r *http.Request) ([]byte, *http.Request) {
			r2 := r.Clone(r.Context())
			r2.URL.RawQuery = ""
			return body, r2
		},
		func(r *http.Request) ([]byte, *http.Request) {
			r2 := r.Clone(r.Context())
			r2.Method = http.MethodPut
			return body, r2
		},
	} {
		b, r := change(req)
		if _, err := VerifySignature(r, b, &key.PublicKey); err == nil {
			t.Fatal("a changed request still verifies")
		}
	}
}
