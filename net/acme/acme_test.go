package acme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeCA is an ACME certificate authority of a test: it checks every JWS,
// nonce and URL, fetches the http-01 challenge from the handler of the
// manager as a CA fetches it on port 80, and issues a certificate of the
// CSR.
type fakeCA struct {
	t       *testing.T
	srv     *httptest.Server
	solver  http.Handler
	caKey   *ecdsa.PrivateKey
	caCert  *x509.Certificate
	mu      sync.Mutex
	nonces  map[string]bool
	account *ecdsa.PublicKey
	token   string
	valid   bool
	cert    []byte
	issued  int
}

func newFakeCA(t *testing.T) *fakeCA {
	ca := &fakeCA{t: t, nonces: map[string]bool{}, token: "tok-" + fmt.Sprint(time.Now().UnixNano())}
	ca.caKey, _ = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Fake CA"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &ca.caKey.PublicKey, ca.caKey)
	ca.caCert, _ = x509.ParseCertificate(der)
	ca.srv = httptest.NewServer(ca)
	t.Cleanup(ca.srv.Close)
	return ca
}

func (ca *fakeCA) nonce(w http.ResponseWriter) {
	n := fmt.Sprint(time.Now().UnixNano())
	ca.nonces[n] = true
	w.Header().Set("Replay-Nonce", n)
}

// verify checks a JWS posted to the URL of r and returns its payload.
func (ca *fakeCA) verify(r *http.Request) ([]byte, bool) {
	var jws struct{ Protected, Payload, Signature string }
	body, _ := io.ReadAll(r.Body)
	if json.Unmarshal(body, &jws) != nil {
		return nil, false
	}
	h, _ := base64.RawURLEncoding.DecodeString(jws.Protected)
	var header struct {
		Alg, Nonce, URL, Kid string
		JWK                  map[string]string
	}
	if json.Unmarshal(h, &header) != nil || header.Alg != "ES256" || !ca.nonces[header.Nonce] || header.URL != ca.srv.URL+r.URL.Path {
		ca.t.Errorf("bad header %s for %s", h, r.URL.Path)
		return nil, false
	}
	delete(ca.nonces, header.Nonce)
	key := ca.account
	if header.JWK != nil {
		x, _ := base64.RawURLEncoding.DecodeString(header.JWK["x"])
		y, _ := base64.RawURLEncoding.DecodeString(header.JWK["y"])
		key = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
	} else if header.Kid != ca.srv.URL+"/account/1" {
		ca.t.Errorf("bad kid %q", header.Kid)
		return nil, false
	}
	sig, _ := base64.RawURLEncoding.DecodeString(jws.Signature)
	sum := sha256.Sum256([]byte(jws.Protected + "." + jws.Payload))
	if key == nil || len(sig) != 64 || !ecdsa.Verify(key, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		ca.t.Errorf("bad signature for %s", r.URL.Path)
		return nil, false
	}
	if header.JWK != nil {
		ca.account = key
	}
	payload, _ := base64.RawURLEncoding.DecodeString(jws.Payload)
	return payload, true
}

