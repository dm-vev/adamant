package lan

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
)

// Config configures a Listener.
type Config struct {
	// NetworkID names the server on the network. A random one is used if
	// zero.
	NetworkID uint64
	// Data returns what the LAN tab shows of the server, asked for each
	// request. A client, which answers no requests, leaves it nil.
	Data func() ServerData
	// Addr is the address to listen on; ":7551" if empty.
	Addr string
}

// Listener answers the requests of clients on the network with the data
// of a server and is the signaling of joins over the network: a
// nethernet.Listener serves it as it serves the signaling of Xbox Live. A
// client finds servers with one too and dials them over it.
type Listener struct {
	conn *net.UDPConn
	id   uint64
	data func() ServerData

	mu      sync.Mutex
	peers   map[uint64]peer
	servers map[uint64]ServerData

	signals chan nethernet.Signal
	closed  chan struct{}
	once    sync.Once
}

// peer is where a network ID sent from last.
type peer struct {
	addr netip.AddrPort
	seen time.Time
}

// maxPeers bounds the network IDs a listener keeps the address of, and
// peerTTL how long it keeps one that sends nothing.
const (
	maxPeers = 4096
	peerTTL  = time.Minute
)

// Listen opens the UDP socket and starts answering.
func (cfg Config) Listen() (*Listener, error) {
	if cfg.Addr == "" {
		cfg.Addr = ":" + strconv.Itoa(Port)
	}
	if cfg.NetworkID == 0 {
		var b [8]byte
		_, _ = rand.Read(b[:])
		cfg.NetworkID = binary.LittleEndian.Uint64(b[:]) | 1
	}
	addr, err := net.ResolveUDPAddr("udp", cfg.Addr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	l := &Listener{
		conn:    conn,
		id:      cfg.NetworkID,
		data:    cfg.Data,
		peers:   make(map[uint64]peer),
		servers: make(map[uint64]ServerData),
		signals: make(chan nethernet.Signal, 64),
		closed:  make(chan struct{}),
	}
	go l.read()
	return l, nil
}

// NetworkID returns the network ID of the server, which a client puts in
// the messages it sends it.
func (l *Listener) NetworkID() uint64 { return l.id }

// Addr returns the address of the socket.
func (l *Listener) Addr() net.Addr { return l.conn.LocalAddr() }

func (l *Listener) read() {
	buf := make([]byte, maxPacket)
	for {
		n, from, err := l.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				_ = l.Close()
				return
			}
			continue
		}
		l.handle(buf[:n], netip.AddrPortFrom(from.Addr().Unmap(), from.Port()))
	}
}

// handle handles a datagram from an address. Anything that is not a packet
// of the game is dropped.
func (l *Listener) handle(b []byte, from netip.AddrPort) {
	pk, sender, err := Unmarshal(b)
	if err != nil || sender == l.id {
		return
	}
	l.remember(sender, from)
	switch pk := pk.(type) {
	case *Request:
		if l.data == nil {
			return
		}
		data, err := l.data().MarshalBinary()
		if err == nil {
			_, _ = l.conn.WriteToUDPAddrPort(Marshal(&Response{ApplicationData: data}, l.id), from)
		}
	case *Response:
		var d ServerData
		if d.UnmarshalBinary(pk.ApplicationData) == nil {
			l.mu.Lock()
			if _, ok := l.servers[sender]; ok || len(l.servers) < maxPeers {
				l.servers[sender] = d
			}
			l.mu.Unlock()
		}
	case *Message:
		if pk.RecipientID != l.id || pk.Data == "" || pk.Data == "Ping" {
			return
		}
		s, err := nethernet.ParseSignal(pk.Data)
		if err != nil {
			return
		}
		s.NetworkID = strconv.FormatUint(sender, 10)
		select {
		case l.signals <- s:
		case <-l.closed:
		default:
			// A reader far behind loses signals, as lost datagrams do.
		}
	}
}

// remember keeps the address a network ID sent from.
func (l *Listener) remember(id uint64, from netip.AddrPort) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.peers[id]; !ok && len(l.peers) >= maxPeers {
		for k, p := range l.peers {
			if now.Sub(p.seen) > peerTTL {
				delete(l.peers, k)
			}
		}
		if len(l.peers) >= maxPeers {
			return
		}
	}
	l.peers[id] = peer{addr: from, seen: now}
}

// Discover asks the servers at addr for their data, the broadcast address
// of the network on Port to find all. Servers lists those that answered.
func (l *Listener) Discover(addr netip.AddrPort) error {
	_, err := l.conn.WriteToUDPAddrPort(Marshal(&Request{}, l.id), addr)
	return err
}

// Servers returns the data of the servers that answered Discover, by their
// network IDs.
func (l *Listener) Servers() map[uint64]ServerData {
	l.mu.Lock()
	defer l.mu.Unlock()
	servers := make(map[uint64]ServerData, len(l.servers))
	for id, d := range l.servers {
		servers[id] = d
	}
	return servers
}

// ReadSignal returns the next signal a peer sent.
func (l *Listener) ReadSignal() (nethernet.Signal, error) {
	select {
	case s := <-l.signals:
		return s, nil
	case <-l.closed:
		return nethernet.Signal{}, net.ErrClosed
	}
}

// WriteSignal sends a signal to the peer of its network ID, at the address
// it last sent from.
func (l *Listener) WriteSignal(s nethernet.Signal) error {
	id, err := strconv.ParseUint(s.NetworkID, 10, 64)
	if err != nil {
		return errors.New("lan: network ID is not a number")
	}
	l.mu.Lock()
	p, ok := l.peers[id]
	l.mu.Unlock()
	if !ok {
		return errors.New("lan: no address for network ID " + s.NetworkID)
	}
	_, err = l.conn.WriteToUDPAddrPort(Marshal(&Message{RecipientID: id, Data: s.String()}, l.id), p.addr)
	return err
}

// Close stops the listener.
func (l *Listener) Close() error {
	var err error
	l.once.Do(func() {
		close(l.closed)
		err = l.conn.Close()
	})
	return err
}
