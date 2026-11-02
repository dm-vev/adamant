package netconn

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bedrocklogin "github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/nethernet"
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

// token returns a token for an audience naming a client key, signed with
// key, as the service signs them.
func (s *authService) token(t *testing.T, key *rsa.PrivateKey, audience, cpk string) string {
	t.Helper()
	b64 := base64.RawURLEncoding
	now := time.Now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "kid": "k1", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{"iss": s.URL + "/", "aud": audience, "exp": now.Add(time.Hour).Unix(), "nbf": now.Add(-time.Minute).Unix(), "cpk": cpk})
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
		{"signed for multiplayer", s.token(t, s.key, "api://auth-minecraft-services/multiplayer", "key"), true},
		{"signed for another audience", s.token(t, s.key, "api://other", "key"), true},
		{"signed by another key", s.token(t, other, "api://other", "key"), false},
		{"not a token", "a.b.c", false},
	} {
		if err := l.vouch(tc.token); (err == nil) != tc.vouched {
			t.Errorf("%s: vouch returned %v", tc.name, err)
		}
	}
}

// offerSignaling takes the offer of a dial and answers nothing.
type offerSignaling struct {
	offers chan string
	done   chan struct{}
}

func (s offerSignaling) WriteSignal(sig nethernet.Signal) error {
	if sig.Type == nethernet.SignalOffer {
		s.offers <- sig.Data
	}
	return nil
}

func (s offerSignaling) ReadSignal() (nethernet.Signal, error) {
	<-s.done
	return nethernet.Signal{}, net.ErrClosed
}

// offerOf returns the offer a dialer makes.
func offerOf(t *testing.T, d nethernet.Dialer) string {
	t.Helper()
	s := offerSignaling{offers: make(chan string, 1), done: make(chan struct{})}
	defer close(s.done)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = d.DialSignaling(ctx, s, "server") }()
	select {
	case offer := <-s.offers:
		return offer
	case <-time.After(10 * time.Second):
		t.Fatal("the dialer made no offer")
		return ""
	}
}

// TestVouchedOfferKeepsItsSlot fills the one slot for connections opening
// of a listener with Xbox Live authentication: an offer the authorization
// service vouches for displaces one it does not, and is not displaced by
// one it does not, which another such displaces.
func TestVouchedOfferKeepsItsSlot(t *testing.T) {
	s := newAuthService(t)
	defer func(keys func() (*bedrocklogin.Keys, error)) { xboxKeys = keys }(xboxKeys)
	xboxKeys = func() (*bedrocklogin.Keys, error) { return bedrocklogin.NewKeys(context.Background(), nil, s.URL) }
	l, err := Config{}.Listen(nethernet.ListenConfig{AllowAnonymous: true, MaxPending: 1}, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	token := s.token(t, s.key, "api://other", base64.StdEncoding.EncodeToString(der))
	vouched, anonymous := offerOf(t, nethernet.Dialer{Key: key, Token: token}), offerOf(t, nethernet.Dialer{})
	for i, step := range []struct {
		offer string
		want  int
	}{{anonymous, 200}, {anonymous, 200}, {vouched, 200}, {anonymous, 503}} {
		resp, err := http.Post("http://"+l.Addr().String()+"/v1/join/1", "application/sdp", strings.NewReader(step.offer))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != step.want {
			t.Fatalf("offer %d: status %d, want %d", i, resp.StatusCode, step.want)
		}
	}
}
