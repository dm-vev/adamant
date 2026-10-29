// Package java speaks the protocol of Minecraft: Java Edition over TCP:
// its frames (a VarInt length, then the packet, compressed with zlib past
// a threshold once SetCompression was sent, encrypted with AES-128-CFB8
// once encryption began), the handshake that picks the state of a
// connection, and the status a server list shows. A Listener answers the
// status of a server and hands logins to the server.
package java

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"unicode/utf8"
)

// Limits of a connection.
const (
	// MaxPacket bounds a packet, as the game bounds it: 2 MiB.
	MaxPacket = 2 << 20
	// maxString bounds a string of a packet, in UTF-16 units as the game
	// counts them, times three bytes.
	maxString = 32767 * 3
)

var (
	errVarInt = errors.New("java: VarInt longer than 5 bytes")
	errFrame  = errors.New("java: malformed frame")
)

// Conn is a connection of the Java protocol: it reads and writes packets,
// each an ID and its fields.
type Conn struct {
	conn net.Conn
	r    *bufio.Reader

	writeMu   sync.Mutex
	threshold int // -1 while compression is off
	encrypt   cipher.Stream
	decrypt   cipher.Stream
}

// NewConn returns a Conn over c, without compression or encryption.
func NewConn(c net.Conn) *Conn {
	return &Conn{conn: c, r: bufio.NewReaderSize(c, 16<<10), threshold: -1}
}

// SetCompression turns compression on for packets of threshold bytes or
// more, after the SetCompression packet of the login went out; a negative
// threshold turns it off.
func (c *Conn) SetCompression(threshold int) {
	c.writeMu.Lock()
	c.threshold = threshold
	c.writeMu.Unlock()
}

// EnableEncryption encrypts both ways with AES-128-CFB8 under the shared
// secret of the login, which is also the IV.
func (c *Conn) EnableEncryption(secret []byte) error {
	if len(secret) != 16 {
		return errors.New("java: the shared secret is not 16 bytes")
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	c.encrypt = newCFB8(block, secret, false)
	c.writeMu.Unlock()
	c.decrypt = newCFB8(block, secret, true)
	c.r = bufio.NewReaderSize(cipher.StreamReader{S: c.decrypt, R: c.conn}, 16<<10)
	return nil
}

// ReadPacket reads the next packet: its ID and its fields.
func (c *Conn) ReadPacket() (int32, []byte, error) {
	size, err := readVarInt(c.r)
	if err != nil {
		return 0, nil, err
	}
	if size <= 0 || size > MaxPacket {
		return 0, nil, errFrame
	}
	frame := make([]byte, size)
	if _, err := io.ReadFull(c.r, frame); err != nil {
		return 0, nil, err
	}
	c.writeMu.Lock()
	compressed := c.threshold >= 0
	threshold := c.threshold
	c.writeMu.Unlock()
	if compressed {
		r := bytes.NewReader(frame)
		dataLength, err := readVarInt(r)
		if err != nil || dataLength < 0 || dataLength > MaxPacket {
			return 0, nil, errFrame
		}
		rest := frame[len(frame)-r.Len():]
		if dataLength == 0 {
			frame = rest
		} else {
			if int(dataLength) < threshold {
				return 0, nil, errFrame
			}
			z, err := zlib.NewReader(bytes.NewReader(rest))
			if err != nil {
				return 0, nil, errFrame
			}
			frame = make([]byte, dataLength)
			if _, err := io.ReadFull(z, frame); err != nil {
				return 0, nil, errFrame
			}
			if n, _ := z.Read(make([]byte, 1)); n != 0 {
				return 0, nil, errFrame
			}
		}
	}
	r := bytes.NewReader(frame)
	id, err := readVarInt(r)
	if err != nil {
		return 0, nil, errFrame
	}
	return id, frame[len(frame)-r.Len():], nil
}

// WritePacket writes a packet of an ID and its fields.
func (c *Conn) WritePacket(id int32, fields []byte) error {
	body := appendVarInt(nil, id)
	body = append(body, fields...)
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	var frame []byte
	switch {
	case c.threshold < 0:
		frame = append(appendVarInt(nil, int32(len(body))), body...)
	case len(body) < c.threshold:
		inner := append([]byte{0}, body...)
		frame = append(appendVarInt(nil, int32(len(inner))), inner...)
	default:
		var z bytes.Buffer
		w := zlib.NewWriter(&z)
		_, _ = w.Write(body)
		_ = w.Close()
		inner := append(appendVarInt(nil, int32(len(body))), z.Bytes()...)
		frame = append(appendVarInt(nil, int32(len(inner))), inner...)
	}
	if c.encrypt != nil {
		c.encrypt.XORKeyStream(frame, frame)
	}
	_, err := c.conn.Write(frame)
	return err
}

// Close closes the connection.
func (c *Conn) Close() error { return c.conn.Close() }

// RemoteAddr returns the address of the peer.
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// readVarInt reads a VarInt: seven bits a byte, least significant first.
func readVarInt(r io.ByteReader) (int32, error) {
	var v uint32
	for i := 0; i < 5; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		v |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(v), nil
		}
	}
	return 0, errVarInt
}

