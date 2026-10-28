package nethernet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha512"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Fingerprint is a DTLS certificate fingerprint of a description.
type Fingerprint struct {
	Algorithm string
	Digest    string
}

// fingerprintPayload encodes fingerprints as the canonical JSON the
// identity assertion signs: sorted keys, no whitespace.
func fingerprintPayload(fps []Fingerprint) []byte {
	var b strings.Builder
	b.WriteString(`{"fingerprint":[`)
	for i, fp := range fps {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"algorithm":`)
		b.WriteString(strconv.Quote(fp.Algorithm))
		b.WriteString(`,"digest":`)
		b.WriteString(strconv.Quote(fp.Digest))
		b.WriteByte('}')
	}
	b.WriteString(`]}`)
	return []byte(b.String())
}

var b64 = base64.RawURLEncoding

// signES384 signs data with a P-384 key as JWS ES384 does: the raw r and s.
func signES384(key *ecdsa.PrivateKey, data []byte) ([]byte, error) {
	sum := sha512.Sum384(data)
	r, s, err := ecdsa.Sign(rand.Reader, key, sum[:])
	if err != nil {
		return nil, err
	}
	sig := make([]byte, 96)
	r.FillBytes(sig[:48])
	s.FillBytes(sig[48:])
	return sig, nil
}

func verifyES384(key *ecdsa.PublicKey, data, sig []byte) bool {
	if len(sig) != 96 || key.Curve != elliptic.P384() {
		return false
	}
	sum := sha512.Sum384(data)
	r := new(big.Int).SetBytes(sig[:48])
	s := new(big.Int).SetBytes(sig[48:])
	return ecdsa.Verify(key, sum[:], r, s)
}

// esHeader is the protected header of the fingerprint assertions.
const esHeader = `{"alg":"ES384"}`

// signFingerprints returns the detached JWS of the fingerprints: the
// header, an empty payload and the signature.
func signFingerprints(key *ecdsa.PrivateKey, fps []Fingerprint) (string, error) {
	header := b64.EncodeToString([]byte(esHeader))
	sig, err := signES384(key, []byte(header+"."+b64.EncodeToString(fingerprintPayload(fps))))
	if err != nil {
		return "", err
	}
	return header + ".." + b64.EncodeToString(sig), nil
}

// verifyFingerprints checks a detached JWS of the fingerprints.
func verifyFingerprints(key *ecdsa.PublicKey, jws string, fps []Fingerprint) error {
	parts := strings.Split(jws, ".")
	if len(parts) != 3 || parts[1] != "" {
		return errors.New("nethernet: malformed fingerprint assertion")
	}
	header, err := b64.DecodeString(parts[0])
	if err != nil {
		return errors.New("nethernet: malformed fingerprint assertion header")
	}
	var h struct {
		Alg string `json:"alg"`
	}
	if json.Unmarshal(header, &h) != nil || h.Alg != "ES384" {
		return fmt.Errorf("nethernet: fingerprint assertion signed with %q", h.Alg)
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil {
		return errors.New("nethernet: malformed fingerprint assertion signature")
	}
	if !verifyES384(key, []byte(parts[0]+"."+b64.EncodeToString(fingerprintPayload(fps))), sig) {
		return errors.New("nethernet: fingerprint assertion does not verify")
	}
	return nil
}

// identityEnvelope is the JSON of an a=identity attribute.
type identityEnvelope struct {
	Assertion string `json:"assertion"`
	IDP       struct {
		Domain   string `json:"domain"`
		Protocol string `json:"protocol"`
	} `json:"idp"`
}

type identityAssertion struct {
	Fingerprints string `json:"fingerprints"`
	Token        string `json:"token"`
}

// encodeIdentity encodes the a=identity attribute of a description.
func encodeIdentity(domain, token, fingerprints string) (string, error) {
	assertion, err := json.Marshal(identityAssertion{Fingerprints: fingerprints, Token: token})
	if err != nil {
		return "", err
	}
	var e identityEnvelope
	e.Assertion = string(assertion)
	e.IDP.Domain = domain
	e.IDP.Protocol = "default"
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// decodeIdentity decodes an a=identity attribute into its token and its
// fingerprint assertion.
func decodeIdentity(attr string) (token, fingerprints string, err error) {
	raw, err := base64.StdEncoding.DecodeString(attr)
	if err != nil {
		return "", "", errors.New("nethernet: identity is not base64")
	}
	var e identityEnvelope
	if err := json.Unmarshal(raw, &e); err != nil || e.IDP.Protocol != "default" {
		return "", "", errors.New("nethernet: malformed identity")
	}
	var a identityAssertion
	if err := json.Unmarshal([]byte(e.Assertion), &a); err != nil || a.Token == "" || a.Fingerprints == "" {
		return "", "", errors.New("nethernet: malformed identity assertion")
	}
	return a.Token, a.Fingerprints, nil
}

// tokenClaims are the claims of an identity token this package reads.
type tokenClaims struct {
	CPK json.RawMessage `json:"cpk"`
	Exp int64           `json:"exp"`
	Nbf int64           `json:"nbf"`
}

// tokenKey returns the public key of the cpk claim of a JWT and its
// expiry. The signature is not checked here: a client token is issued by
// Minecraft's authorization service and checked with the Login, whose key
// must be the same.
func tokenKey(token string, now time.Time) (*ecdsa.PublicKey, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, errors.New("nethernet: malformed token")
	}
	payload, err := b64.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return nil, errors.New("nethernet: malformed token payload")
	}
	var c tokenClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, errors.New("nethernet: malformed token claims")
	}
	if c.Exp != 0 && now.Unix() > c.Exp+60 || c.Nbf != 0 && now.Unix() < c.Nbf-60 {
		return nil, errors.New("nethernet: token expired or not yet valid")
	}
	return parseCPK(c.CPK)
}

// parseCPK decodes a cpk claim: a JWK in clients since 26.40, a base64 DER
// public key before.
func parseCPK(raw json.RawMessage) (*ecdsa.PublicKey, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		der, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			return nil, errors.New("nethernet: cpk is not base64")
		}
		pub, err := x509.ParsePKIXPublicKey(der)
		if err != nil {
			return nil, fmt.Errorf("nethernet: parse cpk: %w", err)
		}
		key, ok := pub.(*ecdsa.PublicKey)
		if !ok {
			return nil, errors.New("nethernet: cpk is not an ECDSA key")
		}
		return key, nil
	}
	var jwk struct {
		Kty string `json:"kty"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	if err := json.Unmarshal(raw, &jwk); err != nil || jwk.Kty != "EC" || jwk.Crv != "P-384" {
		return nil, errors.New("nethernet: cpk is not a P-384 key")
	}
	x, errX := b64.DecodeString(jwk.X)
	y, errY := b64.DecodeString(jwk.Y)
	if errX != nil || errY != nil || len(x) != 48 || len(y) != 48 {
		return nil, errors.New("nethernet: malformed cpk coordinates")
	}
	key, err := ecdsa.ParseUncompressedPublicKey(elliptic.P384(), append(append([]byte{4}, x...), y...))
	if err != nil {
		return nil, fmt.Errorf("nethernet: cpk: %w", err)
	}
	return key, nil
}

