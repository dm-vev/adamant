package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/session"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
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

// anonymousNetwork dials gophertunnel clients over the NetherNet dialer
// without an identity.
type anonymousNetwork struct{}

func (anonymousNetwork) DialContext(ctx context.Context, address string) (net.Conn, error) {
	c, err := nethernet.Dialer{}.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	return c, nil
}

func (anonymousNetwork) PingContext(context.Context, string) ([]byte, error) {
	return nil, errors.New("no ping")
}

func (anonymousNetwork) Listen(string) (minecraft.NetworkListener, error) {
	return nil, errors.New("no listen")
}

// TestNetherNetListenerServesSessions runs a gophertunnel client through
// the listener the server makes for NetherNet: it is accepted as a
// session connection, spawned, and disconnected with a message.
func TestNetherNetListenerServesSessions(t *testing.T) {
	uc := DefaultConfig()
	uc.Network.Address = "127.0.0.1:0"
	uc.Network.IdentityFile = filepath.Join(t.TempDir(), "identity.pem")
	l, err := uc.netherNetListener(Config{Log: slog.New(slog.DiscardHandler), AuthDisabled: true, StatusProvider: statusProvider{name: "Adamant"}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	addr := l.(netherNetListener).Addr().String()
	accepted := make(chan session.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := c.StartGameContext(ctx, minecraft.GameData{EntityRuntimeID: 1, EntityUniqueID: 1}); err != nil {
			t.Errorf("start game: %v", err)
			return
		}
		accepted <- c
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: "Bot"}}.DialContextNetwork(ctx, anonymousNetwork{}, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.DoSpawnContext(ctx); err != nil {
		t.Fatal(err)
	}
	var srv session.Conn
	select {
	case srv = <-accepted:
	case <-ctx.Done():
		t.Fatal("server did not spawn the client")
	}
	if got := conn.GameData().WorldName; got != "Adamant" {
		t.Fatalf("world name %q, want the server name", got)
	}
	if err := l.Disconnect(srv, "bye"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		if _, err := conn.ReadPacket(); err != nil {
			var d minecraft.DisconnectError
			if !errors.As(err, &d) || string(d) != "bye" {
				t.Fatalf("read after the disconnect: %v", err)
			}
			return
		}
	}
}

func TestBatchCompression(t *testing.T) {
	if _, ok := batchCompression(packet.NopCompression).(bedrock.None); !ok {
		t.Fatal("no compression asked for, yet batches are compressed")
	}
	for _, c := range []packet.Compression{nil, packet.FlateCompression, packet.SnappyCompression} {
		if got := batchCompression(c); got != (bedrock.Flate{Level: 1}) {
			t.Fatalf("%T: compression %#v", c, got)
		}
	}
}
