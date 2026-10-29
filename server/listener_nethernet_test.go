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
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/nethernet/lan"
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

// TestNetherNetListenerServesSessions runs a client through the listener
// the server makes for NetherNet: it is accepted as a session connection,
// spawned, and disconnected with a message. The listener answers the
// query of server lists on the same port.
func TestNetherNetListenerServesSessions(t *testing.T) {
	uc := DefaultConfig()
	uc.Network.Address = "127.0.0.1:0"
	uc.Network.IdentityFile = filepath.Join(t.TempDir(), "identity.pem")
	uc.Network.EnableQuery = true
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
		if err := c.StartGameContext(ctx, netconn.GameData{EntityRuntimeID: 1, EntityUniqueID: 1}); err != nil {
			t.Errorf("start game: %v", err)
			return
		}
		accepted <- c
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := client.Dial(ctx, addr, client.Config{Identity: login.Identity{DisplayName: "Bot"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var srv session.Conn
	select {
	case srv = <-accepted:
	case <-ctx.Done():
		t.Fatal("server did not spawn the client")
	}
	if got := conn.StartGame().WorldName; got != "Adamant" {
		t.Fatalf("world name %q, want the server name", got)
	}

	// A query handshake on the port of the game is answered with a token.
	sock, err := net.Dial("udp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer sock.Close()
	_ = sock.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := sock.Write([]byte{0xfe, 0xfd, 0x09, 0, 0, 0, 1}); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 64)
	if n, err := sock.Read(buf); err != nil || n != 10 || buf[0] != 0x09 {
		t.Fatalf("query handshake answered with %x, %v", buf[:n], err)
	}

	if err := l.Disconnect(srv, "bye"); err != nil {
		t.Fatal(err)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	for {
		if _, err := conn.ReadPacket(); err != nil {
			if !errors.Is(err, client.DisconnectError("bye")) {
				t.Fatalf("read after the disconnect: %v", err)
			}
			return
		}
	}
}

func TestBatchCompression(t *testing.T) {
	if _, ok := batchCompression(bedrock.None{}).(bedrock.None); !ok {
		t.Fatal("no compression asked for, yet batches are compressed")
	}
	if got := batchCompression(nil); got != (bedrock.Flate{Level: 1}) {
		t.Fatalf("default compression %#v", got)
	}
}

// TestNetherNetListenerOnLAN shows the server to a client looking for games
// on the network, which joins it over the signaling of the LAN.
func TestNetherNetListenerOnLAN(t *testing.T) {
	uc := DefaultConfig()
	uc.Network.Address = "127.0.0.1:0"
	uc.Network.IdentityFile = filepath.Join(t.TempDir(), "identity.pem")
	conf := Config{Log: slog.New(slog.DiscardHandler), AuthDisabled: true, StatusProvider: statusProvider{name: "Adamant"}}
	l, err := uc.netherNetListener(conf)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	srv := publishLAN(l.(netherNetListener).Listener, conf, "127.0.0.1:0")
	if srv == nil {
		t.Fatal("the server does not show on LAN")
	}
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		_ = c.StartGameContext(ctx, netconn.GameData{EntityRuntimeID: 1, EntityUniqueID: 1})
	}()
	finder, err := lan.Config{Addr: "127.0.0.1:0"}.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer finder.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for finder.Servers()[srv.NetworkID()].ServerName != "Adamant" {
		if err := finder.Discover(netip.MustParseAddrPort(srv.Addr().String())); err != nil {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("the server did not answer on LAN")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if d := finder.Servers()[srv.NetworkID()]; d.Protocol != v2193.Protocol || !d.AcceptsSelfSignedAuth || d.ConnectionType != lan.ConnectionTypeLAN {
		t.Fatalf("LAN data %+v", d)
	}
	key, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	nn, err := nethernet.Dialer{Key: key}.DialSignaling(ctx, finder, strconv.FormatUint(srv.NetworkID(), 10))
	if err != nil {
		t.Fatal(err)
	}
	_ = nn.Close()
}
