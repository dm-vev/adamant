package nethernet

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
)

// Dialer opens NetherNet connections to servers that take offers over
// HTTP, as the game does when it joins a server by address. Each
// connection has a UDP socket and a goroutine reading it.
type Dialer struct {
	// Key is the key of the client identity: the offer carries a token
	// naming its public key and the fingerprint of the DTLS certificate
	// signed with it. Without a key the offer carries no identity.
	Key *ecdsa.PrivateKey
	// Token is the identity token, which names the public key of Key in its
	// cpk claim. A self-signed token is made if empty, as an offline client
	// makes one.
	Token string
	// Domain is the domain of the identity provider the offer names.
	Domain string
	// Certificate is the DTLS certificate. A new one is made for each
	// connection if empty.
	Certificate dtls.Certificate
	// HTTPClient sends the offer. Nil means http.DefaultClient.
	HTTPClient *http.Client
	// SendBuffer bounds the bytes queued on the connection. Zero means 16
	// MiB.
	SendBuffer int
}

// DialContext connects to the server at address: host:port, or an http or
// https URL with a port. It returns once the channels are open.
func (d Dialer) DialContext(ctx context.Context, address string) (*Conn, error) {
	target, err := signalingURL(address)
	if err != nil {
		return nil, err
	}
	cert := d.Certificate
	if len(cert.Chain) == 0 {
		if cert, err = dtls.GenerateCertificate(); err != nil {
			return nil, err
		}
	}
	local := &description{
		ufrag:          randomString(8),
		pwd:            randomString(24),
		fingerprints:   []Fingerprint{{Algorithm: "sha-256", Digest: dtls.Fingerprint(cert.Chain[0])}},
		setup:          SetupActpass,
		maxMessageSize: maxMessageSize,
		sctpPort:       5000,
	}
	if d.Key != nil {
		token := d.Token
		if token == "" {
			if token, err = clientToken(d.Key, "", time.Now()); err != nil {
				return nil, err
			}
		}
		jws, err := signFingerprints(d.Key, local.fingerprints)
		if err != nil {
			return nil, err
		}
		if local.identity, err = encodeIdentity(d.Domain, token, jws); err != nil {
			return nil, err
		}
	}

	sock, err := net.ListenUDP("udp", nil)
	if err != nil {
		return nil, err
	}
	_ = sock.SetReadBuffer(socketBuffer)
	if a := routedAddr(ctx, target); a.IsValid() {
		local.candidates = []netip.AddrPort{netip.AddrPortFrom(a, sock.LocalAddr().(*net.UDPAddr).AddrPort().Port())}
	}
	answer, err := d.signal(ctx, target, encodeDescription(local))
	if err != nil {
		_ = sock.Close()
		return nil, err
	}
	remote, err := parseDescription(answer)
	if err != nil {
		_ = sock.Close()
		return nil, err
	}
	pub, err := answerIdentity(remote, time.Now())
	if err != nil {
		_ = sock.Close()
		return nil, err
	}
	if len(remote.candidates) == 0 {
		// An answer without candidates: try the address of the signaling.
		if a, err := netip.ParseAddr(target.Hostname()); err == nil {
			if p, err := strconv.ParseUint(target.Port(), 10, 16); err == nil {
				remote.candidates = []netip.AddrPort{netip.AddrPortFrom(a.Unmap(), uint16(p))}
			}
		}
	}

	c := newConn(connConfig{
		local:       local,
		remote:      remote,
		controlling: true,
		dtlsClient:  remote.setup != SetupActive,
		opener:      true,
		certificate: cert,
		sendBuffer:  d.SendBuffer,
		out:         func(b []byte, to netip.AddrPort) { _, _ = sock.WriteToUDPAddrPort(b, to) },
		outBatch:    batchWriter(sock),
		localAddr:   sock.LocalAddr(),
	}, time.Now())
	c.publicKey = pub
	c.onClose = func(*Conn) { _ = sock.Close() }
	go readConn(sock, c)
	c.mu.Lock()
	c.schedule()
	c.release()

	select {
	case <-c.opened:
		return c, nil
	case <-c.closed:
		return nil, c.Err()
	case <-ctx.Done():
		c.abort(ctx.Err())
		return nil, ctx.Err()
	}
}

// readConn hands the datagrams of the socket of a dialed connection to it
// until the socket closes with the connection.
func readConn(sock *net.UDPConn, c *Conn) {
	buf := make([]byte, 1<<16)
	for {
		n, from, err := sock.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		handle(c, buf[:n], netip.AddrPortFrom(from.Addr().Unmap(), from.Port()))
	}
}

// handle hands a datagram to a dialed connection, which a panic reading it
// fails.
func handle(c *Conn, b []byte, from netip.AddrPort) {
	defer func() {
		if v := recover(); v != nil {
			c.broken(v)
		}
	}()
	c.handleDatagram(b, from, time.Now())
}

// signalingURL returns the URL of the signaling of a server address.
func signalingURL(address string) (*url.URL, error) {
	if strings.Contains(address, "://") {
		u, err := url.Parse(address)
		if err != nil {
			return nil, err
		}
		if u.Scheme != "http" && u.Scheme != "https" || u.Port() == "" {
			return nil, fmt.Errorf("nethernet: %q is not an http or https URL with a port", address)
		}
		return &url.URL{Scheme: u.Scheme, Host: u.Host}, nil
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(host, port)}, nil
}

// routedAddr returns the local address the system sends to the server
// from: the host candidate of the offer.
func routedAddr(ctx context.Context, target *url.URL) netip.Addr {
	var nd net.Dialer
	probe, err := nd.DialContext(ctx, "udp", target.Host)
	if err != nil {
		return netip.Addr{}
	}
	defer probe.Close()
	a, ok := probe.LocalAddr().(*net.UDPAddr)
	if !ok {
		return netip.Addr{}
	}
	return a.AddrPort().Addr().Unmap()
}

// signal posts an offer and returns the answer.
func (d Dialer) signal(ctx context.Context, target *url.URL, offer string) (string, error) {
	var id [8]byte
	_, _ = rand.Read(id[:])
	u := target.JoinPath("/v1/join", strconv.FormatUint(binary.BigEndian.Uint64(id[:]), 10))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(offer))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/sdp")
	req.Header.Set("User-Agent", "libhttpclient/1.0.0.0")
	client := d.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxDescription+1))
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("nethernet: offer refused: %s %s", resp.Status, text)
	}
	if code, err := strconv.Atoi(text); err == nil {
		return "", fmt.Errorf("nethernet: offer refused with error %d", code)
	}
	if len(body) > maxDescription {
		return "", errDescription
	}
	return string(body), nil
}

// answerIdentity returns the key the identity of an answer proves, or nil
// for an answer without one.
func answerIdentity(d *description, now time.Time) (*ecdsa.PublicKey, error) {
	if d.identity == "" {
		return nil, nil
	}
	token, jws, err := decodeIdentity(d.identity)
	if err != nil {
		return nil, err
	}
	pub, err := tokenKey(token, now)
	if err != nil {
		return nil, err
	}
	if err := verifyFingerprints(pub, jws, d.fingerprints); err != nil {
		return nil, err
	}
	return pub, nil
}
