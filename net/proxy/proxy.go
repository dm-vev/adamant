// Package proxy carries the players of a proxy to the servers behind it
// over one connection a server, instead of a NetherNet connection a
// player: the transport of the game, DTLS and SCTP, is of no use on a
// network the proxy and its servers trust. A Link frames the batches of
// every player of the connection with the ID of the player, and the
// proxy names a player it opens with a token it signs with a secret the
// server shares: who the player is and the address it came from, as the
// proxy proved them.
package proxy

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Frames of a link: a uvarint length, then the type, the uvarint ID of the
// player and the payload.
const (
	frameOpen  = 1 // proxy to server: a signed Token
	frameBatch = 2 // either way: a batch of packets
	frameClose = 3 // either way: why, as text
)

// Limits of a link.
const (
	// MaxFrame bounds a frame: a batch a player may send, decompressed,
	// and its header.
	MaxFrame = 8<<20 + 64
	// maxPlayers bounds the players open on a link at once.
	maxPlayers = 4096
	// tokenLifetime is how long after it was signed a token is taken.
	tokenLifetime = time.Minute
)

// Token names a player the proxy opens: who it is and where it came from,
// as the proxy proved them in the login of the player. The server trusts
// it as far as it trusts the proxy that signed it.
type Token struct {
	// Identity is the identity of the player as its login named it.
	Identity json.RawMessage `json:"identity"`
	// ClientData is the client data of the login.
	ClientData json.RawMessage `json:"clientData"`
	// XboxLive reports whether the login carried a token of Xbox Live.
	XboxLive bool `json:"xboxLive"`
	// Addr is the address the player connected to the proxy from.
	Addr string `json:"addr"`
	// Protocol is the protocol the client speaks.
	Protocol int32 `json:"protocol"`
	// IssuedAt is when the proxy signed the token, in Unix seconds.
	IssuedAt int64 `json:"iat"`
}

// Errors of a link.
var (
	ErrClosed       = errors.New("proxy: closed")
	errFrame        = errors.New("proxy: malformed frame")
	errToken        = errors.New("proxy: token not signed with the secret of the link")
	errTokenExpired = errors.New("proxy: token expired")
)

// signToken returns a token signed with secret: its JSON, then the
// HMAC-SHA256 of it.
func signToken(t Token, secret []byte) ([]byte, error) {
	b, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(b)
	return mac.Sum(b), nil
}

// verifyToken checks a token signed with secret and not older than its
// lifetime at now.
func verifyToken(b, secret []byte, now time.Time) (Token, error) {
	if len(b) < sha256.Size {
		return Token{}, errToken
	}
	body, sum := b[:len(b)-sha256.Size], b[len(b)-sha256.Size:]
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if !hmac.Equal(mac.Sum(nil), sum) {
		return Token{}, errToken
	}
	var t Token
	if err := json.Unmarshal(body, &t); err != nil {
		return Token{}, fmt.Errorf("proxy: token: %w", err)
	}
	issued := time.Unix(t.IssuedAt, 0)
	if now.Sub(issued) > tokenLifetime || issued.Sub(now) > tokenLifetime {
		return Token{}, errTokenExpired
	}
	return t, nil
}

// Link is the connection between a proxy and a server, either end of it.
// Its methods are safe for concurrent use.
type Link struct {
	conn   net.Conn
	secret []byte
	server bool

	writeMu sync.Mutex
	w       *bufio.Writer

	mu      sync.Mutex
	players map[uint64]*Player
	nextID  uint64
	err     error

	accept chan *Player
	done   chan struct{}
	once   sync.Once
}

// Dial connects a proxy to a server at addr, both ends sharing secret.
func Dial(addr string, secret []byte) (*Link, error) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	return newLink(conn, secret, false), nil
}

func newLink(conn net.Conn, secret []byte, server bool) *Link {
	l := &Link{
		conn:    conn,
		secret:  secret,
		server:  server,
		w:       bufio.NewWriterSize(conn, 64<<10),
		players: make(map[uint64]*Player),
		accept:  make(chan *Player, 64),
		done:    make(chan struct{}),
	}
	go l.read()
	return l
}

