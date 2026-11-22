package java

import (
	"bytes"
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"net"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestCFB8 encrypts the vectors of NIST SP 800-38A F.3.7 (CFB8-AES128)
// and decrypts them back.
func TestCFB8(t *testing.T) {
	key, _ := hex.DecodeString("2b7e151628aed2a6abf7158809cf4f3c")
	iv, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	plain, _ := hex.DecodeString("6bc1bee22e409f96e93d7e117393172aae2d")
	want, _ := hex.DecodeString("3b79424c9c0dd436bace9e0ed4586a4f32b9")
	block, _ := aes.NewCipher(key)
	got := make([]byte, len(plain))
	newCFB8(block, iv, false).XORKeyStream(got, plain)
	if !bytes.Equal(got, want) {
		t.Fatalf("encrypted % x, want % x", got, want)
	}
	back := make([]byte, len(got))
	newCFB8(block, iv, true).XORKeyStream(back, got)
	if !bytes.Equal(back, plain) {
		t.Fatalf("decrypted % x", back)
	}
}

func TestVarInt(t *testing.T) {
	for v, want := range map[int32]string{0: "00", 1: "01", 127: "7f", 128: "8001", 255: "ff01", 25565: "ddc701", 2097151: "ffff7f",
		2147483647: "ffffffff07", -1: "ffffffff0f", -2147483648: "8080808008"} {
		if got := hex.EncodeToString(appendVarInt(nil, v)); got != want {
			t.Fatalf("VarInt %d = %s, want %s", v, got, want)
		}
		if back, err := readVarInt(bytes.NewReader(appendVarInt(nil, v))); err != nil || back != v {
			t.Fatalf("read %d back as %d, %v", v, back, err)
		}
	}
	if _, err := readVarInt(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0x01})); err != errVarInt {
		t.Fatalf("six bytes read with %v", err)
	}
}

// TestConnCompressionAndEncryption sends packets both ways, compressed
// past the threshold and encrypted.
func TestConnCompressionAndEncryption(t *testing.T) {
	a, b := net.Pipe()
	ca, cb := NewConn(a), NewConn(b)
	secret := []byte("0123456789abcdef")
	for _, c := range []*Conn{ca, cb} {
		c.SetCompression(64)
		if err := c.EnableEncryption(secret); err != nil {
			t.Fatal(err)
		}
	}
	small, large := []byte("hello"), bytes.Repeat([]byte("chunk data "), 100)
	go func() {
		_ = ca.WritePacket(0x10, small)
		_ = ca.WritePacket(0x27, large)
	}()
	for _, want := range []struct {
		id     int32
		fields []byte
	}{{0x10, small}, {0x27, large}} {
		id, fields, err := cb.ReadPacket()
		if err != nil || id != want.id || !bytes.Equal(fields, want.fields) {
			t.Fatalf("read %#x %d bytes, %v", id, len(fields), err)
		}
	}
}

