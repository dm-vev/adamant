package interop

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	gonethernet "github.com/df-mc/go-nethernet"
	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/pion/webrtc/v4"
)

// pionAPI returns a WebRTC API that gathers loopback candidates, which the
// tests connect over.
func pionAPI() *webrtc.API {
	var settings webrtc.SettingEngine
	settings.SetIncludeLoopbackCandidate(true)
	return webrtc.NewAPI(webrtc.WithSettingEngine(settings))
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// offlineToken returns a self-signed identity token naming key in its cpk
// claim, as an offline client makes one.
func offlineToken(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding
	pub := base64.StdEncoding.EncodeToString(der)
	now := time.Now()
	header := fmt.Sprintf(`{"alg":"ES384","x5u":%q}`, pub)
	payload := fmt.Sprintf(`{"cpk":%q,"exp":%d,"nbf":%d}`, pub, now.Add(time.Hour).Unix(), now.Add(-time.Minute).Unix())
	signing := b64.EncodeToString([]byte(header)) + "." + b64.EncodeToString([]byte(payload))
	sum := sha512.Sum384([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	sig := make([]byte, 96)
	r.FillBytes(sig[:48])
	s.FillBytes(sig[48:])
	return signing + "." + b64.EncodeToString(sig)
}

func p384(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func netherMessage(i, size int) []byte {
	b := make([]byte, size)
	for j := range b {
		b[j] = byte(i*31 + j*7)
	}
	return b
}

// netherSizes cover one segment, the largest segment, and several.
var netherSizes = []int{1, 1000, 100000, 262143, 262144, 600000}

// TestGoNetherNetDialsOurListener joins our listener with the pion based
// dialer gophertunnel uses.
func TestGoNetherNetDialsOurListener(t *testing.T) {
	l, err := nethernet.ListenConfig{}.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	key := p384(t)
	d := gonethernet.Dialer{
		API:               pionAPI(),
		Log:               quietLog(),
		Identity:          &gonethernet.Identity{PrivateKey: key, Token: offlineToken(t, key)},
		DisableTrickleICE: true,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cli, err := d.DialContext(ctx, "http://"+l.Addr().String(), endpoint.ClientConfig{Logger: quietLog()}.New())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	srv, err := l.AcceptConn()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	if !srv.PublicKey().Equal(&key.PublicKey) {
		t.Fatal("our conn does not carry the key of the pion identity")
	}
	if !cli.PublicKey().Equal(l.PublicKey()) {
		t.Fatal("pion did not take the key of our identity")
	}
	for i, size := range netherSizes {
		if _, err := cli.Write(netherMessage(i, size)); err != nil {
			t.Fatal(err)
		}
		_ = srv.SetReadDeadline(time.Now().Add(10 * time.Second))
		got, err := srv.ReadMessage()
		if err != nil || !bytes.Equal(got, netherMessage(i, size)) {
			t.Fatalf("we read %d bytes of %d: %v", len(got), size, err)
		}
		if err := srv.Send(netherMessage(100+i, size)); err != nil {
			t.Fatal(err)
		}
		got, err = readPionPacket(cli)
		if err != nil || !bytes.Equal(got, netherMessage(100+i, size)) {
			t.Fatalf("pion read %d bytes of %d: %v", len(got), size, err)
		}
	}
	_ = cli.Close()
	_ = srv.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := srv.ReadMessage(); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("read after pion closed: %v", err)
	}
}

// readPionPacket reads a packet of a go-nethernet conn within ten seconds.
func readPionPacket(c *gonethernet.Conn) ([]byte, error) {
	type result struct {
		b   []byte
		err error
	}
	done := make(chan result, 1)
	go func() {
		b, err := c.ReadPacket()
		done <- result{b, err}
	}()
	select {
	case r := <-done:
		return r.b, r.err
	case <-time.After(10 * time.Second):
		return nil, errors.New("timed out")
	}
}

// TestOurDialerJoinsGoNetherNet joins the pion based listener with our
// dialer.
func TestOurDialerJoinsGoNetherNet(t *testing.T) {
	handler := endpoint.HandlerConfig{Logger: quietLog()}.New()
	gl, err := gonethernet.ListenConfig{API: pionAPI(), Log: quietLog(), DisableTrickleICE: true}.Listen(handler)
	if err != nil {
		t.Fatal(err)
	}
	defer gl.Close()
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	go server.Serve(tcp)
	defer server.Close()

	key := p384(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cli, err := nethernet.Dialer{Key: key}.DialContext(ctx, "http://"+tcp.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	accepted, err := gl.Accept()
	if err != nil {
		t.Fatal(err)
	}
	srv := accepted.(*gonethernet.Conn)
	defer srv.Close()
	if !srv.PublicKey().Equal(&key.PublicKey) {
		t.Fatal("pion conn does not carry the key of our identity")
	}
	if cli.PublicKey() == nil {
		t.Fatal("our conn took no key from the pion identity")
	}
	for i, size := range netherSizes {
		if err := cli.Send(netherMessage(i, size)); err != nil {
			t.Fatal(err)
		}
		got, err := readPionPacket(srv)
		if err != nil || !bytes.Equal(got, netherMessage(i, size)) {
			t.Fatalf("pion read %d bytes of %d: %v", len(got), size, err)
		}
		if _, err := srv.Write(netherMessage(100+i, size)); err != nil {
			t.Fatal(err)
		}
		_ = cli.SetReadDeadline(time.Now().Add(10 * time.Second))
		got, err = cli.ReadMessage()
		if err != nil || !bytes.Equal(got, netherMessage(100+i, size)) {
			t.Fatalf("we read %d bytes of %d: %v", len(got), size, err)
		}
	}
	if err := cli.Send([]byte("last")); err != nil {
		t.Fatal(err)
	}
	_ = cli.Close()
	if got, err := readPionPacket(srv); err != nil || string(got) != "last" {
		t.Fatalf("pion read %q, %v before the close", got, err)
	}
	if _, err := readPionPacket(srv); err == nil {
		t.Fatal("pion conn still open after we closed")
	}
}
