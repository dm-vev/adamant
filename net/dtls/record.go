package dtls

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
)

// Record content types.
const (
	contentChangeCipherSpec = 20
	contentAlert            = 21
	contentHandshake        = 22
	contentApplicationData  = 23
)

// Protocol versions on the wire. DTLS versions count down: 1.2 is 0xfefd.
const (
	version10 = 0xfeff
	version12 = 0xfefd
)

// recordHeaderSize is the size of a DTLS record header.
const recordHeaderSize = 13

// Sizes of the AES-GCM record protection of RFC 5288: the explicit part of
// the nonce sent with each record and the authentication tag.
const (
	explicitNonceSize = 8
	gcmTagSize        = 16
	sealOverhead      = explicitNonceSize + gcmTagSize
)

// maxSequence is the largest record sequence number, 48 bits.
const maxSequence = 1<<48 - 1

var (
	errRecordShort = errors.New("dtls: record too short")
	errDecrypt     = errors.New("dtls: record failed authentication")
)

// recordHeader is the header of a DTLS record.
type recordHeader struct {
	typ     byte
	version uint16
	epoch   uint16
	seq     uint64
	length  int
}

// parseRecordHeader decodes the header of the record at the start of b.
func parseRecordHeader(b []byte) (recordHeader, error) {
	if len(b) < recordHeaderSize {
		return recordHeader{}, errRecordShort
	}
	h := recordHeader{
		typ:     b[0],
		version: binary.BigEndian.Uint16(b[1:]),
		epoch:   binary.BigEndian.Uint16(b[3:]),
		seq:     uint64(binary.BigEndian.Uint16(b[5:]))<<32 | uint64(binary.BigEndian.Uint32(b[7:])),
		length:  int(binary.BigEndian.Uint16(b[11:])),
	}
	if recordHeaderSize+h.length > len(b) {
		return recordHeader{}, errRecordShort
	}
	return h, nil
}

// appendRecordHeader appends the header of a record with a payload of
// length bytes.
func appendRecordHeader(dst []byte, typ byte, epoch uint16, seq uint64, length int) []byte {
	dst = append(dst, typ)
	dst = binary.BigEndian.AppendUint16(dst, version12)
	dst = binary.BigEndian.AppendUint16(dst, epoch)
	dst = binary.BigEndian.AppendUint16(dst, uint16(seq>>32))
	dst = binary.BigEndian.AppendUint32(dst, uint32(seq))
	return binary.BigEndian.AppendUint16(dst, uint16(length))
}

// cipherState protects the records of one direction of an epoch with
// AES-GCM as RFC 5288 and RFC 6347 describe.
type cipherState struct {
	aead cipher.AEAD
	iv   [4]byte
}

func newCipherState(key, iv []byte) (*cipherState, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	c := &cipherState{aead: aead}
	copy(c.iv[:], iv)
	return c, nil
}

// seal appends a protected record of type typ holding payload to dst. The
// explicit nonce is the epoch and sequence number of the record.
func (c *cipherState) seal(dst []byte, typ byte, epoch uint16, seq uint64, payload []byte) []byte {
	start := len(dst)
	dst = appendRecordHeader(dst, typ, epoch, seq, len(payload)+sealOverhead)
	var nonce [12]byte
	copy(nonce[:4], c.iv[:])
	binary.BigEndian.PutUint16(nonce[4:], epoch)
	binary.BigEndian.PutUint16(nonce[6:], uint16(seq>>32))
	binary.BigEndian.PutUint32(nonce[8:], uint32(seq))
	dst = append(dst, nonce[4:]...)
	var aad [13]byte
	copy(aad[:8], dst[start+3:start+11])
	aad[8] = typ
	binary.BigEndian.PutUint16(aad[9:], version12)
	binary.BigEndian.PutUint16(aad[11:], uint16(len(payload)))
	return c.aead.Seal(dst, nonce[:], payload, aad[:])
}

// open authenticates and decrypts the payload of a record in place and
// returns the plaintext.
func (c *cipherState) open(h recordHeader, payload []byte) ([]byte, error) {
	if len(payload) < sealOverhead {
		return nil, errDecrypt
	}
	var nonce [12]byte
	copy(nonce[:4], c.iv[:])
	copy(nonce[4:], payload[:explicitNonceSize])
	var aad [13]byte
	binary.BigEndian.PutUint16(aad[0:], h.epoch)
	binary.BigEndian.PutUint16(aad[2:], uint16(h.seq>>32))
	binary.BigEndian.PutUint32(aad[4:], uint32(h.seq))
	aad[8] = h.typ
	binary.BigEndian.PutUint16(aad[9:], h.version)
	binary.BigEndian.PutUint16(aad[11:], uint16(len(payload)-sealOverhead))
	ciphertext := payload[explicitNonceSize:]
	plain, err := c.aead.Open(ciphertext[:0], nonce[:], ciphertext, aad[:])
	if err != nil {
		return nil, errDecrypt
	}
	return plain, nil
}

// replayWindow rejects records of an epoch received before, keeping the 64
// sequence numbers below the highest one seen, as RFC 6347 section 4.1.2.6
// describes.
type replayWindow struct {
	latest uint64
	seen   uint64
	any    bool
}

// fresh reports whether a record with sequence number seq may be accepted.
func (w *replayWindow) fresh(seq uint64) bool {
	if !w.any || seq > w.latest {
		return true
	}
	diff := w.latest - seq
	return diff < 64 && w.seen&(1<<diff) == 0
}

// accept marks seq received. It must only be called for a record that
// passed authentication.
func (w *replayWindow) accept(seq uint64) {
	if !w.any {
		w.latest, w.seen, w.any = seq, 1, true
		return
	}
	if seq > w.latest {
		shift := seq - w.latest
		if shift >= 64 {
			w.seen = 0
		} else {
			w.seen <<= shift
		}
		w.seen |= 1
		w.latest = seq
		return
	}
	w.seen |= 1 << (w.latest - seq)
}
