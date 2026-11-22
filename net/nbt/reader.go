package nbt

import (
	"bytes"
	"io"
	"slices"
	"unsafe"
)

// offsetReader is a wrapper around an io.Reader, used to track the offset (amount of bytes read) of the data
// that is being read, so that errors may have offset data.
type offsetReader struct {
	io.Reader
	off     int64
	buf     [16]byte
	nameBuf [64]byte

	rem      []byte
	dataLen  int
	useData  bool
	bytesBuf *bytes.Buffer
	br       io.ByteReader

	// ReadByte is a function provided by offsetReader if constructed manually.
	ReadByte func() (byte, error)
	// Next is a function provided by offsetReader if constructed manually.
	Next func(n int) []byte
}

// initOffsetReader initializes dst for reading from r using the fastest available path without closure allocations.
func initOffsetReader(dst *offsetReader, r io.Reader) {
	dst.Reader = r
	if buf, ok := r.(*bytes.Buffer); ok {
		dst.bytesBuf = buf
		return
	}
	if br, ok := r.(io.ByteReader); ok {
		dst.br = br
	}
}

// initSlice initializes dst to read directly from a byte slice without a bytes.Buffer wrapper.
func (b *offsetReader) initSlice(data []byte) {
	b.rem = data
	b.dataLen = len(data)
	b.useData = true
}

// newOffsetReader returns a new offset reader for the io.Reader passed.
func newOffsetReader(r io.Reader) *offsetReader {
	reader := &offsetReader{}
	initOffsetReader(reader, r)
	return reader
}

// offset returns the total number of bytes read so far.
func (b *offsetReader) offset() int64 {
	if b.useData {
		return int64(b.dataLen - len(b.rem))
	}
	return b.off
}

// readByte reads a single byte using the fastest available source path.
func (b *offsetReader) readByte() (byte, error) {
	if len(b.rem) > 0 {
		v := b.rem[0]
		b.rem = b.rem[1:]
		return v, nil
	}
	return b.readByteSlow()
}

func (b *offsetReader) readByteSlow() (byte, error) {
	if b.useData {
		return 0, io.EOF
	}
	if b.bytesBuf != nil {
		v, err := b.bytesBuf.ReadByte()
		if err == nil {
			b.off++
		}
		return v, err
	}
	if b.br != nil {
		v, err := b.br.ReadByte()
		if err == nil {
			b.off++
		}
		return v, err
	}
	if b.ReadByte != nil {
		return b.ReadByte()
	}
	_, err := io.ReadAtLeast(b, b.buf[:1], 1)
	return b.buf[0], err
}

// readSmall reads n bytes (n <= len(b.buf)) and returns a view valid until the next read on b.
func (b *offsetReader) readSmall(n int) ([]byte, error) {
	if len(b.rem) >= n {
		p := b.rem[:n]
		b.rem = b.rem[n:]
		return p, nil
	}
	return b.readSmallSlow(n)
}

func (b *offsetReader) readSmallSlow(n int) ([]byte, error) {
	if b.useData {
		return nil, io.ErrUnexpectedEOF
	}
	if b.bytesBuf != nil {
		if b.bytesBuf.Len() < n {
			return nil, io.ErrUnexpectedEOF
		}
		p := b.bytesBuf.Next(n)
		b.off += int64(n)
		return p, nil
	}
	p := b.buf[:n]
	if _, err := b.Read(p); err != nil {
		return nil, err
	}
	return p, nil
}

// readOwnedString reads n bytes into an owned Go string that never aliases the underlying input buffer.
func (b *offsetReader) readOwnedString(n int) (string, error) {
	if uint(n) <= uint(len(b.rem)) {
		s := string(b.rem[:n])
		b.rem = b.rem[n:]
		return s, nil
	}
	return b.readOwnedStringSlow(n)
}

func (b *offsetReader) readOwnedStringSlow(n int) (string, error) {
	if n == 0 {
		return "", nil
	}
	if b.useData {
		return "", io.ErrUnexpectedEOF
	}
	if b.bytesBuf != nil {
		if b.bytesBuf.Len() < n {
			return "", io.ErrUnexpectedEOF
		}
		p := b.bytesBuf.Next(n)
		b.off += int64(n)
		return string(p), nil
	}
	if n <= len(b.nameBuf) {
		if _, err := b.Read(b.nameBuf[:n]); err != nil {
			return "", err
		}
		return string(b.nameBuf[:n]), nil
	}
	p := make([]byte, n)
	if _, err := b.Read(p); err != nil {
		return "", err
	}
	return unsafe.String(unsafe.SliceData(p), n), nil
}

// readTempBytes reads n bytes into a temporary slice valid until the next read on b.
func (b *offsetReader) readTempBytes(n int) ([]byte, error) {
	if uint(n) <= uint(len(b.rem)) {
		p := b.rem[:n]
		b.rem = b.rem[n:]
		return p, nil
	}
	return b.readTempBytesSlow(n)
}

