package login

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strings"
	"time"
)

// Algorithms of the signatures of a login: clients sign with P-384 keys,
// the authorization service with RSA keys.
const (
	es384 = "ES384"
	rs256 = "RS256"
)

// errSignature is returned for a signature that does not verify.
var errSignature = errors.New("login: signature does not verify")

// jws is a JWS in compact form: a header, a payload and the signature of
// both.
type jws struct {
	alg, kid, x5u string
	payload       []byte
	// signed is what the signature signs: the header and the payload as
	// they were sent.
	signed string
	sig    []byte
}

// parseJWS splits a compact JWS signed with one of algs. Its signature is
// not checked yet.
func parseJWS(s string, algs ...string) (*jws, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("login: JWS of %d parts", len(parts))
	}
	header, err := b64decode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("login: JWS header: %w", err)
	}
	m, err := object(header)
	if err != nil {
		return nil, fmt.Errorf("login: JWS header: %w", err)
	}
	var t jws
	if err := members(m, "alg", &t.alg, "kid", &t.kid, "x5u", &t.x5u); err != nil {
		return nil, fmt.Errorf("login: JWS header: %w", err)
	}
	if _, ok := m["crit"]; ok {
		return nil, errors.New("login: JWS with critical extensions")
	}
	if !slices.Contains(algs, t.alg) {
		return nil, fmt.Errorf("login: JWS signed with %q", t.alg)
	}
	if t.payload, err = b64decode(parts[1]); err != nil {
		return nil, fmt.Errorf("login: JWS payload: %w", err)
	}
	if t.sig, err = b64decode(parts[2]); err != nil {
		return nil, fmt.Errorf("login: JWS signature: %w", err)
	}
	t.signed = s[:len(parts[0])+1+len(parts[1])]
	return &t, nil
}

// b64decode decodes base64url, padded or not, as go-jose takes it.
func b64decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// verify checks the signature of the JWS with a key: an ES384 one with a
// P-384 key, an RS256 one with an RSA key.
func (t *jws) verify(key crypto.PublicKey) error {
	switch t.alg {
	case es384:
		pub, ok := key.(*ecdsa.PublicKey)
		if !ok || pub.Curve != elliptic.P384() {
			return errors.New("login: ES384 signature without a P-384 key")
		}
		// The signature is r and s, 48 bytes each.
		if len(t.sig) != 96 {
			return errSignature
		}
		sum := sha512.Sum384([]byte(t.signed))
		if !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(t.sig[:48]), new(big.Int).SetBytes(t.sig[48:])) {
			return errSignature
		}
		return nil
	case rs256:
		pub, ok := key.(*rsa.PublicKey)
		if !ok {
			return errors.New("login: RS256 signature without an RSA key")
		}
		sum := sha256.Sum256([]byte(t.signed))
		if rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], t.sig) != nil {
			return errSignature
		}
		return nil
	}
	return fmt.Errorf("login: JWS signed with %q", t.alg)
}

// object reads a JSON object into its members by name. As go-jose reads
// the objects of JOSE, a name matches only itself, and a name given twice
// is an error, so that no two readers of a token read it differently.
func object(b []byte) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("login: JSON is not an object")
	}
	m := make(map[string]json.RawMessage)
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		name := t.(string)
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		if _, ok := m[name]; ok {
			return nil, fmt.Errorf("login: JSON names %q twice", name)
		}
		m[name] = v
	}
	if _, err := d.Token(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("login: data after a JSON object")
	}
	return m, nil
}

// members reads members of an object into values, a name followed by the
// value it goes in; a value stays as it is for a member the object lacks.
func members(m map[string]json.RawMessage, pairs ...any) error {
	for i := 0; i < len(pairs); i += 2 {
		name := pairs[i].(string)
		raw, ok := m[name]
		if !ok {
			continue
		}
		if err := json.Unmarshal(raw, pairs[i+1]); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	return nil
}

// date is a time of a JWT, in seconds since the epoch. A fraction is
// dropped and a number in a string is an error, as go-jose reads it.
type date int64

func (d *date) UnmarshalJSON(b []byte) error {
	var f json.Number
	if err := json.Unmarshal(b, &f); err != nil || len(b) == 0 || b[0] == '"' {
		return errors.New("login: time of a JWT is not a number")
	}
	if n, err := f.Int64(); err == nil {
		*d = date(n)
		return nil
	}
	v, err := f.Float64()
	if err != nil {
		return err
	}
	*d = date(v)
	return nil
}

func (d date) time() time.Time { return time.Unix(int64(d), 0) }

// audience is the audience of a JWT: one string or a list of them.
type audience []string

func (a *audience) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*a = audience{s}
		return nil
	}
	return json.Unmarshal(b, (*[]string)(a))
}

// claims are the registered claims of a JWT a login checks.
type claims struct {
	issuer                   string
	audience                 audience
	expiry, notBefore, issue *date
}

// readClaims reads the registered claims of a payload.
func readClaims(m map[string]json.RawMessage) (claims, error) {
	var c claims
	err := members(m, "iss", &c.issuer, "aud", &c.audience, "exp", &c.expiry, "nbf", &c.notBefore, "iat", &c.issue)
	return c, err
}

// leeway is the error the clocks of a server and of whoever signed a JWT
// may have between them, as go-jose allows it.
const leeway = time.Minute

// valid checks the times of the claims against now.
func (c claims) valid(now time.Time) error {
	switch {
	case c.notBefore != nil && now.Add(leeway).Before(c.notBefore.time()):
		return errors.New("login: JWT not valid yet")
	case c.expiry != nil && now.Add(-leeway).After(c.expiry.time()):
		return errors.New("login: JWT expired")
	case c.issue != nil && now.Add(leeway).Before(c.issue.time()):
		return errors.New("login: JWT issued in the future")
	}
	return nil
}
