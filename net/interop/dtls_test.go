package interop

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
	piondtls "github.com/pion/dtls/v3"
	"github.com/pion/dtls/v3/pkg/crypto/elliptic"
)

// ourDTLS drives a Conn of this module over a UDP socket.
type ourDTLS struct {
	pc       *net.UDPConn
	peer     netip.AddrPort
	mu       sync.Mutex
	conn     *dtls.Conn
	received chan []byte
	stop     chan struct{}
	stopped  chan struct{}
}

func runOurDTLS(t *testing.T, pc *net.UDPConn, peer netip.AddrPort, cfg dtls.Config, client bool) *ourDTLS {
	o := &ourDTLS{pc: pc, peer: peer, received: make(chan []byte, 64), stop: make(chan struct{}), stopped: make(chan struct{})}
	send := func(b []byte) { _, _ = pc.WriteToUDPAddrPort(b, peer) }
	if client {
		o.conn = dtls.Client(cfg, send)
	} else {
		o.conn = dtls.Server(cfg, send)
	}
	o.mu.Lock()
	o.conn.Start(time.Now())
	o.mu.Unlock()
	go o.loop()
	t.Cleanup(func() {
		close(o.stop)
		<-o.stopped
	})
	return o
}

func (o *ourDTLS) loop() {
	defer close(o.stopped)
	buf := make([]byte, 2048)
	for {
		select {
		case <-o.stop:
			return
		default:
		}
		o.mu.Lock()
		deadline := o.conn.Deadline()
		o.mu.Unlock()
		read := time.Now().Add(10 * time.Millisecond)
		if !deadline.IsZero() && deadline.Before(read) {
			read = deadline
		}
		_ = o.pc.SetReadDeadline(read)
		n, from, err := o.pc.ReadFromUDPAddrPort(buf)
		now := time.Now()
		o.mu.Lock()
		if err == nil && from == o.peer {
			_ = o.conn.HandleDatagram(buf[:n], now, func(p []byte) {
				select {
				case o.received <- bytes.Clone(p):
				default:
				}
			})
		}
		if d := o.conn.Deadline(); !d.IsZero() && !now.Before(d) {
			_ = o.conn.HandleTimeout(now)
		}
		o.mu.Unlock()
	}
}

func (o *ourDTLS) waitEstablished(t *testing.T) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		o.mu.Lock()
		ok, err := o.conn.Established(), o.conn.Err()
		o.mu.Unlock()
		if ok {
			return
		}
		if err != nil {
			t.Fatalf("handshake failed: %v", err)
		}
	}
	t.Fatal("handshake did not complete")
}

func (o *ourDTLS) write(t *testing.T, b []byte) {
	t.Helper()
	o.mu.Lock()
	defer o.mu.Unlock()
	if err := o.conn.Write(b); err != nil {
		t.Fatal(err)
	}
}

func listenLoopback(t *testing.T) *net.UDPConn {
	t.Helper()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	// Bursts on loopback overflow the default buffer; the server's sockets
	// are sized the same way.
	_ = pc.SetReadBuffer(8 << 20)
	t.Cleanup(func() { _ = pc.Close() })
	return pc
}

func addrPort(a net.Addr) netip.AddrPort {
	return a.(*net.UDPAddr).AddrPort()
}

func fingerprintCheck(want string) func([][]byte) error {
	return func(chain [][]byte) error {
		if len(chain) == 0 || !dtls.MatchFingerprint(chain[0], "sha-256", want) {
			return errors.New("fingerprint mismatch")
		}
		return nil
	}
}

func pionConfig(cert dtls.Certificate, peerFingerprint string, suite piondtls.CipherSuiteID, curve elliptic.Curve) *piondtls.Config {
	check := fingerprintCheck(peerFingerprint)
	return &piondtls.Config{
		Certificates:   []tls.Certificate{{Certificate: cert.Chain, PrivateKey: cert.Key}},
		CipherSuites:   []piondtls.CipherSuiteID{suite},
		EllipticCurves: []elliptic.Curve{curve},
		// WebRTC peers always settle an SRTP profile, data channels or not.
		SRTPProtectionProfiles: []piondtls.SRTPProtectionProfile{piondtls.SRTP_AEAD_AES_128_GCM, piondtls.SRTP_AES128_CM_HMAC_SHA1_80},
		ClientAuth:             piondtls.RequireAnyClientCert,
		ExtendedMasterSecret:   piondtls.RequireExtendedMasterSecret,
		InsecureSkipVerify:     true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			return check(raw)
		},
	}
}

