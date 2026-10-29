package proxy

import (
	"net"
	"os"
	"sync"
	"time"
)

// maxQueued bounds the batches queued for a player that reads none.
const maxQueued = 1024

// Player is a player on a link: its batches go over the link framed with
// its ID. It is a bedrock.MessageConn, and hands the batches it receives
// to a receiver as a NetherNet connection does.
type Player struct {
	l     *Link
	id    uint64
	token Token

	mu       sync.Mutex
	queue    [][]byte
	readable chan struct{}
	receiver func([]byte)
	closer   func(error)
	err      error
	deadline time.Time
	done     chan struct{}
}

func newPlayer(l *Link, id uint64, t Token) *Player {
	return &Player{l: l, id: id, token: t, readable: make(chan struct{}, 1), done: make(chan struct{})}
}

// ID returns the ID of the player on its link.
func (p *Player) ID() uint64 { return p.id }

// Token returns what the proxy told of the player.
func (p *Player) Token() Token { return p.token }

// deliver hands a batch to the receiver or queues it.
func (p *Player) deliver(b []byte) {
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		return
	}
	if recv := p.receiver; recv != nil {
		p.mu.Unlock()
		recv(b)
		return
	}
	if len(p.queue) >= maxQueued {
		p.mu.Unlock()
		p.close("too many batches queued")
		return
	}
	p.queue = append(p.queue, b)
	p.mu.Unlock()
	select {
	case p.readable <- struct{}{}:
	default:
	}
}

// ReadMessage returns the next batch the other end sent.
func (p *Player) ReadMessage() ([]byte, error) {
	for {
		p.mu.Lock()
		if len(p.queue) > 0 {
			b := p.queue[0]
			p.queue[0] = nil
			p.queue = p.queue[1:]
			p.mu.Unlock()
			return b, nil
		}
		err, deadline := p.err, p.deadline
		p.mu.Unlock()
		if err != nil {
			return nil, err
		}
		var expired <-chan time.Time
		if !deadline.IsZero() {
			d := time.Until(deadline)
			if d <= 0 {
				return nil, os.ErrDeadlineExceeded
			}
			t := time.NewTimer(d)
			defer t.Stop()
			expired = t.C
		}
		select {
		case <-p.readable:
		case <-p.done:
		case <-expired:
		}
	}
}

// Send sends a batch to the other end.
func (p *Player) Send(b []byte) error {
	p.mu.Lock()
	err := p.err
	p.mu.Unlock()
	if err != nil {
		return err
	}
	return p.l.write(frameBatch, p.id, b)
}

// SetReceiver hands the batches received from now on to recv, the queued
// ones first, on the goroutine of the link. recv must not block.
func (p *Player) SetReceiver(recv func([]byte)) {
	p.mu.Lock()
	queued := p.queue
	p.queue = nil
	p.receiver = recv
	p.mu.Unlock()
	for _, b := range queued {
		recv(b)
	}
}

// OnClose has f called once the player closed, at once if it did.
func (p *Player) OnClose(f func(error)) {
	p.mu.Lock()
	err := p.err
	if err == nil {
		p.closer = f
	}
	p.mu.Unlock()
	if err != nil {
		f(err)
	}
}

// SetReadDeadline sets the deadline of ReadMessage.
func (p *Player) SetReadDeadline(t time.Time) error {
	p.mu.Lock()
	p.deadline = t
	p.mu.Unlock()
	select {
	case p.readable <- struct{}{}:
	default:
	}
	return nil
}

// RemoteAddr returns the address the player connected to the proxy from.
func (p *Player) RemoteAddr() net.Addr {
	if a, err := net.ResolveUDPAddr("udp", p.token.Addr); err == nil && a.IP != nil {
		return a
	}
	return &net.UDPAddr{}
}

// Close closes the player and tells the other end.
func (p *Player) Close() error {
	p.close("closed")
	return nil
}

func (p *Player) close(reason string) {
	if p.end(ErrClosed, true) {
		_ = p.l.write(frameClose, p.id, []byte(reason))
		p.l.forget(p.id)
	}
}

// end closes the player for err and reports whether it was open. The
// batches queued are dropped if drop is set, as a player closed here reads
// no more, and read before err otherwise: those the other end sent before
// it closed the player still reach it.
func (p *Player) end(err error, drop bool) bool {
	p.mu.Lock()
	if p.err != nil {
		p.mu.Unlock()
		return false
	}
	p.err = err
	closer := p.closer
	if drop {
		p.queue = nil
	}
	close(p.done)
	p.mu.Unlock()
	if closer != nil {
		closer(err)
	}
	return true
}

// Listener accepts links of proxies on a TCP address and their players.
type Listener struct {
	ln      net.Listener
	secret  []byte
	players chan *Player
	done    chan struct{}
	once    sync.Once
}

// Listen listens for proxies on addr. secret is shared with them; the
// address should be one only they reach.
func Listen(addr string, secret []byte) (*Listener, error) {
	if len(secret) < 16 {
		return nil, errShortSecret
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	l := &Listener{ln: ln, secret: secret, players: make(chan *Player, 64), done: make(chan struct{})}
	go l.serve()
	return l, nil
}

func (l *Listener) serve() {
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			_ = l.Close()
			return
		}
		link := newLink(conn, l.secret, true)
		go func() {
			for {
				p, err := link.Accept()
				if err != nil {
					return
				}
				select {
				case l.players <- p:
				case <-l.done:
					_ = link.Close()
					return
				}
			}
		}()
	}
}

// Accept returns the next player a proxy opened.
func (l *Listener) Accept() (*Player, error) {
	select {
	case p := <-l.players:
		return p, nil
	case <-l.done:
		return nil, ErrClosed
	}
}

// Addr returns the address of the listener.
func (l *Listener) Addr() net.Addr { return l.ln.Addr() }

// Close stops accepting proxies.
func (l *Listener) Close() error {
	l.once.Do(func() {
		close(l.done)
		_ = l.ln.Close()
	})
	return nil
}
