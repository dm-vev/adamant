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
	"os"
	"path/filepath"
	"testing"

	"github.com/dm-vev/adamant/net/xbox/auth"
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
