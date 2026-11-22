package nbt

import (
	"bytes"
	"io"
	"unsafe"
)

// offsetWriter is a wrapper around an io.Writer which keeps track of the amount of bytes written, so that it
// may be used in errors.
type offsetWriter struct {
	io.Writer
	off int64
	buf [32]byte

	// WriteByte is a function implemented by offsetWriter if the io.Writer does not implement it itself.
	WriteByte func(byte) error

	raw    []byte
	out    *bytes.Buffer
	bw     io.ByteWriter
	useRaw bool
}

// initOffsetWriter initializes dst to write to w using the fastest available path without allocating closures.
func initOffsetWriter(dst *offsetWriter, w io.Writer) {
	dst.Writer = w
	if buf, ok := w.(*bytes.Buffer); ok {
		dst.out = buf
		return
	}
	if bw, ok := w.(io.ByteWriter); ok {
		dst.bw = bw
	}
}

// offset returns the total number of bytes written so far.
func (w *offsetWriter) offset() int64 {
	if w.useRaw {
		return int64(len(w.raw))
	}
	return w.off
}

// Write writes a byte slice to the underlying io.Writer. It increases the byte offset by exactly n.
func (w *offsetWriter) Write(b []byte) (n int, err error) {
	if w.useRaw {
		w.raw = append(w.raw, b...)
		return len(b), nil
	}
	if w.out != nil {
		n, err = w.out.Write(b)
		w.off += int64(n)
		return
	}
	n, err = w.Writer.Write(b)
	w.off += int64(n)
	return
}

// writeByte writes a single byte using the fastest available target path.
func (w *offsetWriter) writeByte(b byte) error {
	if w.useRaw {
		w.raw = append(w.raw, b)
		return nil
	}
	return w.writeByteSlow(b)
}

func (w *offsetWriter) writeByteSlow(b byte) error {
	if w.out != nil {
		w.off++
		return w.out.WriteByte(b)
	}
	if w.bw != nil {
		if err := w.bw.WriteByte(b); err != nil {
			return err
		}
		w.off++
		return nil
	}
	if w.WriteByte != nil {
		if err := w.WriteByte(b); err != nil {
			return err
		}
		w.off++
		return nil
	}
	w.buf[0] = b
	_, err := w.Write(w.buf[:1])
	return err
}

// writeBytes writes p to the underlying target.
func (w *offsetWriter) writeBytes(p []byte) error {
	if w.useRaw {
		w.raw = append(w.raw, p...)
		return nil
	}
	return w.writeBytesSlow(p)
}

func (w *offsetWriter) writeBytesSlow(p []byte) error {
	if w.out != nil {
		n, err := w.out.Write(p)
		w.off += int64(n)
		return err
	}
	_, err := w.Write(p)
	return err
}

// writeStringBytes writes the raw bytes of s without copying into a temporary byte slice.
func (w *offsetWriter) writeStringBytes(s string) error {
	if w.useRaw {
		w.raw = append(w.raw, s...)
		return nil
	}
	return w.writeStringBytesSlow(s)
}

func (w *offsetWriter) writeStringBytesSlow(s string) error {
	if len(s) == 0 {
		return nil
	}
	if w.out != nil {
		n, err := w.out.WriteString(s)
		w.off += int64(n)
		return err
	}
	_, err := w.Write(unsafe.Slice(unsafe.StringData(s), len(s)))
	return err
}