// Open opens a player on a link a proxy dialed: the server reads the token
// when it accepts the player.
func (l *Link) Open(t Token) (*Player, error) {
	if l.server {
		return nil, errors.New("proxy: a server does not open players")
	}
	t.IssuedAt = time.Now().Unix()
	tok, err := signToken(t, l.secret)
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	if l.err != nil {
		l.mu.Unlock()
		return nil, l.err
	}
	if len(l.players) >= maxPlayers {
		l.mu.Unlock()
		return nil, errors.New("proxy: too many players on the link")
	}
	l.nextID++
	p := newPlayer(l, l.nextID, t)
	l.players[p.id] = p
	l.mu.Unlock()
	if err := l.write(frameOpen, p.id, tok); err != nil {
		return nil, err
	}
	return p, nil
}

// Accept returns the next player the proxy opened on a link of a server.
func (l *Link) Accept() (*Player, error) {
	select {
	case p := <-l.accept:
		return p, nil
	case <-l.done:
		return nil, l.Err()
	}
}

// Err returns why the link ended, or nil while it lives.
func (l *Link) Err() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.err
}

// Close closes the link and every player on it.
func (l *Link) Close() error {
	l.end(ErrClosed)
	return nil
}

// end ends the link with err.
func (l *Link) end(err error) {
	l.once.Do(func() {
		l.mu.Lock()
		l.err = err
		players := l.players
		l.players = nil
		l.mu.Unlock()
		close(l.done)
		_ = l.conn.Close()
		for _, p := range players {
			p.end(err)
		}
	})
}

// write sends a frame and flushes it.
func (l *Link) write(typ byte, id uint64, payload []byte) error {
	var head [1 + 2*binary.MaxVarintLen64]byte
	n := binary.PutUvarint(head[:], uint64(1+uvarintLen(id)+len(payload)))
	head[n] = typ
	n++
	n += binary.PutUvarint(head[n:], id)
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if _, err := l.w.Write(head[:n]); err != nil {
		l.end(err)
		return err
	}
	if _, err := l.w.Write(payload); err != nil {
		l.end(err)
		return err
	}
	if err := l.w.Flush(); err != nil {
		l.end(err)
		return err
	}
	return nil
}

func uvarintLen(v uint64) int {
	n := 1
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}

// read reads the frames of the link until it ends. A frame that does not
// parse, or a token that does not verify, ends the link: its other end is
// not the proxy or the server it should be.
func (l *Link) read() {
	r := bufio.NewReaderSize(l.conn, 64<<10)
	for {
		size, err := binary.ReadUvarint(r)
		if err != nil {
			l.end(err)
			return
		}
		if size < 2 || size > MaxFrame {
			l.end(errFrame)
			return
		}
		frame := make([]byte, size)
		if _, err := io.ReadFull(r, frame); err != nil {
			l.end(err)
			return
		}
		typ := frame[0]
		id, n := binary.Uvarint(frame[1:])
		if n <= 0 {
			l.end(errFrame)
			return
		}
		payload := frame[1+n:]
		if err := l.handle(typ, id, payload); err != nil {
			l.end(err)
			return
		}
	}
}

// handle handles a frame.
func (l *Link) handle(typ byte, id uint64, payload []byte) error {
	switch typ {
	case frameOpen:
		if !l.server {
			return errFrame
		}
		t, err := verifyToken(payload, l.secret, time.Now())
		if err != nil {
			return err
		}
		l.mu.Lock()
		if _, ok := l.players[id]; ok || len(l.players) >= maxPlayers || l.err != nil {
			l.mu.Unlock()
			return errFrame
		}
		p := newPlayer(l, id, t)
		l.players[id] = p
		l.mu.Unlock()
		select {
		case l.accept <- p:
		case <-l.done:
		}
	case frameBatch:
		if p := l.player(id); p != nil {
			p.deliver(payload)
		}
	case frameClose:
		if p := l.player(id); p != nil {
			p.end(&ClosedError{Reason: string(payload)})
			l.forget(id)
		}
	default:
		return errFrame
	}
	return nil
}

func (l *Link) player(id uint64) *Player {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.players[id]
}

func (l *Link) forget(id uint64) {
	l.mu.Lock()
	delete(l.players, id)
	l.mu.Unlock()
}

// ClosedError is the reason the other end of a link closed a player for.
type ClosedError struct{ Reason string }

func (e *ClosedError) Error() string { return "proxy: player closed: " + e.Reason }

var errShortSecret = errors.New("proxy: the secret is shorter than 16 bytes")
