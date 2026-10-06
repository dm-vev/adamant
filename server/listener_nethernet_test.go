package server

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestServerIdentityConcurrentCreationPreservesIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.pem")
	const clients = 8
	keys := make([]*ecdsa.PrivateKey, clients)
	errs := make([]error, clients)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range clients {
		wg.Go(func() {
			<-start
			keys[i], errs[i] = loadServerIdentityKey(path)
		})
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("create identity %d: %v", i, err)
		}
	}
	reloaded, err := loadServerIdentityKey(path)
	if err != nil {
		t.Fatal(err)
	}
	for i, key := range keys {
		if !key.Equal(reloaded) {
			t.Fatalf("listener %d acquired a different identity from the persisted key", i)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("identity is not private: permissions %o", info.Mode().Perm())
	}
}

func TestServerIdentityRejectsInvalidExistingKeyWithoutReplacingIt(t *testing.T) {
	wrongCurve, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalECPrivateKey(wrongCurve)
	if err != nil {
		t.Fatal(err)
	}
	for name, original := range map[string][]byte{
		"malformed":   []byte("damaged identity"),
		"wrong_curve": pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "identity.pem")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadServerIdentityKey(path); err == nil {
				t.Fatal("accepted an invalid identity")
			}
			stored, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stored, original) {
				t.Fatal("replaced an existing identity on decode failure")
			}
		})
	}
}
