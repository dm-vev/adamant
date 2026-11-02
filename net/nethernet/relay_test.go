package nethernet

import (
	"context"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/turn"
)

// TestRelayPermissionsFollowConnections has connections opened over a
// signaling service hold the permissions of their peer's addresses on the
// relay: at most 8, of those of the offer and those its sender trickles
// for it; once it opens, only that of the address it uses, which another
// connection may hold too; none once it closed.
func TestRelayPermissionsFollowConnections(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true})
	// A relay not allocated yet holds the permissions all the same.
	r := newRelay(l, turn.Config{Server: netip.MustParseAddrPort("127.0.0.1:3478")})
	l.relay.Store(r)
	held := func() map[netip.Addr]int {
		r.mu.Lock()
		defer r.mu.Unlock()
		return maps.Clone(r.held)
	}
	addr := func(i int) netip.Addr { return netip.AddrFrom4([4]byte{192, 0, 2, byte(i)}) }
	trickle := func(network string, id uint64, a netip.Addr) {
		l.permitCandidate(Signal{Type: SignalCandidate, ConnectionID: id, NetworkID: network,
			Data: "candidate:1 1 udp 2122260223 " + a.String() + " 9 typ host generation 0"})
	}
	loopback := netip.MustParseAddr("127.0.0.1")
	var answered map[netip.Addr]int
	join := func(id uint64) *Conn {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		signal := func(_ context.Context, offer string) (string, error) {
			d, err := parseDescription(offer)
			if err != nil {
				return "", err
			}
			for i := range 5 {
				d.candidates = append(d.candidates, netip.AddrPortFrom(addr(i), 9))
			}
			reply := l.answerSignal(Signal{Type: SignalOffer, ConnectionID: id, NetworkID: "client", Data: encodeDescription(d)})
			if reply.Type != SignalAnswer {
				return "", fmt.Errorf("offer answered with %s %s", reply.Type, reply.Data)
			}
			trickle("client", id, addr(10))
			trickle("stranger", id, addr(11))
			trickle("client", id+1, addr(12))
			trickle("client", id, addr(13))
			trickle("client", id, addr(14))
			answered = held()
			return reply.Data, nil
		}
		c, err := newDialer(t).dial(ctx, signal, loopback, netip.AddrPort{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return accept(t, l)
	}

	first := join(1)
	want := map[netip.Addr]int{loopback: 1, addr(10): 1, addr(13): 1}
	for i := range 5 {
		want[addr(i)] = 1
	}
	if !maps.Equal(answered, want) {
		t.Fatalf("held while the connection opened %v, want %v", answered, want)
	}
	if got := held(); !maps.Equal(got, map[netip.Addr]int{loopback: 1}) {
		t.Fatalf("held once the connection opened %v, want only the address it uses", got)
	}
	second := join(3)
	if got := held(); !maps.Equal(got, map[netip.Addr]int{loopback: 2}) {
		t.Fatalf("held by two connections %v", got)
	}
	first.abort(net.ErrClosed)
	if got := held(); !maps.Equal(got, map[netip.Addr]int{loopback: 1}) {
		t.Fatalf("held once one of two connections closed %v", got)
	}
	second.abort(net.ErrClosed)
	// A candidate trickled for a connection that closed is dropped.
	trickle("client", 3, addr(20))
	if got := held(); len(got) != 0 {
		t.Fatalf("held once the connections closed %v", got)
	}
}
