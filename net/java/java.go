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
	"sync/atomic"
	"unicode/utf8"
)

// Limits of a connection.
const (
	// MaxPacket bounds a packet, as the game bounds it: 2 MiB.
	MaxPacket = 2 << 20
	// maxString bounds a string of a packet, in UTF-16 units as the game
	// counts them, times three bytes.
	maxString = 32767 * 3
	// maxPooledBuf bounds reusable per-connection framing buffers.
	maxPooledBuf = 64 << 10
)

var (
	errVarInt = errors.New("java: VarInt longer than 5 bytes")
	errFrame  = errors.New("java: malformed frame")
)

var zlibWriterPool = sync.Pool{
	New: func() any {
		return zlib.NewWriter(io.Discard)
	},
}

type pooledZlibReader struct {
	src   bytes.Reader
	z     io.ReadCloser
	extra [1]byte
}

var zlibReaderPool sync.Pool

// Conn is a connection of the Java protocol: it reads and writes packets,
// each an ID and its fields.
type Conn struct {
	conn    net.Conn
	r       *bufio.Reader
	scratch []byte

	threshold atomic.Int64 // -1 while compression is off

	writeMu     sync.Mutex
	encrypt     cipher.Stream
	decrypt     cipher.Stream
	writeBuf    []byte
	compressBuf bytes.Buffer
	idBuf       [5]byte
	dlBuf       [5]byte
}

// NewConn returns a Conn over c, without compression or encryption.
func NewConn(c net.Conn) *Conn {
	conn := &Conn{conn: c, r: bufio.NewReaderSize(c, 16<<10)}
	conn.threshold.Store(-1)
	return conn
}

// SetCompression turns compression on for packets of threshold bytes or
// more, after the SetCompression packet of the login went out; a negative
// threshold turns it off.
func (c *Conn) SetCompression(threshold int) {
	c.writeMu.Lock()
	c.threshold.Store(int64(threshold))
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
	threshold := int(c.threshold.Load())
	var frame []byte
	if threshold >= 0 {
		dataLength, dlLen, err := readVarIntLimited(c.r, int(size))
		if err != nil {
			return 0, nil, err
		}
		if dataLength < 0 || dataLength > MaxPacket {
			return 0, nil, errFrame
		}
		restLen := int(size) - dlLen
		if dataLength == 0 {
			frame, err = readAll(c.r, restLen)
			if err != nil {
				return 0, nil, err
			}
		} else {
			if int(dataLength) < threshold {
				return 0, nil, errFrame
			}
			rest, err := c.readScratch(restLen)
			if err != nil {
				return 0, nil, err
			}
			frame, err = decompressFrame(rest, int(dataLength))
			if err != nil {
				return 0, nil, errFrame
			}
		}
	} else {
		frame, err = readAll(c.r, int(size))
		if err != nil {
			return 0, nil, err
		}
	}
	id, n, err := decodeVarInt(frame)
	if err != nil {
		return 0, nil, errFrame
	}
	return id, frame[n:], nil
}

func (c *Conn) readScratch(n int) ([]byte, error) {
	const step = 16 << 10
	if n <= step {
		if cap(c.scratch) < n {
			c.scratch = make([]byte, n)
		}
		b := c.scratch[:n]
		_, err := io.ReadFull(c.r, b)
		return b, err
	}
	return readAll(c.r, n)
}

func decompressFrame(rest []byte, dataLength int) ([]byte, error) {
	pr, _ := zlibReaderPool.Get().(*pooledZlibReader)
	if pr == nil {
		pr = &pooledZlibReader{}
	}
	pr.src.Reset(rest)
	var err error
	if pr.z == nil {
		pr.z, err = zlib.NewReader(&pr.src)
	} else {
		err = pr.z.(zlib.Resetter).Reset(&pr.src, nil)
	}
	if err != nil {
		return nil, err
	}
	frame, err := readAll(pr.z, dataLength)
	if err != nil {
		pr.src.Reset(nil)
		zlibReaderPool.Put(pr)
		return nil, err
	}
	if n, _ := pr.z.Read(pr.extra[:]); n != 0 {
		pr.src.Reset(nil)
		zlibReaderPool.Put(pr)
		return nil, errFrame
	}
	_ = pr.z.Close()
	pr.src.Reset(nil)
	zlibReaderPool.Put(pr)
	return frame, nil
}

