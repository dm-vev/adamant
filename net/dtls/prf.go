package dtls

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"hash"
)

// cipherSuite is a cipher suite this package speaks: ECDHE key exchange,
// ECDSA authentication and AES-GCM record protection.
type cipherSuite struct {
	id     uint16
	keyLen int
	// hash is the hash of the PRF and of the Finished messages.
	hash func() hash.Hash
}

const (
	suiteECDHEECDSAAES128GCMSHA256 = 0xc02b
	suiteECDHEECDSAAES256GCMSHA384 = 0xc02c
)

// cipherSuites lists the suites this package speaks in order of preference.
var cipherSuites = []cipherSuite{
	{id: suiteECDHEECDSAAES128GCMSHA256, keyLen: 16, hash: sha256.New},
	{id: suiteECDHEECDSAAES256GCMSHA384, keyLen: 32, hash: sha512.New384},
}

func suiteByID(id uint16) *cipherSuite {
	for i := range cipherSuites {
		if cipherSuites[i].id == id {
			return &cipherSuites[i]
		}
	}
	return nil
}

// prf fills out with the TLS 1.2 PRF of RFC 5246 section 5: P_hash keyed
// with secret over the label and seeds.
func prf(newHash func() hash.Hash, out, secret []byte, label string, seeds ...[]byte) {
	mac := hmac.New(newHash, secret)
	write := func(a []byte) {
		mac.Write(a)
		mac.Write([]byte(label))
		for _, s := range seeds {
			mac.Write(s)
		}
	}
	// A(1) = HMAC(secret, label + seed).
	write(nil)
	a := mac.Sum(nil)
	for len(out) > 0 {
		mac.Reset()
		mac.Write(a)
		mac.Write([]byte(label))
		for _, s := range seeds {
			mac.Write(s)
		}
		n := copy(out, mac.Sum(nil))
		out = out[n:]
		mac.Reset()
		mac.Write(a)
		a = mac.Sum(a[:0])
	}
}

// masterSecret derives the master secret from the premaster secret: from
// the hash of the handshake up to the ClientKeyExchange with the extended
// master secret of RFC 7627, or from the randoms otherwise.
func masterSecret(suite *cipherSuite, premaster, sessionHash, clientRandom, serverRandom []byte, extended bool) [48]byte {
	var master [48]byte
	if extended {
		prf(suite.hash, master[:], premaster, "extended master secret", sessionHash)
	} else {
		prf(suite.hash, master[:], premaster, "master secret", clientRandom, serverRandom)
	}
	return master
}

// keys holds the record keys of both directions.
type keys struct {
	clientKey, serverKey []byte
	clientIV, serverIV   []byte
}

// expandKeys derives the record keys from the master secret. AEAD suites
// have no MAC keys, and a four byte implicit IV per direction.
func expandKeys(suite *cipherSuite, master, clientRandom, serverRandom []byte) keys {
	block := make([]byte, 2*suite.keyLen+8)
	prf(suite.hash, block, master, "key expansion", serverRandom, clientRandom)
	return keys{
		clientKey: block[:suite.keyLen],
		serverKey: block[suite.keyLen : 2*suite.keyLen],
		clientIV:  block[2*suite.keyLen : 2*suite.keyLen+4],
		serverIV:  block[2*suite.keyLen+4:],
	}
}

// verifyData computes the contents of a Finished message over the hash of
// the handshake so far.
func verifyData(suite *cipherSuite, master []byte, client bool, transcript []byte) []byte {
	h := suite.hash()
	h.Write(transcript)
	label := "server finished"
	if client {
		label = "client finished"
	}
	out := make([]byte, 12)
	prf(suite.hash, out, master, label, h.Sum(nil))
	return out
}
