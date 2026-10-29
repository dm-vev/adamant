package nethernet

import (
	"context"
	"net"
	"net/netip"
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

	// peers holds the addresses heard through the relay last, not
	// directly.
	peersMu sync.RWMutex
	peers   map[netip.AddrPort]struct{}
}

// keepaliveInterval paces the datagrams that keep the mapping of a NAT in
// front of the listener to the TURN server.
const keepaliveInterval = 15 * time.Second

// maxRelayPeers bounds the peers a relay keeps the path of.
const maxRelayPeers = 4096

// Relay allocates a relayed address on the TURN server of cfg and announces
// it, with the address the TURN server saw the listener at, in the answers
// to offers that come over a signaling service: clients that cannot reach
// the sockets of the listener connect through it. It returns once the
// allocation was made, and replaces the relay of an earlier call.
func (l *Listener) Relay(ctx context.Context, cfg turn.Config) error {
	r := &relay{l: l, sock: l.socks[0], server: cfg.Server, ready: make(chan struct{}), peers: make(map[netip.AddrPort]struct{})}
	r.client = turn.New(cfg, func(b []byte, to netip.AddrPort) { _, _ = r.sock.WriteToUDPAddrPort(b, to) })
	if old := l.relay.Swap(r); old != nil {
		old.close()
	}
	select {
	case <-l.done:
		r.close()
		return net.ErrClosed
	default:
	}
	r.mu.Lock()
	now := time.Now()
	r.client.Start(now)
	r.alive = now
	r.schedule()
	r.mu.Unlock()
	select {
	case <-r.ready:
	case <-ctx.Done():
		l.relay.CompareAndSwap(r, nil)
		r.close()
		return ctx.Err()
	}
	r.mu.Lock()
	err := r.client.Err()
	r.mu.Unlock()
	if err != nil {
		l.relay.CompareAndSwap(r, nil)
		r.close()
		return err
	}
	return nil
}

// handle handles a datagram from the TURN server: the data of a peer goes
// to its connection as though the peer sent it to the sockets.
func (r *relay) handle(b []byte, msg *stun.Message) {
	r.mu.Lock()
	peer, data, ok := r.client.HandleDatagram(b, time.Now())
	r.reschedule()
	r.mu.Unlock()
	if !ok {
		return
	}
	relayedDatagrams.Inc()
	r.peersMu.RLock()
	_, known := r.peers[peer]
	r.peersMu.RUnlock()
	if !known {
		r.peersMu.Lock()
		if len(r.peers) < maxRelayPeers {
			r.peers[peer] = struct{}{}
		}
		r.peersMu.Unlock()
	}
	r.l.dispatch(data, peer, msg)
}

// heardDirect notes that a peer reached the sockets themselves: what goes
// to it goes there again.
func (r *relay) heardDirect(peer netip.AddrPort) {
	r.peersMu.RLock()
	_, known := r.peers[peer]
	r.peersMu.RUnlock()
	if known {
		r.peersMu.Lock()
		delete(r.peers, peer)
		r.peersMu.Unlock()
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

// permit lets a peer of an offer reach the listener through the relay.
func (r *relay) permit(peer netip.Addr) {
	if !peer.IsValid() || peer.IsUnspecified() || peer.IsLoopback() && !r.server.Addr().IsLoopback() {
		return
	}
	r.mu.Lock()
	r.client.Permit(peer, time.Now())
	r.reschedule()
	r.mu.Unlock()
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
// made or failed. r.mu is held.
func (r *relay) reschedule() {
	if r.client.Allocated() || r.client.Err() != nil {
		r.signal.Do(func() { close(r.ready) })
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


// Relayed reports whether what goes to a peer goes through the TURN relay
// of the listener.
func (l *Listener) Relayed(peer netip.AddrPort) bool {
	r := l.relay.Load()
	return r != nil && r.via(peer)
}