// readAll reads n bytes of r. It grows what it read as the bytes come
// rather than allocate n at once, so that a length no data follows costs
// the memory of the data alone.
func readAll(r io.Reader, n int) ([]byte, error) {
	const step = 16 << 10
	if n <= step {
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		return b, err
	}
	var buf bytes.Buffer
	buf.Grow(step)
	if _, err := io.CopyN(&buf, r, int64(n)); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf.Bytes(), nil
}

// WritePacket writes a packet of an ID and its fields.
func (c *Conn) WritePacket(id int32, fields []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	idBytes := appendVarInt(c.idBuf[:0], id)
	bodyLen := len(idBytes) + len(fields)

	threshold := int(c.threshold.Load())
	buf := c.writeBuf[:0]
	switch {
	case threshold < 0:
		buf = appendVarInt(buf, int32(bodyLen))
		buf = append(buf, idBytes...)
		buf = append(buf, fields...)
	case bodyLen < threshold:
		buf = appendVarInt(buf, int32(1+bodyLen))
		buf = append(buf, 0)
		buf = append(buf, idBytes...)
		buf = append(buf, fields...)
	default:
		c.compressBuf.Reset()
		w := zlibWriterPool.Get().(*zlib.Writer)
		w.Reset(&c.compressBuf)
		_, _ = w.Write(idBytes)
		_, _ = w.Write(fields)
		_ = w.Close()
		w.Reset(io.Discard)
		zlibWriterPool.Put(w)
		compressed := c.compressBuf.Bytes()
		dlBytes := appendVarInt(c.dlBuf[:0], int32(bodyLen))
		buf = appendVarInt(buf, int32(len(dlBytes)+len(compressed)))
		buf = append(buf, dlBytes...)
		buf = append(buf, compressed...)
		if c.compressBuf.Cap() > maxPooledBuf {
			c.compressBuf = bytes.Buffer{}
		}
	}
	if c.encrypt != nil {
		c.encrypt.XORKeyStream(buf, buf)
	}
	_, err := c.conn.Write(buf)
	if cap(buf) <= maxPooledBuf {
		c.writeBuf = buf[:0]
	} else {
		c.writeBuf = nil
	}
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

// readVarIntLimited reads a VarInt consuming at most limit bytes from r.
func readVarIntLimited(r io.ByteReader, limit int) (int32, int, error) {
	var v uint32
	maxBytes := min(5, limit)
	for i := range maxBytes {
		b, err := r.ReadByte()
		if err != nil {
			if err == io.EOF && i > 0 {
				return 0, 0, io.ErrUnexpectedEOF
			}
			return 0, 0, err
		}
		v |= uint32(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return int32(v), i + 1, nil
		}
	}
	return 0, 0, errFrame
}

// decodeVarInt decodes a VarInt from b, returning the value and bytes consumed.
func decodeVarInt(b []byte) (int32, int, error) {
	var v uint32
	for i := range 5 {
		if i >= len(b) {
			return 0, 0, io.EOF
		}
		c := b[i]
		v |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return int32(v), i + 1, nil
		}
	}
	return 0, 0, errVarInt
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
// each byte goes through the block cipher alone. buf holds a 32-byte sliding
// window so shifting occurs once per 16 bytes instead of once per byte.
type cfb8 struct {
	block   cipher.Block
	buf     [32]byte
	tmp     [16]byte
	pos     int
	decrypt bool
}

func newCFB8(block cipher.Block, iv []byte, decrypt bool) *cfb8 {
	c := &cfb8{block: block, decrypt: decrypt}
	copy(c.buf[:16], iv)
	return c
}

func (c *cfb8) XORKeyStream(dst, src []byte) {
	if len(src) == 0 {
		return
	}
	_ = dst[len(src)-1]
	pos := c.pos
	if c.decrypt {
		for i, in := range src {
			c.block.Encrypt(c.tmp[:], c.buf[pos:pos+16])
			dst[i] = in ^ c.tmp[0]
			c.buf[pos+16] = in
			pos++
			if pos == 16 {
				copy(c.buf[:16], c.buf[16:32])
				pos = 0
			}
		}
	} else {
		for i, in := range src {
			c.block.Encrypt(c.tmp[:], c.buf[pos:pos+16])
			out := in ^ c.tmp[0]
			dst[i] = out
			c.buf[pos+16] = out
			pos++
			if pos == 16 {
				copy(c.buf[:16], c.buf[16:32])
				pos = 0
			}
		}
	}
	c.pos = pos
}

// fmtErr wraps an error of a connection with its state.
func fmtErr(state string, err error) error { return fmt.Errorf("java: %s: %w", state, err) }
