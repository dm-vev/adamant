package nethernet

import (
	"crypto/tls"
	"io"
	"net"
	"sync"
	"time"
)

const (
	// sniffTimeout bounds the wait for the first byte of a connection of
	// the signaling.
	sniffTimeout = 5 * time.Second
	// maxSniffing bounds the connections waiting for their first byte, so
	// that connections that send nothing hold no more than that.
	maxSniffing = 1024
)

// sniffListener hands out the connections of a TCP listener as HTTPS ones
// if they open with a TLS handshake and as plain HTTP ones otherwise, so
// that one port serves both: clients join by an https:// address with a
// certificate they trust, or by an http:// one trusting the key of the
// server on first use.
type sniffListener struct {
	net.Listener
	tls      *tls.Config
	conns    chan net.Conn
	sniffing chan struct{}
	done     chan struct{}
	once     sync.Once
	// err is why the listener ended, set before done is closed.
	err error
}

func newSniffListener(ln net.Listener, cfg *tls.Config) *sniffListener {
	l := &sniffListener{Listener: ln, tls: cfg, conns: make(chan net.Conn), sniffing: make(chan struct{}, maxSniffing), done: make(chan struct{})}
	go l.accept()
	return l
}

func (l *sniffListener) end(err error) {
	l.once.Do(func() {
		l.err = err
		close(l.done)
	})
}

func (l *sniffListener) accept() {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			l.end(err)
			return
		}
		select {
		case l.sniffing <- struct{}{}:
			go l.sniff(c)
		default:
			// As many wait for their first byte as may: this one waits
			// for none.
			_ = c.Close()
		}
	}
}

// sniff reads the first byte of a connection: 0x16 opens a TLS record of
// a handshake, which no request of HTTP starts with.
func (l *sniffListener) sniff(c net.Conn) {
	defer func() { <-l.sniffing }()
	_ = c.SetReadDeadline(time.Now().Add(sniffTimeout))
	var b [1]byte
	if _, err := io.ReadFull(c, b[:]); err != nil {
		_ = c.Close()
		return
	}
	_ = c.SetReadDeadline(time.Time{})
	var conn net.Conn = &peekedConn{Conn: c, first: b[0], peeked: true}
	if b[0] == 0x16 {
		conn = tls.Server(conn, l.tls)
	}
	select {
	case l.conns <- conn:
	case <-l.done:
		_ = conn.Close()
	}
}

func (l *sniffListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, l.err
	}
}

func (l *sniffListener) Close() error {
	err := l.Listener.Close()
	l.end(net.ErrClosed)
	return err
}

// peekedConn reads the byte peeked off it first.
type peekedConn struct {
	net.Conn
	first  byte
	peeked bool
}

func (c *peekedConn) Read(p []byte) (int, error) {
	if c.peeked && len(p) > 0 {
		p[0], c.peeked = c.first, false
		return 1, nil
	}
	return c.Conn.Read(p)
}
