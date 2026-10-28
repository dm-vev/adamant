//go:build linux

package nethernet

import (
	"bytes"
	"net"
	"testing"
	"time"
)

// TestSegmentedSend sends ten datagrams in one call: the peer receives
// them one by one, the last shorter.
func TestSegmentedSend(t *testing.T) {
	a, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var batch []byte
	for i := range 10 {
		size := 1200
		if i == 9 {
			size = 500
		}
		batch = append(batch, bytes.Repeat([]byte{byte(i)}, size)...)
	}
	writeSegments(a, batch, 1200, b.LocalAddr().(*net.UDPAddr).AddrPort())
	if segmentsBroken.Load() {
		t.Fatal("the kernel refused UDP_SEGMENT")
	}
	buf := make([]byte, 2000)
	_ = b.SetReadDeadline(time.Now().Add(5 * time.Second))
	for i := range 10 {
		n, _, err := b.ReadFromUDPAddrPort(buf)
		if err != nil {
			t.Fatal(err)
		}
		want := 1200
		if i == 9 {
			want = 500
		}
		if n != want || buf[0] != byte(i) || buf[n-1] != byte(i) {
			t.Fatalf("datagram %d: %d bytes of %d, want %d bytes", i, n, buf[0], want)
		}
	}
}
