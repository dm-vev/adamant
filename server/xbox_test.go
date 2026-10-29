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
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"github.com/dm-vev/adamant/net/xbox/auth"
	"github.com/dm-vev/adamant/net/xbox/signaling"
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