func (b *offsetReader) readTempBytesSlow(n int) ([]byte, error) {
	if n == 0 {
		return nil, nil
	}
	if b.useData {
		return nil, io.ErrUnexpectedEOF
	}
	if b.bytesBuf != nil {
		if b.bytesBuf.Len() < n {
			return nil, io.ErrUnexpectedEOF
		}
		p := b.bytesBuf.Next(n)
		b.off += int64(n)
		return p, nil
	}
	if n <= len(b.nameBuf) {
		if _, err := b.Read(b.nameBuf[:n]); err != nil {
			return nil, err
		}
		return b.nameBuf[:n], nil
	}
	p := make([]byte, n)
	if _, err := b.Read(p); err != nil {
		return nil, err
	}
	return p, nil
}

// readInto reads exactly len(dst) bytes into dst.
func (b *offsetReader) readInto(dst []byte) error {
	if len(dst) == 0 {
		return nil
	}
	if b.useData {
		if len(b.rem) < len(dst) {
			return io.ErrUnexpectedEOF
		}
		copy(dst, b.rem[:len(dst)])
		b.rem = b.rem[len(dst):]
		return nil
	}
	if b.bytesBuf != nil {
		if b.bytesBuf.Len() < len(dst) {
			return io.ErrUnexpectedEOF
		}
		copy(dst, b.bytesBuf.Next(len(dst)))
		b.off += int64(len(dst))
		return nil
	}
	_, err := b.Read(dst)
	return err
}

// Read reads from the io.Reader and increases the reader's offset by exactly n.
func (b *offsetReader) Read(p []byte) (n int, err error) {
	if b.useData {
		if len(b.rem) < len(p) {
			n = copy(p, b.rem)
			b.rem = b.rem[n:]
			if n == 0 && len(p) > 0 {
				return 0, io.EOF
			}
			return n, io.ErrUnexpectedEOF
		}
		n = copy(p, b.rem[:len(p)])
		b.rem = b.rem[n:]
		return n, nil
	}
	if b.bytesBuf != nil {
		n, err = io.ReadAtLeast(b.bytesBuf, p, len(p))
		b.off += int64(n)
		return
	}
	n, err = io.ReadAtLeast(b.Reader, p, len(p))
	b.off += int64(n)
	return
}

// fixed reads n bytes. Past a bound it grows the bytes as they are read
// rather than allocate n at once, so that a length that no data follows
// costs no memory, and it refuses a length past the end of data of a
// known length outright.
func (b *offsetReader) fixed(n int64) ([]byte, error) {
	if n < 0 {
		return nil, io.ErrUnexpectedEOF
	}
	if b.useData {
		if n > int64(len(b.rem)) {
			return nil, io.ErrUnexpectedEOF
		}
		p := make([]byte, n)
		copy(p, b.rem[:int(n)])
		b.rem = b.rem[int(n):]
		return p, nil
	}
	if b.bytesBuf != nil {
		if n > int64(b.bytesBuf.Len()) {
			return nil, io.ErrUnexpectedEOF
		}
		p := make([]byte, n)
		copy(p, b.bytesBuf.Next(int(n)))
		b.off += n
		return p, nil
	}
	if l, ok := b.Reader.(interface{ Len() int }); ok && n > int64(l.Len()) {
		return nil, io.ErrUnexpectedEOF
	}
	const step = 64 << 10
	p := make([]byte, 0, min(n, step))
	for int64(len(p)) < n {
		m := int(min(n-int64(len(p)), step))
		p = slices.Grow(p, m)
		if _, err := b.Read(p[len(p) : len(p)+m]); err != nil {
			return nil, err
		}
		p = p[:len(p)+m]
	}
	return p, nil
}

// capacity returns how many of n elements, each taking a byte at least, to
// allocate room for at once: all of them if the data has room for them,
// some if its length is not known, and an error if it has no room.
func (b *offsetReader) capacity(n int64) (int, error) {
	if n < 0 {
		return 0, io.ErrUnexpectedEOF
	}
	if b.useData {
		if n > int64(len(b.rem)) {
			return 0, io.ErrUnexpectedEOF
		}
		return int(n), nil
	}
	if b.bytesBuf != nil {
		if n > int64(b.bytesBuf.Len()) {
			return 0, io.ErrUnexpectedEOF
		}
		return int(n), nil
	}
	if l, ok := b.Reader.(interface{ Len() int }); ok {
		if n > int64(l.Len()) {
			return 0, io.ErrUnexpectedEOF
		}
		return int(n), nil
	}
	return int(min(n, 1<<12)), nil
}

// hasBytes reports whether b is backed by an in-memory buffer known to have at least n unread bytes.
func (b *offsetReader) hasBytes(n int64) bool {
	if n < 0 {
		return false
	}
	if b.useData {
		return n <= int64(len(b.rem))
	}
	if b.bytesBuf != nil {
		return n <= int64(b.bytesBuf.Len())
	}
	return false
}
