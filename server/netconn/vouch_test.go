package netconn

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	bedrocklogin "github.com/dm-vev/adamant/net/bedrock/login"
)

// authService is an authorization service of the game run by a test: it
// publishes its OpenID configuration and key, and signs tokens.
type authService struct {
	*httptest.Server
	key *rsa.PrivateKey
}

func newAuthService(t *testing.T) *authService {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &authService{key: key}
	b64 := base64.RawURLEncoding
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": s.URL + "/", "jwks_uri": s.URL + "/keys"})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]any{
			"kty": "RSA", "kid": "k1", "n": b64.EncodeToString(key.N.Bytes()), "e": b64.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// token returns a token for an audience signed with key, as the service
// signs them.
func (s *authService) token(t *testing.T, key *rsa.PrivateKey, audience string) string {
	t.Helper()
	b64 := base64.RawURLEncoding
	now := time.Now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{"iss": s.URL + "/", "aud": audience, "exp": now.Add(time.Hour).Unix(), "nbf": now.Add(-time.Minute).Unix(), "cpk": "key"})
	signed := b64.EncodeToString(header) + "." + b64.EncodeToString(payload)
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signed + "." + b64.EncodeToString(sig)
}

// TestVouch vouches for the identity tokens of offers the authorization
// service signed, whatever their audience, and not for others.
func TestVouch(t *testing.T) {
	s := newAuthService(t)
	keys, err := bedrocklogin.NewKeys(context.Background(), nil, s.URL)
	if err != nil {
		t.Fatal(err)
	}
	l := &Listener{keys: keys}
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	for _, tc := range []struct {
		name    string
		token   string
		vouched bool
	}{
		{"signed for multiplayer", s.token(t, s.key, "api://auth-minecraft-services/multiplayer"), true},
		{"signed for another audience", s.token(t, s.key, "api://other"), true},
		{"signed by another key", s.token(t, other, "api://other"), false},
		{"not a token", "a.b.c", false},
	} {
		if err := l.vouch(tc.token); (err == nil) != tc.vouched {
			t.Errorf("%s: vouch returned %v", tc.name, err)
		}
	}
}
