package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/xbox/auth"
	"github.com/dm-vev/adamant/net/xbox/signaling"
	"github.com/dm-vev/adamant/net/xbox/social"
)

// TestXboxTokenFile keeps a sign-in in a file only its owner reads and
// signs in from it on the next start without asking the user.
func TestXboxTokenFile(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	snap := auth.Snapshot{RefreshToken: "refresh", ProofKey: base64.StdEncoding.EncodeToString(der), DeviceID: "{device}"}
	path := filepath.Join(t.TempDir(), "xbox-token.json")
	if err := saveXboxToken(path, snap); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v, want 0600", info.Mode().Perm())
	}
	snap.RefreshToken = "rotated"
	if err := saveXboxToken(path, snap); err != nil {
		t.Fatal(err)
	}
	s, err := xboxSession(context.Background(), path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Snapshot(); got != snap {
		t.Fatalf("restored %+v, want %+v", got, snap)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("%d files beside the token file", len(entries)-1)
	}
}

// TestRelayServer takes the first TURN server over UDP the signaling
// service hands out, with its credentials, and passes over STUN servers
// and TURN over TCP.
func TestRelayServer(t *testing.T) {
	cfg, ok := relayServer(context.Background(), []signaling.ICEServer{
		{URLs: []string{"stun:192.0.2.1:3478"}},
		{URLs: []string{"turn:192.0.2.2:443?transport=tcp", "turn:192.0.2.3:3479?transport=udp"}, Username: "u", Credential: "p"},
		{URLs: []string{"turn:192.0.2.4"}, Username: "x", Credential: "y"},
	})
	if !ok || cfg.Server != netip.MustParseAddrPort("192.0.2.3:3479") || cfg.Username != "u" || cfg.Password != "p" {
		t.Fatalf("relay server %+v, %v", cfg, ok)
	}
	if cfg, ok := relayServer(context.Background(), []signaling.ICEServer{{URLs: []string{"turn:192.0.2.4"}}}); !ok || cfg.Server.Port() != 3478 {
		t.Fatalf("default port: %+v, %v", cfg, ok)
	}
	if _, ok := relayServer(context.Background(), []signaling.ICEServer{{URLs: []string{"stun:192.0.2.1"}}}); ok {
		t.Fatal("a STUN server taken for a relay")
	}
}

// TestXboxFriendsAccept adds the people who sent the account a friend
// request, and stops for as long as the service asks once it refuses for
// too many.
func TestXboxFriendsAccept(t *testing.T) {
	var added []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/users/me/people/friendrequests(received)":
			_, _ = io.WriteString(w, `{"people":[{"xuid":"2535400000000002","gamertag":"A"},{"xuid":"2535400000000003","gamertag":"B"},{"xuid":"2535400000000004","gamertag":"C"}]}`)
		case strings.HasPrefix(r.URL.Path, "/users/me/people/friends/v2/"):
			if len(added) == 2 {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			added = append(added, r.URL.Path)
			_, _ = io.WriteString(w, `{"isFriend":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	f := &xboxFriends{
		client: social.Client{Token: func(context.Context) (string, error) { return "XBL3.0 x=1;t", nil },
			Endpoints: social.Endpoints{Social: srv.URL, PeopleHub: srv.URL, SessionDirectory: srv.URL}},
		requests: make(chan struct{}, 1),
		log:      slog.New(slog.DiscardHandler),
	}
	if wait := f.accept(context.Background()); wait != 30*time.Second || len(added) != 2 {
		t.Fatalf("waits %v after adding %v", wait, added)
	}
}