// appendVarInt appends a VarInt.
func appendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

// Reader reads the fields of a packet, failing softly: the first error
// sticks and later reads return zero values.
type Reader struct {
	b   []byte
	err error
}

// NewReader returns a Reader of the fields of a packet.
func NewReader(b []byte) *Reader { return &Reader{b: b} }

// Err returns the first error of the reads.
func (r *Reader) Err() error { return r.err }

// Len returns the bytes left.
func (r *Reader) Len() int { return len(r.b) }

func (r *Reader) fail() {
	if r.err == nil {
		r.err = errFrame
	}
	r.b = nil
}

// VarInt reads a VarInt.
func (r *Reader) VarInt() int32 {
	var v uint32
	for i := 0; i < 5; i++ {
		if len(r.b) == 0 {
			r.fail()
			return 0
		}
		c := r.b[0]
		r.b = r.b[1:]
		v |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return int32(v)
		}
	}
	r.fail()
	return 0
}

// String reads a string: a VarInt length in bytes, then UTF-8.
func (r *Reader) String() string {
	n := r.VarInt()
	if n < 0 || n > maxString || int(n) > len(r.b) {
		r.fail()
		return ""
	}
	s := string(r.b[:n])
	r.b = r.b[n:]
	if !utf8.ValidString(s) {
		r.fail()
		return ""
	}
	return s
}

// Uint16 reads a big-endian unsigned short.
func (r *Reader) Uint16() uint16 {
	if len(r.b) < 2 {
		r.fail()
		return 0
	}
	v := binary.BigEndian.Uint16(r.b)
	r.b = r.b[2:]
	return v
}

// Int64 reads a big-endian long.
func (r *Reader) Int64() int64 {
	if len(r.b) < 8 {
		r.fail()
		return 0
	}
	v := int64(binary.BigEndian.Uint64(r.b))
	r.b = r.b[8:]
	return v
}

// UUID reads a UUID: two big-endian longs.
func (r *Reader) UUID() [16]byte {
	var u [16]byte
	if len(r.b) < 16 {
		r.fail()
		return u
	}
	copy(u[:], r.b)
	r.b = r.b[16:]
	return u
}

// AppendString appends a string with its VarInt length.
func AppendString(b []byte, s string) []byte {
	return append(appendVarInt(b, int32(len(s))), s...)
}

// AppendVarInt appends a VarInt.
func AppendVarInt(b []byte, v int32) []byte { return appendVarInt(b, v) }

// cfb8 is AES in CFB mode of 8-bit segments, as the protocol encrypts:
// each byte goes through the block cipher alone.
type cfb8 struct {
	block   cipher.Block
	iv, tmp []byte
	decrypt bool
}

func newCFB8(block cipher.Block, iv []byte, decrypt bool) *cfb8 {
	return &cfb8{block: block, iv: bytes.Clone(iv), tmp: make([]byte, block.BlockSize()), decrypt: decrypt}
}

func (c *cfb8) XORKeyStream(dst, src []byte) {
	for i := range src {
		c.block.Encrypt(c.tmp, c.iv)
		in := src[i]
		out := in ^ c.tmp[0]
		dst[i] = out
		copy(c.iv, c.iv[1:])
		if c.decrypt {
			c.iv[len(c.iv)-1] = in
		} else {
			c.iv[len(c.iv)-1] = out
		}
	}
}

// fmtErr wraps an error of a connection with its state.
func fmtErr(state string, err error) error { return fmt.Errorf("java: %s: %w", state, err) }
