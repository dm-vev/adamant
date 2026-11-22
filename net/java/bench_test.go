package java

import (
	"bytes"
	"crypto/aes"
	"io"
	"net"
	"testing"
	"time"
)

// loopbackConn is an in-memory net.Conn backed by a bytes.Buffer for single-goroutine
// write-then-read benchmarking without OS pipe/socket overhead.
type loopbackConn struct {
	buf bytes.Buffer
}

func (c *loopbackConn) Read(p []byte) (int, error)       { return c.buf.Read(p) }
func (c *loopbackConn) Write(p []byte) (int, error)      { return c.buf.Write(p) }
func (c *loopbackConn) Close() error                     { return nil }
func (c *loopbackConn) LocalAddr() net.Addr              { return nil }
func (c *loopbackConn) RemoteAddr() net.Addr             { return nil }
func (c *loopbackConn) SetDeadline(time.Time) error      { return nil }
func (c *loopbackConn) SetReadDeadline(time.Time) error  { return nil }
func (c *loopbackConn) SetWriteDeadline(time.Time) error { return nil }

func BenchmarkPacketRoundTrip(b *testing.B) {
	small := bytes.Repeat([]byte("player move "), 10)
	large := bytes.Repeat([]byte("chunk section palette data 0123456789 "), 40)
	secret := []byte("0123456789abcdef")

	cases := []struct {
		name      string
		payload   []byte
		threshold int
		encrypt   bool
	}{
		{"Uncompressed_120B", small, -1, false},
		{"BelowThreshold_120B", small, 256, false},
		{"Compressed_1520B", large, 256, false},
		{"Encrypted_120B", small, -1, true},
		{"CompressedEncrypted_1520B", large, 256, true},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			lc := &loopbackConn{}
			lc.buf.Grow(8192)
			conn := NewConn(lc)
			if tc.threshold >= 0 {
				conn.SetCompression(tc.threshold)
			}
			if tc.encrypt {
				if err := conn.EnableEncryption(secret); err != nil {
					b.Fatal(err)
				}
			}
			// Warm up one round trip.
			if err := conn.WritePacket(0x27, tc.payload); err != nil {
				b.Fatal(err)
			}
			if _, _, err := conn.ReadPacket(); err != nil {
				b.Fatal(err)
			}

			b.ReportAllocs()
			b.SetBytes(int64(len(tc.payload)))
			b.ResetTimer()
			for b.Loop() {
				if err := conn.WritePacket(0x27, tc.payload); err != nil {
					b.Fatal(err)
				}
				id, got, err := conn.ReadPacket()
				if err != nil || id != 0x27 || len(got) != len(tc.payload) {
					b.Fatalf("ReadPacket: id=%#x len=%d err=%v", id, len(got), err)
				}
			}
		})
	}
}

func BenchmarkWritePacket(b *testing.B) {
	payload := bytes.Repeat([]byte("chunk section palette data 0123456789 "), 40)
	for _, tc := range []struct {
		name      string
		threshold int
	}{
		{"Uncompressed", -1},
		{"Compressed", 256},
	} {
		b.Run(tc.name, func(b *testing.B) {
			conn := NewConn(&discardConn{})
			if tc.threshold >= 0 {
				conn.SetCompression(tc.threshold)
			}
			if err := conn.WritePacket(0x27, payload); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			b.ResetTimer()
			for b.Loop() {
				if err := conn.WritePacket(0x27, payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCFB8(b *testing.B) {
	block, err := aes.NewCipher([]byte("0123456789abcdef"))
	if err != nil {
		b.Fatal(err)
	}
	iv := []byte("0123456789abcdef")
	c := newCFB8(block, iv, false)
	buf := make([]byte, 512)
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for b.Loop() {
		c.XORKeyStream(buf, buf)
	}
}

type discardConn struct{}

func (discardConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (discardConn) Write(p []byte) (int, error)      { return len(p), nil }
func (discardConn) Close() error                     { return nil }
func (discardConn) LocalAddr() net.Addr              { return nil }
func (discardConn) RemoteAddr() net.Addr             { return nil }
func (discardConn) SetDeadline(time.Time) error      { return nil }
func (discardConn) SetReadDeadline(time.Time) error  { return nil }
func (discardConn) SetWriteDeadline(time.Time) error { return nil }
