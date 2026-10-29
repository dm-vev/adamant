// Package acme obtains and renews the certificate of a domain from an ACME
// certificate authority (RFC 8555), such as Let's Encrypt, proving the
// domain with the http-01 challenge, so that clients join the signaling
// of a server over HTTPS with a certificate they trust rather than trust
// its key on first use.
package acme

import (
	"bytes"
	"context"
	"crypto"
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
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LetsEncrypt is the directory of the production CA of Let's Encrypt.
const LetsEncrypt = "https://acme-v02.api.letsencrypt.org/directory"

// renewBefore is how long before it expires a certificate is renewed.
const renewBefore = 30 * 24 * time.Hour

// Manager keeps the certificate of a domain: it loads it from its cache,
// obtains one when there is none, and renews it a month before it expires.
type Manager struct {
	// Directory is the directory URL of the CA. Empty means LetsEncrypt.
	Directory string
	// Domain is the domain the certificate is for.
	Domain string
	// Email is the contact of the account, optional.
	Email string
	// CacheDir keeps the key of the account and the certificate across
	// restarts. Empty keeps them in memory alone.
	CacheDir string
	// HTTPClient talks to the CA. Nil means http.DefaultClient.
	HTTPClient *http.Client

	mu         sync.Mutex
	cert       *tls.Certificate
	challenges map[string]string // token -> key authorization
	accountKey *ecdsa.PrivateKey
	account    string
	dir        *directory
	nonce      string
}

// directory is the directory of a CA.
type directory struct {
	NewNonce   string `json:"newNonce"`
	NewAccount string `json:"newAccount"`
	NewOrder   string `json:"newOrder"`
}

// GetCertificate hands the certificate of the domain to a TLS server:
// tls.Config.GetCertificate.
func (m *Manager) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cert == nil {
		return nil, errors.New("acme: no certificate yet")
	}
	return m.cert, nil
}

// HTTPHandler answers the http-01 challenges of the CA, which it asks for
// at http://<domain>/.well-known/acme-challenge/<token> on port 80, and
// hands other requests to fallback, a 404 if nil.
func (m *Manager) HTTPHandler(fallback http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.URL.Path, "/.well-known/acme-challenge/")
		if !ok {
			if fallback != nil {
				fallback.ServeHTTP(w, r)
			} else {
				http.NotFound(w, r)
			}
			return
		}
		m.mu.Lock()
		auth, ok := m.challenges[token]
		m.mu.Unlock()
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, auth)
	})
}

// Run keeps the certificate until ctx ends: it loads it from the cache or
// obtains it, then renews it when due. It returns once the first
// certificate is in hand or failed; the renewals go on in the background.
func (m *Manager) Run(ctx context.Context) error {
	if m.Domain == "" {
		return errors.New("acme: no domain")
	}
	if c, err := m.loadCert(); err == nil && time.Until(c.Leaf.NotAfter) > renewBefore {
		m.setCert(c)
	} else if err := m.renew(ctx); err != nil {
		return err
	}
	go m.keep(ctx)
	return nil
}

func (m *Manager) setCert(c *tls.Certificate) {
	m.mu.Lock()
	m.cert = c
	m.mu.Unlock()
}

// keep renews the certificate when due until ctx ends, retrying hourly
// after a failure.
func (m *Manager) keep(ctx context.Context) {
	for {
		m.mu.Lock()
		due := m.cert.Leaf.NotAfter.Add(-renewBefore)
		m.mu.Unlock()
		wait := time.Until(due)
		select {
		case <-ctx.Done():
			return
		case <-time.After(max(wait, time.Minute)):
		}
		if err := m.renew(ctx); err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Hour):
			}
		}
	}
}

// renew obtains a new certificate and keeps it.
func (m *Manager) renew(ctx context.Context) error {
	c, err := m.obtain(ctx)
	if err != nil {
		return err
	}
	m.setCert(c)
	return m.storeCert(c)
}

// obtain runs an order: the account, the order of the domain, its
// authorization by http-01, the finalization with a new key, and the
// download of the certificate.
func (m *Manager) obtain(ctx context.Context) (*tls.Certificate, error) {
	if err := m.ensureAccount(ctx); err != nil {
		return nil, err
	}
	var order struct {
		Status         string   `json:"status"`
		Authorizations []string `json:"authorizations"`
		Finalize       string   `json:"finalize"`
		Certificate    string   `json:"certificate"`
	}
	orderURL, err := m.post(ctx, m.dir.NewOrder, map[string]any{
		"identifiers": []map[string]string{{"type": "dns", "value": m.Domain}},
	}, &order)
	if err != nil {
		return nil, fmt.Errorf("acme: new order: %w", err)
	}
	for _, authz := range order.Authorizations {
		if err := m.authorize(ctx, authz); err != nil {
			return nil, err
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: m.Domain}, DNSNames: []string{m.Domain},
	}, key)
	if err != nil {
		return nil, err
	}
	if _, err := m.post(ctx, order.Finalize, map[string]string{"csr": b64(csr)}, &order); err != nil {
		return nil, fmt.Errorf("acme: finalize: %w", err)
	}
	for order.Status != "valid" {
		if order.Status == "invalid" {
			return nil, errors.New("acme: order invalid")
		}
		if err := sleep(ctx, time.Second); err != nil {
			return nil, err
		}
		if _, err := m.post(ctx, orderURL, nil, &order); err != nil {
			return nil, err
		}
	}
	chain, err := m.download(ctx, order.Certificate)
	if err != nil {
		return nil, err
	}
	return certificate(chain, key)
}