type dtlsVariant struct {
	name  string
	suite piondtls.CipherSuiteID
	curve elliptic.Curve
}

var dtlsVariants = []dtlsVariant{
	{"aes128-x25519", piondtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, elliptic.X25519},
	{"aes128-p256", piondtls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256, elliptic.P256},
	{"aes256-p384", piondtls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384, elliptic.P384},
}

func certificates(t *testing.T) (dtls.Certificate, dtls.Certificate) {
	t.Helper()
	a, err := dtls.GenerateCertificate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := dtls.GenerateCertificate()
	if err != nil {
		t.Fatal(err)
	}
	return a, b
}

// TestDTLSServerWithPionClient runs this module's server against the pion
// client, which offers what Chrome's DTLS stack offers.
func TestDTLSServerWithPionClient(t *testing.T) {
	for _, v := range dtlsVariants {
		for _, helloVerify := range []bool{false, true} {
			t.Run(v.name, func(t *testing.T) {
				ourCert, pionCert := certificates(t)
				ourSock, pionSock := listenLoopback(t), listenLoopback(t)
				ours := runOurDTLS(t, ourSock, addrPort(pionSock.LocalAddr()), dtls.Config{
					Certificate: ourCert,
					VerifyPeer:  fingerprintCheck(dtls.Fingerprint(pionCert.Chain[0])),
					HelloVerify: helloVerify,
				}, false)
				pion, err := piondtls.Client(pionSock, ourSock.LocalAddr(), pionConfig(pionCert, dtls.Fingerprint(ourCert.Chain[0]), v.suite, v.curve))
				if err != nil {
					t.Fatal(err)
				}
				defer pion.Close()
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := pion.HandshakeContext(ctx); err != nil {
					t.Fatalf("pion handshake: %v", err)
				}
				ours.waitEstablished(t)
				if _, ok := pion.SelectedSRTPProtectionProfile(); !ok {
					t.Fatal("no SRTP profile settled")
				}
				exchangeWithPion(t, ours, pion)
			})
		}
	}
}

// TestDTLSClientWithPionServer runs this module's client against the pion
// server, which sends a HelloVerifyRequest first.
func TestDTLSClientWithPionServer(t *testing.T) {
	for _, v := range dtlsVariants {
		t.Run(v.name, func(t *testing.T) {
			ourCert, pionCert := certificates(t)
			ourSock, pionSock := listenLoopback(t), listenLoopback(t)
			pionDone := make(chan *piondtls.Conn, 1)
			go func() {
				pion, err := piondtls.Server(pionSock, ourSock.LocalAddr(), pionConfig(pionCert, dtls.Fingerprint(ourCert.Chain[0]), v.suite, v.curve))
				if err != nil {
					t.Error(err)
					pionDone <- nil
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				if err := pion.HandshakeContext(ctx); err != nil {
					t.Errorf("pion handshake: %v", err)
					_ = pion.Close()
					pionDone <- nil
					return
				}
				pionDone <- pion
			}()
			ours := runOurDTLS(t, ourSock, addrPort(pionSock.LocalAddr()), dtls.Config{
				Certificate: ourCert,
				VerifyPeer:  fingerprintCheck(dtls.Fingerprint(pionCert.Chain[0])),
			}, true)
			pion := <-pionDone
			if pion == nil {
				t.FailNow()
			}
			defer pion.Close()
			ours.waitEstablished(t)
			if _, ok := pion.SelectedSRTPProtectionProfile(); !ok {
				t.Fatal("no SRTP profile settled")
			}
			exchangeWithPion(t, ours, pion)
		})
	}
}

func exchangeWithPion(t *testing.T, ours *ourDTLS, pion *piondtls.Conn) {
	t.Helper()
	if _, err := pion.Write([]byte("from pion")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ours.received:
		if string(got) != "from pion" {
			t.Fatalf("received %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no data from pion")
	}
	ours.write(t, []byte("from adamant"))
	_ = pion.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 1500)
	n, err := pion.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if string(buf[:n]) != "from adamant" {
		t.Fatalf("pion received %q", buf[:n])
	}
}
