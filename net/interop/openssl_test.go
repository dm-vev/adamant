package interop

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
)

// openSSL returns the openssl binary, skipping the test without one.
func openSSL(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("openssl")
	if err != nil {
		t.Skip("openssl not found")
	}
	return bin
}

// writePEM writes the certificate and key of cert to files in dir.
func writePEM(t *testing.T, dir string, cert dtls.Certificate) (certFile, keyFile string) {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(cert.Key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Chain[0]}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certFile, keyFile
}

// lockedBuffer is a buffer the output of a process goes to while a test
// reads it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// startOpenSSL runs openssl with args, its standard input a pipe, in a
// process group of its own that the test kills at the end.
func startOpenSSL(t *testing.T, args ...string) (io.WriteCloser, *lockedBuffer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, openSSL(t), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	out := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
		if t.Failed() {
			t.Logf("openssl %s:\n%s", strings.Join(args, " "), out)
		}
	})
	return in, out
}

// exchangeWithOpenSSL sends a line each way between ours and openssl,
// whose standard input in sends and whose output out prints what it reads.
func exchangeWithOpenSSL(t *testing.T, ours *ourDTLS, in io.Writer, out *lockedBuffer) {
	t.Helper()
	if _, err := io.WriteString(in, "from openssl\n"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ours.received:
		if string(got) != "from openssl\n" {
			t.Fatalf("read %q from openssl", got)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no data from openssl")
	}
	ours.write(t, []byte("from ours\n"))
	for end := time.Now().Add(10 * time.Second); !strings.Contains(out.String(), "from ours"); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal("openssl did not print our data")
		}
	}
}

// opensslSuites are the suites this module offers, as OpenSSL names them.
var opensslSuites = []string{"ECDHE-ECDSA-AES128-GCM-SHA256", "ECDHE-ECDSA-AES256-GCM-SHA384"}

// TestDTLSServerWithOpenSSLClient runs this module's server against the
// client of OpenSSL, the DTLS of the compatibility matrix besides pion's
// and BoringSSL's (Chrome), with and without a HelloVerifyRequest.
func TestDTLSServerWithOpenSSLClient(t *testing.T) {
	openSSL(t)
	for _, suite := range opensslSuites {
		for _, helloVerify := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-hvr-%v", suite, helloVerify), func(t *testing.T) {
				ourCert, peerCert := certificates(t)
				certFile, keyFile := writePEM(t, t.TempDir(), peerCert)
				ourSock := listenLoopback(t)
				// The client binds the port it tells us of, so that the
				// server knows its peer before the first datagram.
				peerSock := listenLoopback(t)
				peer := addrPort(peerSock.LocalAddr())
				_ = peerSock.Close()
				ours := runOurDTLS(t, ourSock, peer, dtls.Config{
					Certificate: ourCert,
					VerifyPeer:  fingerprintCheck(dtls.Fingerprint(peerCert.Chain[0])),
					HelloVerify: helloVerify,
				}, false)
				in, out := startOpenSSL(t, "s_client", "-dtls1_2", "-quiet",
					"-connect", ourSock.LocalAddr().String(), "-bind", peer.String(),
					"-cert", certFile, "-key", keyFile, "-cipher", suite,
					"-use_srtp", "SRTP_AEAD_AES_128_GCM:SRTP_AES128_CM_SHA1_80")
				ours.waitEstablished(t)
				exchangeWithOpenSSL(t, ours, in, out)
			})
		}
	}
}

// TestDTLSClientWithOpenSSLServer runs this module's client against the
// server of OpenSSL, which asks for a cookie first (-listen) and for the
// certificate of the client.
func TestDTLSClientWithOpenSSLServer(t *testing.T) {
	openSSL(t)
	for _, suite := range opensslSuites {
		t.Run(suite, func(t *testing.T) {
			ourCert, peerCert := certificates(t)
			certFile, keyFile := writePEM(t, t.TempDir(), peerCert)
			peerSock := listenLoopback(t)
			peer := addrPort(peerSock.LocalAddr())
			_ = peerSock.Close()
			in, out := startOpenSSL(t, "s_server", "-dtls1_2", "-quiet", "-listen",
				"-accept", peer.String(), "-cert", certFile, "-key", keyFile, "-cipher", suite,
				"-Verify", "1", "-use_srtp", "SRTP_AEAD_AES_128_GCM:SRTP_AES128_CM_SHA1_80")
			// s_server binds its port once it started.
			for end := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
				if c, err := os.ReadFile("/proc/net/udp"); err == nil && strings.Contains(string(c), fmt.Sprintf(":%04X ", peer.Port())) {
					break
				}
				if time.Now().After(end) {
					t.Fatal("openssl did not bind its port")
				}
			}
			ours := runOurDTLS(t, listenLoopback(t), peer, dtls.Config{
				Certificate: ourCert,
				VerifyPeer:  fingerprintCheck(dtls.Fingerprint(peerCert.Chain[0])),
			}, true)
			ours.waitEstablished(t)
			exchangeWithOpenSSL(t, ours, in, out)
		})
	}
}
