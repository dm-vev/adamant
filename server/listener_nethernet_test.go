package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/netconn"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/dm-vev/adamant/net/bedrock/client"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/nethernet/lan"
	"github.com/dm-vev/adamant/net/proxy"
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

// TestNetherNetListenerTakesProxies serves the links of proxies the config
// names: a player a proxy opens on one logs in, and a secret short enough
// to guess keeps the server from starting.
func TestNetherNetListenerTakesProxies(t *testing.T) {
	uc := DefaultConfig()
	uc.Network.Address = "127.0.0.1:0"
	uc.Network.IdentityFile = filepath.Join(t.TempDir(), "identity.pem")
	uc.Proxy.Address = "127.0.0.1:0"
	uc.Proxy.Secret = "short"
	conf := Config{Log: slog.New(slog.DiscardHandler), AuthDisabled: true, StatusProvider: statusProvider{name: "Adamant"}}
	if l, err := uc.netherNetListener(conf); err == nil {
		_ = l.Close()
		t.Fatal("the server started with a secret of 5 characters")
	}

	// The address of the links is only known once listening: take a port.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	uc.Proxy.Address = probe.Addr().String()
	_ = probe.Close()
	uc.Proxy.Secret = strings.Repeat("s", minProxySecret)
	l, err := uc.netherNetListener(conf)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	accepted := make(chan session.Conn, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			accepted <- c
		}
	}()
	link, err := proxy.Dial(uc.Proxy.Address, []byte(uc.Proxy.Secret))
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	// The proxy names a login the rules of the server take.
	identity, _ := json.Marshal(login.Identity{DisplayName: "Proxied", Identity: "8b4ec2c6-6f3b-4d0a-9d55-3b7e0c3c3f1a"})
	data, _ := json.Marshal(login.DefaultClientData("play.example.invalid:19132", "Proxied", v2193.Version))
	p, err := link.Open(proxy.Token{Identity: identity, ClientData: data, Addr: "203.0.113.7:4242", Protocol: v2193.Protocol})
	if err != nil {
		t.Fatal(err)
	}
	b := bedrock.NewConn(p, bedrock.Config{})
	b.EnableCompression(bedrock.None{})
	_ = p.SetReadDeadline(time.Now().Add(10 * time.Second))
	for _, response := range []uint32{v2193.PackResponseAllPacksDownloaded, v2193.PackResponseCompleted} {
		w := wire.Writer{B: bedrock.AppendHeader(nil, bedrock.Header{ID: v2193.IDResourcePackClientResponse})}
		(&v2193.ResourcePackClientResponsePacket{Response: response}).Encode(&w)
		if err := b.WritePacket(w.B); err != nil {
			t.Fatal(err)
		}
		if err := b.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case c := <-accepted:
		if c.IdentityData().DisplayName != "Proxied" || c.RemoteAddr().String() != "203.0.113.7:4242" {
			t.Fatalf("accepted %s from %v", c.IdentityData().DisplayName, c.RemoteAddr())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the player of the proxy was not accepted")
	}
}

// TestNetherNetListenerServesHTTPS serves the signaling over HTTPS with
// the certificate the authority issued the domain, kept in the cache of
// the server, and over plain HTTP on the same port.
func TestNetherNetListenerServesHTTPS(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.org"}, DNSNames: []string{"example.org"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	cache := t.TempDir()
	pemData := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	if err := os.WriteFile(filepath.Join(cache, "certificate.pem"), pemData, 0o600); err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)

	uc := DefaultConfig()
	uc.Network.Address = "127.0.0.1:0"
	uc.Network.IdentityFile = filepath.Join(t.TempDir(), "identity.pem")
	uc.Network.HTTPSDomain = "example.org"
	uc.Network.ACMECacheDir = cache
	uc.Network.ACMEDirectory = "http://127.0.0.1:1/unused"
	uc.Network.ACMEChallengeAddress = "127.0.0.1:0"
	l, err := uc.netherNetListener(Config{Log: slog.New(slog.DiscardHandler), AuthDisabled: true, StatusProvider: statusProvider{name: "Adamant"}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	addr := l.(netherNetListener).Addr().String()
	https := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.org"}}}
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := https.Get("https://" + addr + "/v1/join")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK || resp.TLS == nil {
				t.Fatalf("HTTPS signaling answered %s", resp.Status)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("HTTPS signaling: %v", err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	resp, err := http.Get("http://" + addr + "/v1/join")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.TLS != nil {
		t.Fatalf("HTTP signaling answered %s", resp.Status)
	}
}
