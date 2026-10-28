package nethernet

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
)

func listen(t *testing.T, cfg ListenConfig) *Listener {
	t.Helper()
	l, err := cfg.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l
}

func newDialer(t *testing.T) Dialer {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return Dialer{Key: key}
}

func dial(t *testing.T, d Dialer, l *Listener) *Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := d.DialContext(ctx, l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func accept(t *testing.T, l *Listener) *Conn {
	t.Helper()
	got := make(chan *Conn, 1)
	go func() {
		c, err := l.AcceptConn()
		if err != nil {
			t.Error(err)
		}
		got <- c
	}()
	select {
	case c := <-got:
		if c == nil {
			t.FailNow()
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	case <-time.After(10 * time.Second):
		t.Fatal("no connection accepted")
		return nil
	}
}

func read(t *testing.T, c *Conn) []byte {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	msg, err := c.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestListenAndDial(t *testing.T) {
	l := listen(t, ListenConfig{Sockets: 4})
	d := newDialer(t)
	cli := dial(t, d, l)
	srv := accept(t, l)
	if !srv.PublicKey().Equal(&d.Key.PublicKey) {
		t.Fatal("server conn does not carry the key of the client identity")
	}
	if !cli.PublicKey().Equal(l.PublicKey()) {
		t.Fatal("client conn does not carry the key of the server identity")
	}
	for i, size := range []int{1, 1000, 100000, 1 << 20} {
		if err := cli.Send(message(i, size)); err != nil {
			t.Fatal(err)
		}
		if got := read(t, srv); !bytes.Equal(got, message(i, size)) {
			t.Fatalf("server read %d bytes, want %d", len(got), size)
		}
		if err := srv.Send(message(100+i, size)); err != nil {
			t.Fatal(err)
		}
		if got := read(t, cli); !bytes.Equal(got, message(100+i, size)) {
			t.Fatalf("client read %d bytes, want %d", len(got), size)
		}
	}
	if err := cli.Send([]byte("last")); err != nil {
		t.Fatal(err)
	}
	_ = cli.Close()
	if got := read(t, srv); string(got) != "last" {
		t.Fatalf("read %q before the close", got)
	}
	_ = srv.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := srv.ReadMessage(); !errors.Is(err, errPeerClosed) {
		t.Fatalf("read after the client closed: %v", err)
	}
}

// TestManyDials opens connections at once over sockets sharing the port:
// each datagram must reach its own connection.
func TestManyDials(t *testing.T) {
	l := listen(t, ListenConfig{Sockets: 4})
	const n = 40
	d := newDialer(t)
	conns := make([]*Conn, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			c, err := d.DialContext(ctx, l.Addr().String())
			if err != nil {
				t.Error(err)
				return
			}
			conns[i] = c
		})
	}
	go func() {
		for {
			c, err := l.AcceptConn()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				for {
					msg, err := c.ReadMessage()
					if err != nil {
						return
					}
					if c.Send(msg) != nil {
						return
					}
				}
			}()
		}
	}()
	wg.Wait()
	if t.Failed() {
		return
	}
	for i, c := range conns {
		wg.Go(func() {
			defer c.Close()
			want := message(i, 5000+i)
			if err := c.Send(want); err != nil {
				t.Error(err)
				return
			}
			_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
			got, err := c.ReadMessage()
			if err != nil || !bytes.Equal(got, want) {
				t.Errorf("conn %d echo: %d bytes, %v", i, len(got), err)
			}
		})
	}
	wg.Wait()
}

func TestStatus(t *testing.T) {
	l := listen(t, ListenConfig{})
	get := func() (string, string) {
		resp, err := http.Get("http://" + l.Addr().String() + "/v1/join")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %s", resp.Status)
		}
		return resp.Header.Get("Content-Type"), string(b)
	}
	if _, body := get(); body != "" {
		t.Fatalf("status before one was set: %q", body)
	}
	want := Status{ServerName: "Adamant", Protocol: 975, Version: "1.26.52", PlayerCount: 3, MaxPlayerCount: 20, LevelName: "world"}
	l.SetStatus(want)
	typ, body := get()
	var got Status
	if err := json.Unmarshal([]byte(body), &got); err != nil || got != want || typ != "application/json" {
		t.Fatalf("status %s %q: %+v, %v", typ, body, got, err)
	}
	if !strings.Contains(body, `"maxPlayers":20`) {
		t.Fatalf("status JSON %s", body)
	}
}

func TestAnonymousOffers(t *testing.T) {
	l := listen(t, ListenConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := Dialer{}.DialContext(ctx, l.Addr().String())
	if err == nil || !strings.Contains(err.Error(), fmt.Sprint(codeIdentityNotAllowed)) {
		t.Fatalf("anonymous dial: %v", err)
	}
	open := listen(t, ListenConfig{AllowAnonymous: true})
	cli := dial(t, Dialer{}, open)
	srv := accept(t, open)
	if srv.PublicKey() != nil {
		t.Fatal("anonymous conn has a key")
	}
	if err := cli.Send([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, srv); string(got) != "hi" {
		t.Fatalf("read %q", got)
	}
}

// TestForgedIdentity signs the fingerprint with a key other than the one
// the token names: the offer is refused.
func TestForgedIdentity(t *testing.T) {
	l := listen(t, ListenConfig{})
	d := newDialer(t)
	other := newDialer(t)
	token, err := clientToken(other.Key, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	d.Token = token
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := d.DialContext(ctx, l.Addr().String()); err == nil || !strings.Contains(err.Error(), fmt.Sprint(codeIdentityNotAllowed)) {
		t.Fatalf("dial with a token of another key: %v", err)
	}
}

// TestPendingBound sends offers that never connect: past MaxPending the
// listener refuses them, and takes them again once they expired.
func TestPendingBound(t *testing.T) {
	l := listen(t, ListenConfig{MaxPending: 2, AllowAnonymous: true})
	cert, _ := dtls.GenerateCertificate()
	offer := encodeDescription(&description{
		ufrag: "abcd", pwd: randomString(24), setup: SetupActpass, maxMessageSize: maxMessageSize, sctpPort: 5000,
		fingerprints: []Fingerprint{{"sha-256", dtls.Fingerprint(cert.Chain[0])}},
	})
	post := func() int {
		resp, err := http.Post("http://"+l.Addr().String()+"/v1/join/1", "application/sdp", strings.NewReader(offer))
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	for i, want := range []int{200, 200, 503} {
		if got := post(); got != want {
			t.Fatalf("offer %d: status %d, want %d", i, got, want)
		}
	}
	l.mu.Lock()
	conns := make([]*Conn, 0, len(l.conns))
	for c := range l.conns {
		conns = append(conns, c)
	}
	l.mu.Unlock()
	for _, c := range conns {
		c.abort(errOpenTimeout)
	}
	if got := post(); got != 200 {
		t.Fatalf("offer after the pending ones closed: status %d", got)
	}
}

func TestListenerClose(t *testing.T) {
	l := listen(t, ListenConfig{})
	cli := dial(t, newDialer(t), l)
	accept(t, l)
	_ = l.Close()
	if _, err := l.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("accept after close: %v", err)
	}
	_ = cli.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := cli.ReadMessage(); !errors.Is(err, errPeerClosed) {
		t.Fatalf("client read after the listener closed: %v", err)
	}
}