// TestListenerStatusAndLogin lists the server as a server list does, with
// the protocol of the client, and hands a login to Accept, which
// disconnects it with a message.
func TestListenerStatusAndLogin(t *testing.T) {
	l, err := Listen("127.0.0.1:0", func() Status {
		return Status{Name: "Adamant", Description: "A server", Online: 3, Max: 20}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	status, rtt, err := QueryStatus(l.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var st struct {
		Version struct {
			Name     string
			Protocol int32
		}
		Players     struct{ Online, Max int }
		Description struct{ Text string }
	}
	if err := json.Unmarshal(status, &st); err != nil || st.Version.Name != "Adamant" || st.Version.Protocol != -1 ||
		st.Players.Online != 3 || st.Description.Text != "A server" || rtt <= 0 {
		t.Fatalf("status %s (%+v), rtt %v, %v", status, st, rtt, err)
	}

	nc, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	c := NewConn(nc)
	hs := AppendVarInt(nil, 774)
	hs = AppendString(hs, "localhost")
	hs = append(hs, 0x63, 0xdd)
	hs = AppendVarInt(hs, stateLogin)
	_ = c.WritePacket(0x00, hs)
	_ = c.WritePacket(0x00, append(AppendString(nil, "Steve"), make([]byte, 16)...))
	login, err := l.Accept()
	if err != nil || login.Name != "Steve" || login.Protocol != 774 {
		t.Fatalf("login %+v, %v", login, err)
	}
	go func() { _ = login.Disconnect("Not yet.") }()
	id, fields, err := c.ReadPacket()
	r := NewReader(fields)
	if text := r.String(); err != nil || id != 0x00 || !strings.Contains(text, "Not yet.") {
		t.Fatalf("disconnect %#x %q, %v", id, text, err)
	}
}

func FuzzConnRead(f *testing.F) {
	f.Add([]byte{0x02, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, threshold := range []int{-1, 64} {
			a, p := net.Pipe()
			c := NewConn(a)
			c.SetCompression(threshold)
			go func() {
				_, _ = p.Write(b)
				_ = p.Close()
			}()
			for {
				if _, _, err := c.ReadPacket(); err != nil {
					break
				}
			}
		}
	})
}

// TestLengthPastTheData reads a frame whose length asks for the largest
// packet, of which a few bytes come: it costs the memory of those bytes,
// not of the length, as many such connections would run the server out of
// memory otherwise.
func TestLengthPastTheData(t *testing.T) {
	for _, threshold := range []int{-1, 0} {
		frame := appendVarInt(nil, MaxPacket)
		if threshold >= 0 {
			// A compressed frame claiming the largest packet in 3 bytes
			// of zlib.
			body := append(appendVarInt(nil, MaxPacket), 0x78, 0x9c, 0x03)
			frame = append(appendVarInt(nil, int32(len(body))), body...)
		} else {
			frame = append(frame, 1, 2, 3)
		}
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		a, p := net.Pipe()
		c := NewConn(a)
		c.SetCompression(threshold)
		go func() {
			_, _ = p.Write(frame)
			_ = p.Close()
		}()
		if _, _, err := c.ReadPacket(); err == nil {
			t.Fatal("a frame past its data was read")
		}
		runtime.ReadMemStats(&after)
		if n := after.TotalAlloc - before.TotalAlloc; n > 512<<10 {
			t.Fatalf("threshold %d: %d bytes allocated for a frame of %d", threshold, n, len(frame))
		}
	}
}

func TestCFB8MultiBlockEquivalence(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := []byte("fedcba9876543210")
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	plain := make([]byte, 129)
	for i := range plain {
		plain[i] = byte(i*37 + 11)
	}

	// Reference byte-shifting CFB8 encryption.
	refIV := bytes.Clone(iv)
	refTmp := make([]byte, 16)
	want := make([]byte, len(plain))
	for i, in := range plain {
		block.Encrypt(refTmp, refIV)
		out := in ^ refTmp[0]
		want[i] = out
		copy(refIV, refIV[1:])
		refIV[15] = out
	}

	// Single-call in-place encrypt + decrypt.
	enc := newCFB8(block, iv, false)
	got := bytes.Clone(plain)
	enc.XORKeyStream(got, got)
	if !bytes.Equal(got, want) {
		t.Fatalf("single-call encrypt mismatch")
	}
	dec := newCFB8(block, iv, true)
	back := bytes.Clone(got)
	dec.XORKeyStream(back, back)
	if !bytes.Equal(back, plain) {
		t.Fatalf("single-call decrypt mismatch")
	}

	// Chunked streaming across 16-byte sliding-window boundaries.
	encChunk := newCFB8(block, iv, false)
	decChunk := newCFB8(block, iv, true)
	gotChunk := make([]byte, len(plain))
	backChunk := make([]byte, len(plain))
	chunks := []int{1, 7, 16, 17, 33, 55}
	off := 0
	for _, n := range chunks {
		encChunk.XORKeyStream(gotChunk[off:off+n], plain[off:off+n])
		decChunk.XORKeyStream(backChunk[off:off+n], gotChunk[off:off+n])
		off += n
	}
	if !bytes.Equal(gotChunk, want) || !bytes.Equal(backChunk, plain) {
		t.Fatalf("chunked CFB8 mismatch")
	}
}

func TestCompressionEdgeCases(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	ca, cb := NewConn(a), NewConn(b)
	ca.SetCompression(64)
	cb.SetCompression(64)

	// Test exact threshold boundaries (id=1 takes 1 byte, so bodyLen = 1 + len(fields)).
	sizes := []int{0, 62, 63, 64, 1024, 70000}
	errCh := make(chan error, 1)
	go func() {
		for _, sz := range sizes {
			fields := bytes.Repeat([]byte("A"), sz)
			if err := ca.WritePacket(1, fields); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()
	for _, sz := range sizes {
		id, fields, err := cb.ReadPacket()
		if err != nil {
			t.Fatalf("size %d: ReadPacket: %v", sz, err)
		}
		if id != 1 || len(fields) != sz {
			t.Fatalf("size %d: got id=%d len=%d", sz, id, len(fields))
		}
	}
	if err := <-errCh; err != nil {
		t.Fatalf("WritePacket: %v", err)
	}
}
