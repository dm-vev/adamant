package nethernet

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/stun"
	"github.com/dm-vev/adamant/net/turn"
)

// TestRelayPermissionsFollowConnections has connections opened over a
// signaling service hold the permissions of their peer's addresses on the
// relay: at most 8, of those of the offer and those its sender trickles
// for it; once it opens, only that of the address it uses, which another
// connection may hold too; none once it closed.
func TestRelayPermissionsFollowConnections(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true})
	// Without a relay, an offer over a signaling service holds nothing.
	d, err := parseDescription(idleOffer())
	if err != nil {
		t.Fatal(err)
	}
	d.candidates = []netip.AddrPort{netip.MustParseAddrPort("192.0.2.99:9")}
	if reply := l.answerSignal(Signal{Type: SignalOffer, ConnectionID: 99, NetworkID: "client", Data: encodeDescription(d)}); reply.Type != SignalAnswer {
		t.Fatalf("offer without a relay answered with %s %s", reply.Type, reply.Data)
	}
	expireAll(l)
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
	// One trickled once the connection opened is dropped.
	trickle("client", 1, addr(30))
	if got := held(); !maps.Equal(got, map[netip.Addr]int{loopback: 1}) {
		t.Fatalf("held after a candidate trickled for an open connection %v", got)
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
	l.mu.Lock()
	signals := len(l.signals)
	l.mu.Unlock()
	if signals != 0 {
		t.Fatalf("%d offers over signaling still noted once their connections closed", signals)
	}
}

// TestRelayPermitsPeerAddresses holds permissions only for addresses a
// peer may send from through the relay: not an invalid or unspecified
// one, nor loopback unless the TURN server is on loopback too.
func TestRelayPermitsPeerAddresses(t *testing.T) {
	l := listen(t, ListenConfig{})
	r := newRelay(l, turn.Config{Server: netip.MustParseAddrPort("192.0.2.1:3478")})
	t.Cleanup(r.close)
	c := &Conn{}
	for _, a := range []netip.Addr{{}, netip.IPv4Unspecified(), netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("::ffff:198.51.100.1")} {
		r.hold(c, a)
	}
	r.mu.Lock()
	held := maps.Clone(r.held)
	r.mu.Unlock()
	if !maps.Equal(held, map[netip.Addr]int{netip.MustParseAddr("198.51.100.1"): 1}) {
		t.Fatalf("held %v", held)
	}
}

// TestRelayNotMade returns why Relay made no relay, leaving the listener
// without one: the TURN server refused the allocation, the context ended
// before the server answered, or the listener closed.
func TestRelayNotMade(t *testing.T) {
	l := listen(t, ListenConfig{})
	server, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	cfg := turn.Config{Server: server.LocalAddr().(*net.UDPAddr).AddrPort()}
	refused := make(chan struct{})
	go func() {
		// The server refuses the first request, an Allocate, and answers
		// no other.
		defer close(refused)
		buf := make([]byte, 1500)
		n, from, err := server.ReadFromUDPAddrPort(buf)
		var m stun.Message
		if err != nil || stun.Parse(buf[:n], &m) != nil {
			return
		}
		b := stun.NewBuilder(nil, 0x0113, m.Transaction)
		b.AddErrorCode(403, "Forbidden")
		_, _ = server.WriteToUDPAddrPort(b.Bytes(), from)
	}()
	none := func(what string, err error, want error) {
		t.Helper()
		if !errors.Is(err, want) || l.RelayDone() != nil || l.RelayErr() != nil {
			t.Fatalf("%s: Relay returned %v, want %v; RelayDone %v, RelayErr %v", what, err, want, l.RelayDone(), l.RelayErr())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = l.Relay(ctx, cfg)
	<-refused
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("refused allocation: Relay returned %v", err)
	}
	none("refused allocation", nil, nil)
	short, cancelShort := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancelShort()
	none("server silent", l.Relay(short, cfg), context.DeadlineExceeded)
	_ = l.Close()
	if err := l.Relay(ctx, cfg); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed listener: Relay returned %v", err)
	}
}

// relayedConn is a connection opening, answered over a signaling service,
// with the checks of its peer.
type relayedConn struct {
	c     *Conn
	check func(proved bool) []byte
}

// openRelayed has l answer an offer over a signaling service with the
// connection ID id.
func openRelayed(t *testing.T, l *Listener, id uint64) relayedConn {
	t.Helper()
	answerSDP, _, err := l.answer(idleOffer(), nil, nil, &signalKey{"client", id}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	answer, err := parseDescription(answerSDP)
	if err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	c := l.signals[signalKey{"client", id}]
	l.mu.Unlock()
	return relayedConn{c: c, check: func(proved bool) []byte {
		pwd := "not the password of the connection"
		if proved {
			pwd = answer.pwd
		}
		b := stun.NewBuilder(nil, stun.BindingRequest, [12]byte{1})
		b.AddString(stun.AttrUsername, answer.ufrag+":abcd")
		b.AddUint64(stun.AttrICEControlling, 1)
		b.AddIntegrity(hmac.New(sha1.New, []byte(pwd)))
		b.AddFingerprint()
		return b.Bytes()
	}}
}

// TestRelayRoutes routes what goes to a peer through the relay once a
// check that came through it proved the connection, until the connection
// closes; a check that proves nothing makes no route nor undoes one, and
// one that came directly makes none. A peer heard directly is routed
// through the relay again by its next check through it, and the route of
// an address another connection proved since stays as the first closes.
func TestRelayRoutes(t *testing.T) {
	l := listen(t, ListenConfig{AllowAnonymous: true})
	r := newRelay(l, turn.Config{Server: netip.MustParseAddrPort("127.0.0.1:3478")})
	l.relay.Store(r)
	first, second := openRelayed(t, l, 1), openRelayed(t, l, 2)
	peer, direct := netip.MustParseAddrPort("192.0.2.1:5000"), netip.MustParseAddrPort("192.0.2.2:5000")
	var msg stun.Message
	l.dispatch(first.check(false), peer, r, &msg)
	if l.Relayed(peer) {
		t.Fatal("a check that proved nothing made a route")
	}
	l.dispatch(first.check(true), peer, r, &msg)
	if !l.Relayed(peer) {
		t.Fatal("a check that proved the connection made no route")
	}
	l.dispatch(first.check(false), peer, r, &msg)
	if !l.Relayed(peer) {
		t.Fatal("a check that proved nothing undid a route")
	}
	l.dispatch(first.check(true), direct, nil, &msg)
	if l.Relayed(direct) {
		t.Fatal("a check that came directly made a route")
	}
	r.heardDirect(peer)
	if l.Relayed(peer) {
		t.Fatal("a peer heard directly still routed through the relay")
	}
	l.dispatch(first.check(true), peer, r, &msg)
	if !l.Relayed(peer) {
		t.Fatal("a check through the relay of a peer heard directly made no route")
	}
	l.dispatch(second.check(true), peer, r, &msg)
	first.c.abort(net.ErrClosed)
	if !l.Relayed(peer) {
		t.Fatal("the route of an address another connection proved went with the first")
	}
	second.c.abort(net.ErrClosed)
	if l.Relayed(peer) {
		t.Fatal("the route of a connection that closed kept")
	}
}
