package nethernet

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"sync"
	"time"

	"github.com/dm-vev/adamant/net/stun"
	"github.com/dm-vev/adamant/net/turn"
)

// relay is an allocation on a TURN server that clients reach the listener
// through when they cannot reach its sockets, as clients of Xbox Live on
// other networks cannot reach a server behind NAT. Datagrams of a peer
// come from the server in Data indications and go to it in Send ones.
type relay struct {
	l      *Listener
	sock   *net.UDPConn
	server netip.AddrPort

	mu     sync.Mutex
	client *turn.Client
	timer  *time.Timer
	next   time.Time
	alive  time.Time
	ready  chan struct{}
	signal sync.Once
	closed bool
	// failed is closed once the allocation failed.
	failed   chan struct{}
	failOnce sync.Once
	// held counts the connections holding the permission of each peer
	// address, and holds holds the addresses each connection holds.
	held  map[netip.Addr]int
	holds map[*Conn][]netip.Addr

	// peers holds the addresses that proved their connections with a check
	// through the relay, and were not heard directly since: what goes to
	// them goes through the relay. Each is of a connection, which forgets
	// it as it closes.
	peersMu sync.RWMutex
	peers   map[netip.AddrPort]struct{}
}

// keepaliveInterval paces the datagrams that keep the mapping of a NAT in
// front of the listener to the TURN server.
const keepaliveInterval = 15 * time.Second

// Relay allocates a relayed address on the TURN server of cfg and announces
// it, with the address the TURN server saw the listener at, in the answers
// to offers that come over a signaling service: clients that cannot reach
// the sockets of the listener connect through it. It returns once the
// allocation was made. The relay of an earlier call that holds its
// allocation on the same TURN server is kept, with the connections through
// it, and Relay returns at once; one that failed, as RelayDone tells, or
// is on another server is replaced.
func (l *Listener) Relay(ctx context.Context, cfg turn.Config) error {
	if r := l.relay.Load(); r != nil && r.serves(cfg) {
		return nil
	}
	r := newRelay(l, cfg)
	if old := l.relay.Swap(r); old != nil {
		old.close()
	}
	select {
	case <-l.done:
		r.close()
		return net.ErrClosed
	default:
	}
	r.start()
	if err := r.wait(ctx); err != nil {
		l.relay.CompareAndSwap(r, nil)
		r.close()
		return err
	}
	return nil
}

// start asks the TURN server for the allocation.
func (r *relay) start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	r.client.Start(now)
	r.alive = now
	r.schedule()
}

// wait waits until the allocation was made, and returns why it was not.
func (r *relay) wait(ctx context.Context) error {
	select {
	case <-r.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client.Err()
}

// newRelay returns a relay of l on the TURN server of cfg, not started.
func newRelay(l *Listener, cfg turn.Config) *relay {
	r := &relay{
		l:      l,
		sock:   l.socks[0],
		server: cfg.Server,
		ready:  make(chan struct{}),
		failed: make(chan struct{}),
		held:   make(map[netip.Addr]int),
		holds:  make(map[*Conn][]netip.Addr),
		peers:  make(map[netip.AddrPort]struct{}),
	}
	r.client = turn.New(cfg, func(b []byte, to netip.AddrPort) { _, _ = r.sock.WriteToUDPAddrPort(b, to) })
	return r
}

// serves reports whether the relay holds its allocation on the TURN server
// of cfg.
func (r *relay) serves(cfg turn.Config) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.server == cfg.Server && r.client.Allocated()
}

// handle handles a datagram from the TURN server: the data of a peer goes
// to its connection as though the peer sent it to the sockets.
func (r *relay) handle(b []byte, msg *stun.Message) {
	r.mu.Lock()
	peer, data, ok := r.client.HandleDatagram(b, time.Now())
	r.reschedule()
	r.mu.Unlock()
	if ok {
		relayedDatagrams.Inc()
		r.l.dispatch(data, peer, r, msg)
	}
}

// route has what goes to a peer go through the relay, and reports whether
// it did not before.
func (r *relay) route(peer netip.AddrPort) bool {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()
	if _, ok := r.peers[peer]; ok {
		return false
	}
	r.peers[peer] = struct{}{}
	return true
}

// unroute has what goes to a peer go to it directly.
func (r *relay) unroute(peer netip.AddrPort) {
	r.peersMu.Lock()
	delete(r.peers, peer)
	r.peersMu.Unlock()
}