// authorize proves the domain of an authorization with its http-01
// challenge.
func (m *Manager) authorize(ctx context.Context, url string) error {
	var authz struct {
		Status     string `json:"status"`
		Challenges []struct {
			Type, URL, Token string
		} `json:"challenges"`
	}
	if _, err := m.post(ctx, url, nil, &authz); err != nil {
		return fmt.Errorf("acme: authorization: %w", err)
	}
	if authz.Status == "valid" {
		return nil
	}
	for _, ch := range authz.Challenges {
		if ch.Type != "http-01" {
			continue
		}
		thumb, err := thumbprint(&m.accountKey.PublicKey)
		if err != nil {
			return err
		}
		m.mu.Lock()
		if m.challenges == nil {
			m.challenges = make(map[string]string)
		}
		m.challenges[ch.Token] = ch.Token + "." + thumb
		m.mu.Unlock()
		defer func() {
			m.mu.Lock()
			delete(m.challenges, ch.Token)
			m.mu.Unlock()
		}()
		if _, err := m.post(ctx, ch.URL, struct{}{}, nil); err != nil {
			return fmt.Errorf("acme: challenge: %w", err)
		}
		for authz.Status != "valid" {
			if authz.Status == "invalid" {
				return errors.New("acme: the CA could not fetch the http-01 challenge on port 80 of " + m.Domain)
			}
			if err := sleep(ctx, time.Second); err != nil {
				return err
			}
			if _, err := m.post(ctx, url, nil, &authz); err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New("acme: the CA offered no http-01 challenge")
}

// ensureAccount loads the directory and registers or finds the account.
func (m *Manager) ensureAccount(ctx context.Context) error {
	if m.account != "" {
		return nil
	}
	if m.dir == nil {
		url := m.Directory
		if url == "" {
			url = LetsEncrypt
		}
		var d directory
		if err := m.getJSON(ctx, url, &d); err != nil {
			return fmt.Errorf("acme: directory: %w", err)
		}
		m.dir = &d
	}
	if m.accountKey == nil {
		key, err := m.loadAccountKey()
		if err != nil {
			return err
		}
		m.accountKey = key
	}
	payload := map[string]any{"termsOfServiceAgreed": true}
	if m.Email != "" {
		payload["contact"] = []string{"mailto:" + m.Email}
	}
	account, err := m.post(ctx, m.dir.NewAccount, payload, nil)
	if err != nil {
		return fmt.Errorf("acme: account: %w", err)
	}
	if account == "" {
		return errors.New("acme: account without a location")
	}
	m.account = account
	return nil
}

// post sends a JWS of payload to url, POST-as-GET for a nil payload, and
// decodes the JSON response into out. It returns the Location of the
// response.
func (m *Manager) post(ctx context.Context, url string, payload, out any) (string, error) {
	for attempt := 0; ; attempt++ {
		nonce, err := m.takeNonce(ctx)
		if err != nil {
			return "", err
		}
		body, err := m.jws(url, nonce, payload)
		if err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/jose+json")
		resp, err := m.client().Do(req)
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if n := resp.Header.Get("Replay-Nonce"); n != "" {
			m.nonce = n
		}
		if err != nil {
			return "", err
		}
		if resp.StatusCode >= 400 {
			var problem struct{ Type, Detail string }
			_ = json.Unmarshal(data, &problem)
			if problem.Type == "urn:ietf:params:acme:error:badNonce" && attempt < 3 {
				continue
			}
			return "", fmt.Errorf("status %d: %s %s", resp.StatusCode, problem.Type, problem.Detail)
		}
		if out != nil && len(data) > 0 && strings.Contains(resp.Header.Get("Content-Type"), "json") {
			if err := json.Unmarshal(data, out); err != nil {
				return "", err
			}
		}
		if raw, ok := out.(*[]byte); ok {
			*raw = data
		}
		return resp.Header.Get("Location"), nil
	}
}

// download returns the PEM certificate chain at url.
func (m *Manager) download(ctx context.Context, url string) ([]byte, error) {
	var chain []byte
	if _, err := m.post(ctx, url, nil, &chain); err != nil {
		return nil, fmt.Errorf("acme: certificate: %w", err)
	}
	return chain, nil
}

// takeNonce returns a fresh nonce: the last response's, or a new one.
func (m *Manager) takeNonce(ctx context.Context) (string, error) {
	if n := m.nonce; n != "" {
		m.nonce = ""
		return n, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, m.dir.NewNonce, nil)
	if err != nil {
		return "", err
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	n := resp.Header.Get("Replay-Nonce")
	if n == "" {
		return "", errors.New("acme: no nonce")
	}
	return n, nil
}

// jws returns the flattened JWS of payload for url, signed with the key of
// the account with ES256: with its JWK before the account exists, its URL
// after.
func (m *Manager) jws(url, nonce string, payload any) ([]byte, error) {
	header := map[string]any{"alg": "ES256", "nonce": nonce, "url": url}
	if m.account == "" {
		header["jwk"] = jwk(&m.accountKey.PublicKey)
	} else {
		header["kid"] = m.account
	}
	h, err := json.Marshal(header)
	if err != nil {
		return nil, err
	}
	p := ""
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		p = b64(b)
	}
	signed := b64(h) + "." + p
	sum := sha256.Sum256([]byte(signed))
	r, s, err := ecdsa.Sign(rand.Reader, m.accountKey, sum[:])
	if err != nil {
		return nil, err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return json.Marshal(map[string]string{"protected": b64(h), "payload": p, "signature": b64(sig)})
}

func (m *Manager) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := m.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

func (m *Manager) client() *http.Client {
	if m.HTTPClient != nil {
		return m.HTTPClient
	}
	return http.DefaultClient
}

// jwk returns the JWK of a P-256 key, its members in the order RFC 7638
// orders them for the thumbprint.
func jwk(pub *ecdsa.PublicKey) map[string]string {
	return map[string]string{"crv": "P-256", "kty": "EC", "x": b64(pad32(pub.X)), "y": b64(pad32(pub.Y))}
}

// thumbprint returns the JWK thumbprint of a key (RFC 7638), which a key
// authorization names the account by.
func thumbprint(pub *ecdsa.PublicKey) (string, error) {
	k := jwk(pub)
	b := fmt.Sprintf(`{"crv":%q,"kty":%q,"x":%q,"y":%q}`, k["crv"], k["kty"], k["x"], k["y"])
	sum := sha256.Sum256([]byte(b))
	return b64(sum[:]), nil
}

func pad32(n *big.Int) []byte { return n.FillBytes(make([]byte, 32)) }

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// certificate builds a TLS certificate of a PEM chain and its key.
func certificate(chain []byte, key crypto.Signer) (*tls.Certificate, error) {
	var c tls.Certificate
	for {
		var block *pem.Block
		block, chain = pem.Decode(chain)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			c.Certificate = append(c.Certificate, block.Bytes)
		}
	}
	if len(c.Certificate) == 0 {
		return nil, errors.New("acme: no certificate in the chain")
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil {
		return nil, err
	}
	c.Leaf, c.PrivateKey = leaf, key
	return &c, nil
}

// The files of the cache: the key of the account and the certificate with
// its key, all readable by the owner alone.
const (
	accountFile = "account.key"
	certFile    = "certificate.pem"
)

func (m *Manager) loadAccountKey() (*ecdsa.PrivateKey, error) {
	if m.CacheDir != "" {
		if b, err := os.ReadFile(filepath.Join(m.CacheDir, accountFile)); err == nil {
			if block, _ := pem.Decode(b); block != nil {
				return x509.ParseECPrivateKey(block.Bytes)
			}
			return nil, errors.New("acme: the cached account key is not PEM")
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	if m.CacheDir != "" {
		der, err := x509.MarshalECPrivateKey(key)
		if err != nil {
			return nil, err
		}
		if err := writePrivate(filepath.Join(m.CacheDir, accountFile), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})); err != nil {
			return nil, err
		}
	}
	return key, nil
}

func (m *Manager) loadCert() (*tls.Certificate, error) {
	if m.CacheDir == "" {
		return nil, os.ErrNotExist
	}
	b, err := os.ReadFile(filepath.Join(m.CacheDir, certFile))
	if err != nil {
		return nil, err
	}
	c, err := tls.X509KeyPair(b, b)
	if err != nil {
		return nil, err
	}
	if c.Leaf == nil {
		if c.Leaf, err = x509.ParseCertificate(c.Certificate[0]); err != nil {
			return nil, err
		}
	}
	if err := c.Leaf.VerifyHostname(m.Domain); err != nil {
		return nil, err
	}
	return &c, nil
}

func (m *Manager) storeCert(c *tls.Certificate) error {
	if m.CacheDir == "" {
		return nil
	}
	var b bytes.Buffer
	for _, der := range c.Certificate {
		_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: der})
	}
	key, ok := c.PrivateKey.(*ecdsa.PrivateKey)
	if !ok {
		return errors.New("acme: the key of the certificate is not ECDSA")
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	_ = pem.Encode(&b, &pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	return writePrivate(filepath.Join(m.CacheDir, certFile), b.Bytes())
}

// writePrivate writes a file only its owner reads, replacing the old one
// at once.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".acme-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
