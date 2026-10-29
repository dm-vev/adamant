package nethernet

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"testing"
	"time"
)

// selfSigned returns a certificate of example.org signed by itself, and a
// pool that trusts it.
func selfSigned(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "example.org"}, DNSNames: []string{"example.org"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

// TestSignalingOverHTTPSAndHTTP serves the signaling with a certificate:
// a client that opens with TLS is served HTTPS, one that does not plain
// HTTP, on the one port.
func TestSignalingOverHTTPSAndHTTP(t *testing.T) {
	cert, pool := selfSigned(t)
	l := listen(t, ListenConfig{TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}}})
	l.SetStatus(Status{ServerName: "Adamant"})
	addr := l.Addr().String()
	https := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.org"},
	}}
	for _, c := range []struct {
		client *http.Client
		url    string
	}{{https, "https://" + addr + "/v1/join"}, {&http.Client{Timeout: 5 * time.Second}, "http://" + addr + "/v1/join"}} {
		resp, err := c.client.Get(c.url)
		if err != nil {
			t.Fatalf("%s: %v", c.url, err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(b) == 0 {
			t.Fatalf("%s: %s %q", c.url, resp.Status, b)
		}
		if (resp.TLS != nil) != (c.client == https) {
			t.Fatalf("%s served with TLS %v", c.url, resp.TLS != nil)
		}
	}
}

// TestSniffingBounded opens more connections that send nothing than may
// wait for their first byte: those past the bound are closed at once, and
// the signaling still answers once they time out.
func TestSniffingBounded(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := selfSigned(t)
	l := newSniffListener(ln, &tls.Config{Certificates: []tls.Certificate{cert}})
	defer l.Close()
	var idle []net.Conn
	defer func() {
		for _, c := range idle {
			_ = c.Close()
		}
	}()
	for range maxSniffing {
		c, err := net.Dial("tcp", ln.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		idle = append(idle, c)
	}
	// Wait for the listener to take them all.
	deadline := time.Now().Add(5 * time.Second)
	for len(l.sniffing) < maxSniffing && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	extra, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	_ = extra.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := extra.Read(make([]byte, 1)); err == nil {
		t.Fatal("a connection past the bound was kept")
	}
	for _, c := range idle {
		_ = c.Close()
	}
	idle = nil
	// The sniffers end as their connections close, and a request is
	// served again.
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	go func() { _, _ = c.Write([]byte("G")) }()
	accepted := make(chan net.Conn, 1)
	go func() {
		if a, err := l.Accept(); err == nil {
			accepted <- a
		}
	}()
	select {
	case a := <-accepted:
		_ = a.Close()
	case <-time.After(10 * time.Second):
		t.Fatal("no connection accepted after the idle ones closed")
	}
}

// TestDialOverHTTPS joins a listener whose signaling has a certificate by
// an https:// address, and another client by an http:// one: both open
// their channels and carry messages.
func TestDialOverHTTPS(t *testing.T) {
	cert, pool := selfSigned(t)
	l := listen(t, ListenConfig{TLSConfig: &tls.Config{Certificates: []tls.Certificate{cert}}})
	_, port, _ := net.SplitHostPort(l.Addr().String())
	secure := newDialer(t)
	secure.HTTPClient = &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "example.org"},
	}}
	for _, c := range []struct {
		d       Dialer
		address string
	}{{secure, "https://127.0.0.1:" + port}, {newDialer(t), "http://127.0.0.1:" + port}} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cli, err := c.d.DialContext(ctx, c.address)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", c.address, err)
		}
		srv := accept(t, l)
		if err := cli.Send([]byte(c.address)); err != nil {
			t.Fatal(err)
		}
		if got := read(t, srv); string(got) != c.address {
			t.Fatalf("server read %q", got)
		}
		_ = cli.Close()
	}
}
