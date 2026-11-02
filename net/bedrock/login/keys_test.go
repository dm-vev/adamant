package login

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKeysReadyBeforeLogin(t *testing.T) {
	s := newAuthService(t)
	keys, err := NewKeys(context.Background(), nil, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	token := signRS256(t, s.key, s.kid, s.claims(now, "client-key"))
	s.Close()
	if _, err := keys.verify(context.Background(), token, now); err != nil {
		t.Fatalf("login needs the authorization service after initialization: %v", err)
	}
}

func TestNewKeysFetchFailure(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "unavailable"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/.well-known/openid-configuration" {
					_, _ = w.Write([]byte(`{"jwks_uri":"http://` + r.Host + `/keys"}`))
					return
				}
				if cancelled {
					cancel()
					<-r.Context().Done()
					return
				}
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
			}))
			defer s.Close()
			keys, err := NewKeys(ctx, nil, s.URL)
			if err == nil || keys != nil {
				t.Fatalf("returned uninitialized keys: %v, %v", keys, err)
			}
			if !strings.Contains(err.Error(), "keys of the authorization service") {
				t.Fatalf("missing fetch context: %v", err)
			}
			if cancelled && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
			}
		})
	}
}

// TestVerifyIssued vouches for a token the authorization service signed
// for any audience, as the identity token of a NetherNet offer, whose
// audience is not confirmed, and not for one of another issuer, signed by
// another key or by the client, or out of its time; a Login still asks for
// the multiplayer audience.
func TestVerifyIssued(t *testing.T) {
	s := newAuthService(t)
	keys, err := NewKeys(context.Background(), nil, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	with := func(change func(map[string]any)) string {
		c := s.claims(now, "client-key")
		change(c)
		return signRS256(t, s.key, s.kid, c)
	}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	client, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	selfSigned, _ := sign(client, "client-key", s.claims(now, "client-key"))
	for name, tc := range map[string]struct {
		token  string
		issued bool
	}{
		"multiplayer":           {with(func(map[string]any) {}), true},
		"other audience":        {with(func(c map[string]any) { c["aud"] = "api://other" }), true},
		"no audience":           {with(func(c map[string]any) { delete(c, "aud") }), true},
		"other issuer":          {with(func(c map[string]any) { c["iss"] = "https://evil.example/" }), false},
		"expired":               {with(func(c map[string]any) { c["exp"] = now.Add(-time.Second).Unix() }), false},
		"no expiry":             {with(func(c map[string]any) { delete(c, "exp") }), false},
		"not valid yet":         {with(func(c map[string]any) { c["nbf"] = now.Add(2 * time.Minute).Unix() }), false},
		"signed by another key": {signRS256(t, other, s.kid, s.claims(now, "client-key")), false},
		"signed by the client":  {selfSigned, false},
	} {
		if err := keys.VerifyIssued(context.Background(), tc.token, now); (err == nil) != tc.issued {
			t.Errorf("%s: VerifyIssued returned %v", name, err)
		}
	}
	if _, err := keys.verify(context.Background(), with(func(c map[string]any) { c["aud"] = "api://other" }), now); err == nil {
		t.Error("a Login took a token for another audience")
	}
}