func (ca *fakeCA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	u := ca.srv.URL
	switch r.URL.Path {
	case "/directory":
		_ = json.NewEncoder(w).Encode(map[string]string{"newNonce": u + "/nonce", "newAccount": u + "/account", "newOrder": u + "/order"})
		return
	case "/nonce":
		ca.nonce(w)
		return
	}
	payload, ok := ca.verify(r)
	ca.nonce(w)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"type":"urn:ietf:params:acme:error:malformed"}`)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/account":
		w.Header().Set("Location", u+"/account/1")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"status":"valid"}`)
	case "/order", "/order/1":
		w.Header().Set("Location", u+"/order/1")
		status := "pending"
		if ca.valid {
			status = "ready"
		}
		if ca.cert != nil {
			status = "valid"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "authorizations": []string{u + "/authz/1"},
			"finalize": u + "/finalize", "certificate": u + "/cert"})
	case "/authz/1":
		status := "pending"
		if ca.valid {
			status = "valid"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "challenges": []map[string]string{
			{"type": "dns-01", "url": u + "/chall/dns", "token": "dns"},
			{"type": "http-01", "url": u + "/chall/1", "token": ca.token}}})
	case "/chall/1":
		// The CA fetches the challenge as it does on port 80.
		rec := httptest.NewRecorder()
		ca.solver.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://example.org/.well-known/acme-challenge/"+ca.token, nil))
		jwk := map[string]string{"crv": "P-256", "kty": "EC", "x": b64(pad32(ca.account.X)), "y": b64(pad32(ca.account.Y))}
		sum := sha256.Sum256([]byte(fmt.Sprintf(`{"crv":%q,"kty":%q,"x":%q,"y":%q}`, jwk["crv"], jwk["kty"], jwk["x"], jwk["y"])))
		ca.valid = rec.Body.String() == ca.token+"."+b64(sum[:])
		if !ca.valid {
			ca.t.Errorf("challenge answered %q", rec.Body.String())
		}
		_, _ = io.WriteString(w, `{"status":"processing"}`)
	case "/finalize":
		var req struct{ CSR string }
		_ = json.Unmarshal(payload, &req)
		der, _ := base64.RawURLEncoding.DecodeString(req.CSR)
		csr, err := x509.ParseCertificateRequest(der)
		if err != nil || csr.CheckSignature() != nil || len(csr.DNSNames) != 1 || csr.DNSNames[0] != "example.org" || !ca.valid {
			ca.t.Errorf("bad CSR %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		ca.issued++
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(int64(ca.issued + 1)), Subject: pkix.Name{CommonName: "example.org"}, DNSNames: csr.DNSNames,
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(90 * 24 * time.Hour)}
		cert, _ := x509.CreateCertificate(rand.Reader, tmpl, ca.caCert, csr.PublicKey, ca.caKey)
		ca.cert = append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.caCert.Raw})...)
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "valid", "certificate": u + "/cert", "finalize": u + "/finalize"})
	case "/cert":
		w.Header().Set("Content-Type", "application/pem-certificate-chain")
		_, _ = w.Write(ca.cert)
	default:
		http.NotFound(w, r)
	}
}

// TestManagerObtains obtains a certificate from the fake CA, which a TLS
// server then serves, and keeps it in the cache for the next start.
func TestManagerObtains(t *testing.T) {
	ca := newFakeCA(t)
	dir := t.TempDir()
	m := &Manager{Directory: ca.srv.URL + "/directory", Domain: "example.org", Email: "admin@example.org", CacheDir: dir}
	ca.solver = m.HTTPHandler(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := m.Run(ctx); err != nil {
		t.Fatal(err)
	}
	c, err := m.GetCertificate(&tls.ClientHelloInfo{ServerName: "example.org"})
	if err != nil || c.Leaf == nil || c.Leaf.Subject.CommonName != "example.org" || len(c.Certificate) != 2 {
		t.Fatalf("certificate %+v, %v", c, err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca.caCert)
	if _, err := c.Leaf.Verify(x509.VerifyOptions{DNSName: "example.org", Roots: pool}); err != nil {
		t.Fatal(err)
	}
	// A restart takes the certificate from the cache without the CA.
	again := &Manager{Directory: "http://127.0.0.1:1/unused", Domain: "example.org", CacheDir: dir}
	if err := again.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if c2, _ := again.GetCertificate(nil); c2 == nil || !c2.Leaf.Equal(c.Leaf) {
		t.Fatal("the cached certificate was not taken")
	}
	if ca.issued != 1 {
		t.Fatalf("%d certificates issued, want 1", ca.issued)
	}
	if rec := httptest.NewRecorder(); true {
		m.HTTPHandler(nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/acme-challenge/unknown", nil))
		if rec.Code != http.StatusNotFound || strings.Contains(rec.Body.String(), ".") {
			t.Fatalf("an unknown token answered %d %q", rec.Code, rec.Body.String())
		}
	}
}
