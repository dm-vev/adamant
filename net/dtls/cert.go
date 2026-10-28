package dtls

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Certificate is a certificate chain and the key of its leaf. WebRTC peers
// authenticate with self-signed certificates whose fingerprints the
// signaling carries.
type Certificate struct {
	// Chain holds the DER encoded certificates, leaf first.
	Chain [][]byte
	// Key is the private key of the leaf, an ECDSA P-256 key.
	Key *ecdsa.PrivateKey
}

// GenerateCertificate creates a self-signed ECDSA P-256 certificate valid
// for a year, as WebRTC implementations do for each peer connection.
func GenerateCertificate() (Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 63))
	if err != nil {
		return Certificate{}, err
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "WebRTC"},
		NotBefore:    now.Add(-24 * time.Hour),
		NotAfter:     now.Add(365 * 24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return Certificate{}, err
	}
	return Certificate{Chain: [][]byte{der}, Key: key}, nil
}

// Fingerprint returns the SHA-256 fingerprint of a DER certificate as SDP
// carries it: upper case hexadecimal bytes separated by colons.
func Fingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	return formatFingerprint(sum[:])
}

func formatFingerprint(sum []byte) string {
	s := strings.ToUpper(hex.EncodeToString(sum))
	var b strings.Builder
	for i := 0; i < len(s); i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(s[i : i+2])
	}
	return b.String()
}

// MatchFingerprint reports whether a DER certificate has the fingerprint
// passed, computed with the SDP hash function name algorithm.
func MatchFingerprint(der []byte, algorithm, fingerprint string) bool {
	var sum []byte
	switch strings.ToLower(algorithm) {
	case "sha-256":
		s := sha256.Sum256(der)
		sum = s[:]
	case "sha-384":
		s := sha512.Sum384(der)
		sum = s[:]
	case "sha-512":
		s := sha512.Sum512(der)
		sum = s[:]
	default:
		return false
	}
	return strings.EqualFold(formatFingerprint(sum), fingerprint)
}

var errSignature = errors.New("dtls: invalid signature")

// sign signs message with the certificate key under scheme, which must be
// an ECDSA scheme.
func sign(key *ecdsa.PrivateKey, scheme uint16, message []byte) ([]byte, error) {
	h, ok := schemeHash(scheme)
	if !ok || !isECDSA(scheme) {
		return nil, fmt.Errorf("dtls: cannot sign with scheme %#04x", scheme)
	}
	d := h.New()
	d.Write(message)
	return ecdsa.SignASN1(rand.Reader, key, d.Sum(nil))
}

// verify checks a signature of message by the key of a certificate.
func verify(pub crypto.PublicKey, scheme uint16, message, sig []byte) error {
	h, ok := schemeHash(scheme)
	if !ok {
		return errSignature
	}
	d := h.New()
	d.Write(message)
	digest := d.Sum(nil)
	switch key := pub.(type) {
	case *ecdsa.PublicKey:
		if isECDSA(scheme) && ecdsa.VerifyASN1(key, digest, sig) {
			return nil
		}
	case *rsa.PublicKey:
		switch scheme {
		case sigRSAPKCS1SHA256, sigRSAPKCS1SHA384, sigRSAPKCS1SHA512:
			if rsa.VerifyPKCS1v15(key, h, digest, sig) == nil {
				return nil
			}
		case sigRSAPSSSHA256, sigRSAPSSSHA384, sigRSAPSSSHA512:
			if rsa.VerifyPSS(key, h, digest, sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}) == nil {
				return nil
			}
		}
	}
	return errSignature
}

func isECDSA(scheme uint16) bool {
	return scheme == sigECDSAP256SHA256 || scheme == sigECDSAP384SHA384 || scheme == sigECDSAP521SHA512
}

func schemeHash(scheme uint16) (crypto.Hash, bool) {
	switch scheme {
	case sigECDSAP256SHA256, sigRSAPKCS1SHA256, sigRSAPSSSHA256:
		return crypto.SHA256, true
	case sigECDSAP384SHA384, sigRSAPKCS1SHA384, sigRSAPSSSHA384:
		return crypto.SHA384, true
	case sigECDSAP521SHA512, sigRSAPKCS1SHA512, sigRSAPSSSHA512:
		return crypto.SHA512, true
	}
	return 0, false
}

// chooseScheme returns the first ECDSA scheme of offered this package can
// sign with, preferring SHA-256 for its P-256 key.
func chooseScheme(offered []uint16) (uint16, bool) {
	for _, want := range []uint16{sigECDSAP256SHA256, sigECDSAP384SHA384, sigECDSAP521SHA512} {
		for _, s := range offered {
			if s == want {
				return s, true
			}
		}
	}
	return 0, false
}

// peerPublicKey parses the leaf of a peer chain.
func peerPublicKey(chain [][]byte) (crypto.PublicKey, error) {
	if len(chain) == 0 {
		return nil, errors.New("dtls: peer sent no certificate")
	}
	cert, err := x509.ParseCertificate(chain[0])
	if err != nil {
		return nil, fmt.Errorf("dtls: parse peer certificate: %w", err)
	}
	return cert.PublicKey, nil
}