// serverToken returns the self-signed identity token of a server, as BDS
// issues it: the public key in the x5u header and as a JWK in the cpk
// claim, valid for a minute.
func serverToken(key *ecdsa.PrivateKey, now time.Time) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	header := fmt.Sprintf(`{"alg":"ES384","x5u":%q}`+"\n", base64.StdEncoding.EncodeToString(der))
	point, err := key.PublicKey.Bytes()
	if err != nil {
		return "", err
	}
	x, y := point[1:49], point[49:97]
	payload := fmt.Sprintf(`{"cpk":{"crv":"P-384","kty":"EC","x":%q,"y":%q},"exp":%d,"iat":%d}`+"\n",
		b64.EncodeToString(x), b64.EncodeToString(y), now.Add(time.Minute).Unix(), now.Unix())
	signing := b64.EncodeToString([]byte(header)) + "." + b64.EncodeToString([]byte(payload))
	sig, err := signES384(key, []byte(signing))
	if err != nil {
		return "", err
	}
	return signing + "." + b64.EncodeToString(sig), nil
}

// clientToken returns a self-signed token for an offline client, with the
// public key as a base64 DER cpk claim, as offline Bedrock clients send.
func clientToken(key *ecdsa.PrivateKey, name string, now time.Time) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(der)
	header := fmt.Sprintf(`{"alg":"ES384","x5u":%q}`, encoded)
	payload := fmt.Sprintf(`{"cpk":%q,"exp":%d,"nbf":%d,"xname":%q}`, encoded, now.Add(12*time.Hour).Unix(), now.Add(-time.Minute).Unix(), name)
	signing := b64.EncodeToString([]byte(header)) + "." + b64.EncodeToString([]byte(payload))
	sig, err := signES384(key, []byte(signing))
	if err != nil {
		return "", err
	}
	return signing + "." + b64.EncodeToString(sig), nil
}