// heardDirect notes that a peer reached the sockets themselves: what goes
// to it goes there again.
func (r *relay) heardDirect(peer netip.AddrPort) {
	if r.via(peer) {
		r.unroute(peer)
	}
}

// via reports whether what goes to a peer goes through the relay.
func (r *relay) via(peer netip.AddrPort) bool {
	r.peersMu.RLock()
	defer r.peersMu.RUnlock()
	_, ok := r.peers[peer]
	return ok
}

// send sends a datagram to a peer through the relay.
func (r *relay) send(peer netip.AddrPort, b []byte) {
	r.mu.Lock()
	_ = r.client.Send(peer, b)
	r.mu.Unlock()
}

// hold has c hold the permission of a peer address, which lets the peer
// reach the listener through the relay, unless c holds maxPairs already.
// The permission is asked for when the first connection holds it.
func (r *relay) hold(c *Conn, peer netip.Addr) {
	peer = peer.Unmap()
	if !peer.IsValid() || peer.IsUnspecified() || peer.IsLoopback() && !r.server.Addr().IsLoopback() {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	addrs := r.holds[c]
	if len(addrs) >= maxPairs || slices.Contains(addrs, peer) {
		return
	}
	r.holds[c] = append(addrs, peer)
	if r.held[peer]++; r.held[peer] == 1 {
		r.client.Permit(peer, time.Now())
		r.reschedule()
	}
}

// keep releases the permissions c holds but that of peer, all of them
// for an invalid peer. A permission no connection holds is not renewed.
func (r *relay) keep(c *Conn, peer netip.Addr) {
	peer = peer.Unmap()
	r.mu.Lock()
	defer r.mu.Unlock()
	var kept []netip.Addr
	for _, a := range r.holds[c] {
		if a == peer {
			kept = append(kept, a)
		} else if r.held[a]--; r.held[a] == 0 {
			delete(r.held, a)
			r.client.Release(a)
		}
	}
	if kept == nil {
		delete(r.holds, c)
	} else {
		r.holds[c] = kept
	}
}

// candidates returns the server reflexive and the relayed address of the
// allocation, if there is one.
func (r *relay) candidates() (reflexive, relayed netip.AddrPort, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.client.Allocated() {
		return netip.AddrPort{}, netip.AddrPort{}, false
	}
	return r.client.Mapped(), r.client.Relayed(), true
}

// schedule arms the timer for the next deadline of the client or the
// keepalive. r.mu is held.
func (r *relay) schedule() {
	if r.closed {
		return
	}
	at := r.alive.Add(keepaliveInterval)
	if d := r.client.Deadline(); !d.IsZero() && d.Before(at) {
		at = d
	}
	if r.timer != nil && r.next.Equal(at) {
		return
	}
	r.next = at
	if r.timer == nil {
		r.timer = time.AfterFunc(time.Until(at), r.fire)
	} else {
		r.timer.Reset(time.Until(at))
	}
}

// reschedule arms the timer anew and tells Relay once the allocation was
// made or failed, and RelayDone once it failed. r.mu is held.
func (r *relay) reschedule() {
	if r.client.Allocated() || r.client.Err() != nil {
		r.signal.Do(func() { close(r.ready) })
	}
	if r.client.Err() != nil {
		r.failOnce.Do(func() { close(r.failed) })
	}
	r.schedule()
}

func (r *relay) fire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	now := time.Now()
	r.client.HandleTimeout(now)
	if !now.Before(r.alive.Add(keepaliveInterval)) {
		r.client.Keepalive()
		r.alive = now
	}
	r.reschedule()
}

// close gives the allocation back.
func (r *relay) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	if r.timer != nil {
		r.timer.Stop()
	}
	r.client.Close(time.Now())
	r.signal.Do(func() { close(r.ready) })
}

// RelayDone returns a channel closed once the allocation of the current
// relay failed, nil without a relay. Relay, called again with fresh
// credentials, makes a new one.
func (l *Listener) RelayDone() <-chan struct{} {
	if r := l.relay.Load(); r != nil {
		return r.failed
	}
	return nil
}

// RelayErr returns why the allocation of the current relay failed, or nil.
func (l *Listener) RelayErr() error {
	r := l.relay.Load()
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client.Err()
}

// Relayed reports whether what goes to a peer goes through the TURN relay
// of the listener.
func (l *Listener) Relayed(peer netip.AddrPort) bool {
	r := l.relay.Load()
	return r != nil && r.via(peer)
}
