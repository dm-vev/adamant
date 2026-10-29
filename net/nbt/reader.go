package nbt

import (
	"io"
	"slices"
)

// offsetReader is a wrapper around an io.Reader, used to track the offset (amount of bytes read) of the data
// that is being read, so that errors may have offset data.
type offsetReader struct {
	io.Reader
	off int64
	buf [8]byte

	// ReadByte is a function provided by offsetReader if the io.Reader does not implement io.ByteReader.
	ReadByte func() (byte, error)
	// Next is a function provided by offsetReader if the io.Reader does not have a Next method.
	Next func(n int) []byte
}

// newOffsetReader returns a new offset reader for the io.Reader passed, setting the ReadByte and Next
// functions as appropriate for that particular reader.
func newOffsetReader(r io.Reader) *offsetReader {
	reader := &offsetReader{Reader: r}
	if byteReader, ok := r.(io.ByteReader); ok {
		reader.ReadByte = func() (byte, error) {
			reader.off++
			return byteReader.ReadByte()
		}
	} else {
		reader.ReadByte = func() (byte, error) {
			data := make([]byte, 1)
			_, err := io.ReadAtLeast(reader, data, 1)
			return data[0], err
		}
	}
	if r, ok := r.(interface {
		Next(n int) []byte
	}); ok {
		reader.Next = func(n int) []byte {
			data := r.Next(n)
			reader.off += int64(len(data))
			return data
		}
	} else {
		reader.Next = func(n int) []byte {
			data := make([]byte, n)
			_, _ = io.ReadAtLeast(reader, data, n)
			return data
		}
	}
	return reader
}

// Read reads from the io.Reader and increases the reader's offset by exactly n.
func (b *offsetReader) Read(p []byte) (n int, err error) {
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
	if l, ok := b.Reader.(interface{ Len() int }); ok {
		if n > int64(l.Len()) {
			return 0, io.ErrUnexpectedEOF
		}
		return int(n), nil
	}
	return int(min(n, 1<<12)), nil
}
