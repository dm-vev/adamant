package interop

import (
	"bytes"
	"io"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// messages is a MessageConn and an io.Writer of whole messages, as a
// NetherNet connection is to gophertunnel.
type messages struct{ queue [][]byte }

func (m *messages) Write(b []byte) (int, error) {
	m.queue = append(m.queue, bytes.Clone(b))
	return len(b), nil
}
func (m *messages) Send(b []byte) error { _, err := m.Write(b); return err }
func (m *messages) ReadMessage() ([]byte, error) {
	if len(m.queue) == 0 {
		return nil, io.EOF
	}
	b := m.queue[0]
	m.queue = m.queue[1:]
	return b, nil
}
func (m *messages) ReadPacket() ([]byte, error) { return m.ReadMessage() }
func (m *messages) Read(b []byte) (int, error) {
	msg, err := m.ReadMessage()
	return copy(b, msg), err
}
func (m *messages) Close() error { return nil }

// NetherNet batches have no header and no encryption.
func (m *messages) BatchHeader() []byte     { return nil }
func (m *messages) DisableEncryption() bool { return true }

func gophertunnelPackets() [][]byte {
	small := []byte{0x09, 'h', 'i'}
	large := append([]byte{0x3a}, bytes.Repeat([]byte("level chunk "), 2000)...)
	return [][]byte{small, large, small}
}

// TestBatchesFromGophertunnel reads what gophertunnel's encoder writes,
// before and after compression is on, compressed or under its threshold.
func TestBatchesFromGophertunnel(t *testing.T) {
	m := &messages{}
	enc := packet.NewEncoder(m)
	conn := bedrock.NewConn(m, bedrock.Config{})
	if err := enc.Encode([][]byte{{0xc1, 0x01, 0, 0, 8, 0x91}}); err != nil {
		t.Fatal(err)
	}
	if p, err := conn.ReadPacket(); err != nil || !bytes.Equal(p, []byte{0xc1, 0x01, 0, 0, 8, 0x91}) {
		t.Fatalf("read % x, %v", p, err)
	}
	for _, comp := range []packet.Compression{packet.FlateCompression, packet.NopCompression} {
		enc.EnableCompression(comp, 256)
		conn.EnableCompression(bedrock.Flate{})
		want := gophertunnelPackets()
		if err := enc.Encode(want[:1]); err != nil {
			t.Fatal(err)
		}
		if err := enc.Encode(want[1:]); err != nil {
			t.Fatal(err)
		}
		for _, w := range want {
			if p, err := conn.ReadPacket(); err != nil || !bytes.Equal(p, w) {
				t.Fatalf("%T: read %d bytes, %v; want %d", comp, len(p), err, len(w))
			}
		}
	}
}

// TestBatchesToGophertunnel has gophertunnel's decoder read what we write.
func TestBatchesToGophertunnel(t *testing.T) {
	m := &messages{}
	conn := bedrock.NewConn(m, bedrock.Config{})
	dec := packet.NewDecoder(m)
	_ = conn.WritePacket([]byte{0x8f, 0x01, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0})
	_ = conn.Flush()
	got, err := dec.Decode()
	if err != nil || len(got) != 1 || got[0][0] != 0x8f {
		t.Fatalf("decoded %v, %v", got, err)
	}
	conn.EnableCompression(bedrock.Flate{Level: 6})
	dec.EnableCompression(packet.FlateCompression, 1<<24)
	want := gophertunnelPackets()
	for _, p := range want {
		_ = conn.WritePacket(p)
	}
	_ = conn.Flush()
	_ = conn.WritePacket(want[0])
	_ = conn.Flush()
	var all [][]byte
	for len(all) < 4 {
		got, err := dec.Decode()
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, got...)
	}
	for i, w := range append(want, want[0]) {
		if !bytes.Equal(all[i], w) {
			t.Fatalf("packet %d: %d bytes, want %d", i, len(all[i]), len(w))
		}
	}
}
