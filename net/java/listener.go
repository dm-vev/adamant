package java

import (
	"encoding/json"
	"errors"
	"net"
	"sync"
	"time"
)

// States a handshake picks.
const (
	stateStatus   = 1
	stateLogin    = 2
	stateTransfer = 3
)

// Status is what a server list shows of a server.
type Status struct {
	// Name is the version of the game the server names, as "Adamant".
	Name string
	// Protocol is the protocol of the server; zero answers each client
	// with its own, so that every version lists the server as one it may
	// join.
	Protocol int32
	// Description is the message of the day.
	Description string
	// Online and Max count the players.
	Online, Max int
}

// Login is a client that started to log in: its name and UUID as it named
// them, and its connection, at the start of the login.
type Login struct {
	Name     string
	UUID     [16]byte
	Protocol int32
	Conn     *Conn
}

// Disconnect ends a login with a message.
func (l *Login) Disconnect(message string) error {
	text, _ := json.Marshal(map[string]string{"text": message})
	err := l.Conn.WritePacket(0x00, AppendString(nil, string(text)))
	_ = l.Conn.Close()
	return err
}

// Listener accepts Java connections: it answers status requests itself and
// hands logins to Accept.
type Listener struct {
	ln     net.Listener
	status func() Status
	logins chan *Login
	// handshakes holds a slot for each connection in its handshake.
	handshakes chan struct{}
	done       chan struct{}
	once       sync.Once
}

// maxHandshakes bounds the connections in their handshake at once, so that
// connections that send little hold no more than that.
const maxHandshakes = 1024

// Listen listens on a TCP address; status gives the status asked for.
func Listen(addr string, status func() Status) (*Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	l := &Listener{ln: ln, status: status, logins: make(chan *Login, 16), handshakes: make(chan struct{}, maxHandshakes), done: make(chan struct{})}
	go l.serve()
	return l, nil
}

// Addr returns the address of the listener.
func (l *Listener) Addr() net.Addr { return l.ln.Addr() }

// Accept returns the next login.
func (l *Listener) Accept() (*Login, error) {
	select {
	case login := <-l.logins:
		return login, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Close stops the listener.
func (l *Listener) Close() error {
	l.once.Do(func() {
		close(l.done)
		_ = l.ln.Close()
	})
	return nil
}

func (l *Listener) serve() {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			_ = l.Close()
			return
		}
		select {
		case l.handshakes <- struct{}{}:
			go func() {
				defer func() { <-l.handshakes }()
				l.handle(c)
			}()
		default:
			_ = c.Close()
		}
	}
}

// handshakeTimeout bounds the handshake and a status exchange.
const handshakeTimeout = 10 * time.Second

// handle reads the handshake of a connection and serves its state.
func (l *Listener) handle(nc net.Conn) {
	_ = nc.SetDeadline(time.Now().Add(handshakeTimeout))
	c := NewConn(nc)
	id, fields, err := c.ReadPacket()
	if err != nil || id != 0x00 {
		_ = c.Close()
		return
	}
	r := NewReader(fields)
	protocol := r.VarInt()
	_ = r.String() // the address the client dialled
	_ = r.Uint16()
	next := r.VarInt()
	if r.Err() != nil || r.Len() != 0 {
		_ = c.Close()
		return
	}
	switch next {
	case stateStatus:
		_ = l.serveStatus(c, protocol)
		_ = c.Close()
	case stateLogin, stateTransfer:
		id, fields, err := c.ReadPacket()
		if err != nil || id != 0x00 {
			_ = c.Close()
			return
		}
		r := NewReader(fields)
		name := r.String()
		uuid := r.UUID()
		if r.Err() != nil || name == "" || len(name) > 16 {
			_ = c.Close()
			return
		}
		_ = nc.SetDeadline(time.Time{})
		select {
		case l.logins <- &Login{Name: name, UUID: uuid, Protocol: protocol, Conn: c}:
		case <-l.done:
			_ = c.Close()
		default:
			_ = (&Login{Conn: c}).Disconnect("The server is busy.")
		}
	default:
		_ = c.Close()
	}
}

// serveStatus answers a status request and a ping.
func (l *Listener) serveStatus(c *Conn, protocol int32) error {
	for range 2 {
		id, fields, err := c.ReadPacket()
		if err != nil {
			return err
		}
		switch id {
		case 0x00:
			st := l.status()
			if st.Protocol != 0 {
				protocol = st.Protocol
			}
			b, err := json.Marshal(map[string]any{
				"version":     map[string]any{"name": st.Name, "protocol": protocol},
				"players":     map[string]any{"max": st.Max, "online": st.Online},
				"description": map[string]any{"text": st.Description},
			})
			if err != nil {
				return err
			}
			if err := c.WritePacket(0x00, AppendString(nil, string(b))); err != nil {
				return err
			}
		case 0x01:
			if len(fields) != 8 {
				return errors.New("java: malformed ping")
			}
			return c.WritePacket(0x01, fields)
		default:
			return errors.New("java: unknown status packet")
		}
	}
	return nil
}

// QueryStatus asks a Java server at addr for its status, as a server list
// does, and returns the status JSON and the round trip of the ping.
func QueryStatus(addr string, timeout time.Duration) ([]byte, time.Duration, error) {
	nc, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, 0, err
	}
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(timeout))
	c := NewConn(nc)
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := net.LookupPort("tcp", portStr)
	hs := AppendVarInt(nil, -1)
	hs = AppendString(hs, host)
	hs = append(hs, byte(port>>8), byte(port))
	hs = AppendVarInt(hs, stateStatus)
	if err := c.WritePacket(0x00, hs); err != nil {
		return nil, 0, err
	}
	if err := c.WritePacket(0x00, nil); err != nil {
		return nil, 0, err
	}
	id, fields, err := c.ReadPacket()
	if err != nil {
		return nil, 0, fmtErr("status", err)
	}
	r := NewReader(fields)
	status := r.String()
	if id != 0x00 || r.Err() != nil {
		return nil, 0, errors.New("java: malformed status")
	}
	start := time.Now()
	ping := make([]byte, 8)
	ping[7] = 42
	if err := c.WritePacket(0x01, ping); err != nil {
		return nil, 0, err
	}
	id, fields, err = c.ReadPacket()
	if err != nil || id != 0x01 || len(fields) != 8 || fields[7] != 42 {
		return nil, 0, errors.New("java: malformed pong")
	}
	return []byte(status), time.Since(start), nil
}
