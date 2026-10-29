package nethernet

import (
	"bytes"
	"net"
	"net/netip"
	"testing"
	"time"
)

// TestSendBatch gathers what several connections send to two peers, alone
// and segmented, on sockets of IPv4 and of IPv6, and sends it: every
// datagram reaches its peer whole and in order.
func TestSendBatch(t *testing.T) {
	for _, network := range []string{"127.0.0.1:0", "[::]:0"} {
		pc, err := net.ListenPacket("udp", network)
		if err != nil {
			t.Fatal(err)
		}
		sock := pc.(*net.UDPConn)
		var peers [2]*net.UDPConn
		var to [2]netip.AddrPort
		for i := range peers {
			p, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
			if err != nil {
				t.Fatal(err)
			}
			peers[i], to[i] = p, p.LocalAddr().(*net.UDPAddr).AddrPort()
		}
		b := NewSendBatch()
		single := bytes.Repeat([]byte{1}, 100)
		segmented := append(bytes.Repeat([]byte{2}, 1000), bytes.Repeat([]byte{3}, 400)...)
		last := []byte{4, 4, 4}
		for round := range 2 {
			b.add(sock, single, len(single), to[0])
			b.add(sock, segmented[:1000], 500, to[1])
			b.add(sock, segmented, 500, to[1])
			b.add(sock, last, len(last), to[0])
			b.Flush()
			want := [2][][]byte{
				{single, last},
				{segmented[:500], segmented[500:1000], segmented[:500], segmented[500:1000], segmented[1000:]},
			}
			for i, p := range peers {
				_ = p.SetReadDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, 2048)
				for j, w := range want[i] {
					n, err := p.Read(buf)
					if err != nil || !bytes.Equal(buf[:n], w) {
						t.Fatalf("%s round %d: peer %d datagram %d is %d bytes (%v), want %d", network, round, i, j, n, err, len(w))
					}
				}
			}
		}
		if len(b.socks) != 1 || len(b.socks[0].msgs) != 0 {
			t.Fatalf("%s: the batch kept %d messages", network, len(b.socks[0].msgs))
		}
		_ = sock.Close()
		for _, p := range peers {
			_ = p.Close()
		}
	}
}

// TestConnSendBatch sets a SendBatch on a connection: what it sends waits
// for Flush, which sends it.
func TestConnSendBatch(t *testing.T) {
	l := listen(t, ListenConfig{})
	cli := dial(t, newDialer(t), l)
	srv := accept(t, l)
	b := NewSendBatch()
	srv.SetSendBatch(b)
	want := bytes.Repeat([]byte("batched "), 2000)
	if err := srv.Send(want); err != nil {
		t.Fatal(err)
	}
	srv.SetSendBatch(nil)
	if sendBatchSupported {
		_ = cli.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		if msg, err := cli.ReadMessage(); err == nil {
			t.Fatalf("read %d bytes before the batch was flushed", len(msg))
		}
	}
	b.Flush()
	if got := read(t, cli); !bytes.Equal(got, want) {
		t.Fatalf("read %d bytes, want %d", len(got), len(want))
	}
	// Without the batch the connection sends as before.
	if err := srv.Send([]byte("alone")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, cli); string(got) != "alone" {
		t.Fatalf("read %q", got)
	}
}
