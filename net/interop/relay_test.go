package interop

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/stun"
	"github.com/dm-vev/adamant/net/turn"
	pionturn "github.com/pion/turn/v5"
)

// memSignaling is one end of a signaling channel held in memory: what one
// end writes, the other reads, named by the network ID of the writer.
// Rewrite changes a signal before it is written; sent is told of one once
// it was.
type memSignaling struct {
	id      string
	in, out chan nethernet.Signal
	rewrite func(nethernet.Signal) nethernet.Signal
	sent    func(nethernet.Signal)
	closed  chan struct{}
}

func memSignalingPair() (server, client *memSignaling) {
	a, b := make(chan nethernet.Signal, 16), make(chan nethernet.Signal, 16)
	closed := make(chan struct{})
	return &memSignaling{id: "server", in: a, out: b, closed: closed}, &memSignaling{id: "client", in: b, out: a, closed: closed}
}

func (m *memSignaling) ReadSignal() (nethernet.Signal, error) {
	select {
	case s := <-m.in:
		return s, nil
	case <-m.closed:
		return nethernet.Signal{}, net.ErrClosed
	}
}

func (m *memSignaling) WriteSignal(s nethernet.Signal) error {
	if m.rewrite != nil {
		s = m.rewrite(s)
	}

	s.NetworkID = m.id
	select {
	case m.out <- s:
	case <-m.closed:
		return net.ErrClosed
	}
	if m.sent != nil {
		m.sent(s)
	}
	return nil
}

// onlyRelayCandidates drops every candidate of an answer but the relayed
// ones, so that a client can reach the server through its relay alone.
func onlyRelayCandidates(s nethernet.Signal) nethernet.Signal {
	if s.Type != nethernet.SignalAnswer {
		return s
	}
	var lines []string
	for _, line := range strings.Split(s.Data, "\r\n") {
		if strings.HasPrefix(line, "a=candidate:") && !strings.Contains(line, " typ relay ") {
			continue
		}
		lines = append(lines, line)
	}
	s.Data = strings.Join(lines, "\r\n")
	return s
}

// turnServer is a TURN server of pion on loopback that takes any user with
// the password "secret" and notes the peer addresses it is asked to permit.
type turnServer struct {
	net.PacketConn
	addr netip.AddrPort

	mu        sync.Mutex
	permitted map[netip.Addr]bool
}

func newTURNServer(t *testing.T) *turnServer {
	t.Helper()
	sock, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &turnServer{PacketConn: sock, addr: addrPort(sock.LocalAddr()), permitted: make(map[netip.Addr]bool)}
	server, err := pionturn.NewServer(pionturn.ServerConfig{
		Realm: "adamant",
		AuthHandler: func(ra *pionturn.RequestAttributes) (string, []byte, bool) {
			return ra.Username, pionturn.GenerateAuthKey(ra.Username, "adamant", "secret"), true
		},
		PacketConnConfigs: []pionturn.PacketConnConfig{{
			PacketConn:            s,
			RelayAddressGenerator: &pionturn.RelayAddressGeneratorStatic{RelayAddress: net.ParseIP("127.0.0.1"), Address: "127.0.0.1"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return s
}

// ReadFrom reads a datagram for the server, noting the peer of a
// CreatePermission request.
func (s *turnServer) ReadFrom(b []byte) (int, net.Addr, error) {
	n, from, err := s.PacketConn.ReadFrom(b)
	var m stun.Message
	if err == nil && stun.Parse(b[:n], &m) == nil && m.Type == 0x0008 {
		if v, ok := m.Get(0x0012); ok && len(v) == 8 && v[1] == 1 {
			var ip [4]byte
			binary.BigEndian.PutUint32(ip[:], binary.BigEndian.Uint32(v[4:])^0x2112A442)
			s.mu.Lock()
			s.permitted[netip.AddrFrom4(ip)] = true
			s.mu.Unlock()
		}
	}
	return n, from, err
}

// asked returns the peer addresses the server was asked to permit.
func (s *turnServer) asked() []netip.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []netip.Addr
	for a := range s.permitted {
		out = append(out, a)
	}
	return out
}

// config returns the config of a relay on the server for a user.
func (s *turnServer) config(user string) turn.Config {
	return turn.Config{Server: s.addr, Username: user, Password: "secret"}
}

// relayListener returns a listener with a relay on a TURN server, serving
// the signaling of the server end returned with the candidates of its
// sockets taken out of the answers, so that a client on the client end
// reaches it through the relay alone.
func relayListener(t *testing.T, ctx context.Context, srv *turnServer) (l *nethernet.Listener, server, client *memSignaling) {
	t.Helper()
	l, err := nethernet.ListenConfig{AllowAnonymous: true}.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err = l.Relay(ctx, srv.config("user")); err != nil {
		t.Fatal(err)
	}
	server, client = memSignalingPair()
	t.Cleanup(func() { close(server.closed) })
	server.rewrite = onlyRelayCandidates
	go func() { _ = l.Serve(server) }()
	return l, server, client
}

// dialRelayed has a client join a listener over sig, trickling the
// candidate of its socket after its offer, as libwebrtc does, which the
// server permits on the relay; the dialer offers none. It returns both
// ends of the connection.
func dialRelayed(t *testing.T, ctx context.Context, l *nethernet.Listener, sig *memSignaling) (client, server *nethernet.Conn) {
	t.Helper()
	sig.sent = func(s nethernet.Signal) {
		if s.Type == nethernet.SignalOffer {
			_ = sig.WriteSignal(nethernet.Signal{Type: nethernet.SignalCandidate, ConnectionID: s.ConnectionID,
				Data: "candidate:1 1 udp 2122260223 127.0.0.1 9 typ host generation 0"})
		}
	}
	c, err := nethernet.Dialer{}.DialSignaling(ctx, sig, "server")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	accepted, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accepted.Close() })
	return c, accepted.(*nethernet.Conn)
}

// exchangeRelayed sends a message each way between the ends of a
// connection.
func exchangeRelayed(t *testing.T, client, server *nethernet.Conn) {
	t.Helper()
	_ = client.SetReadDeadline(time.Now().Add(10 * time.Second))
	_ = server.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := client.Send([]byte("through the relay")); err != nil {
		t.Fatal(err)
	}
	if msg, err := server.ReadMessage(); err != nil || string(msg) != "through the relay" {
		t.Fatalf("server read %q, %v", msg, err)
	}
	if err := server.Send([]byte("and back")); err != nil {
		t.Fatal(err)
	}
	if msg, err := client.ReadMessage(); err != nil || string(msg) != "and back" {
		t.Fatalf("client read %q, %v", msg, err)
	}
}

// TestListenerRelay has a client join a listener through the relay the
// listener allocated on the TURN server of pion, with the candidates of
// the listener's sockets taken out of the answer.
func TestListenerRelay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, _, sig := relayListener(t, ctx, newTURNServer(t))
	client, server := dialRelayed(t, ctx, l, sig)
	exchangeRelayed(t, client, server)
}

// TestRelayRoutesProvenPeers has a client join through the relay, then a
// socket at the address the relay permits for the client send junk to the
// relay: a datagram of no protocol and a check with the ufrag of the
// server's end of the connection not signed with its password. What goes
// to the socket goes to it directly, not through the relay, while what
// goes to the client goes through it.
func TestRelayRoutesProvenPeers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, srvSig, sig := relayListener(t, ctx, newTURNServer(t))
	var ufrag string
	srvSig.rewrite = func(s nethernet.Signal) nethernet.Signal {
		for line := range strings.Lines(s.Data) {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "a=ice-ufrag:"); ok {
				ufrag = v
			}
		}
		return onlyRelayCandidates(s)
	}
	client, server := dialRelayed(t, ctx, l, sig)
	relayed := addrPort(client.RemoteAddr())

	junk := listenLoopback(t)
	check := stun.NewBuilder(nil, stun.BindingRequest, [12]byte{1, 2, 3})
	check.AddString(stun.AttrUsername, ufrag+":abcd")
	check.AddUint64(stun.AttrICEControlling, 1)
	check.AddIntegrity(hmac.New(sha1.New, []byte("not the password of the connection")))
	check.AddFingerprint()
	for _, b := range [][]byte{{23, 0xfe, 0xfd, 0, 1, 2, 3}, check.Bytes()} {
		if _, err := junk.WriteToUDPAddrPort(b, relayed); err != nil {
			t.Fatal(err)
		}
	}
	// The relay hands over what the client sends after the junk.
	exchangeRelayed(t, client, server)
	if l.Relayed(addrPort(junk.LocalAddr())) {
		t.Fatal("junk from a permitted address made a route through the relay")
	}
	if !l.Relayed(netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), addrPort(client.LocalAddr()).Port())) {
		t.Fatal("no route through the relay to the client")
	}
}

// strangerOffer returns an anonymous offer of a client that never
// connects, with a candidate of each address.
func strangerOffer(addrs []netip.Addr) string {
	var b strings.Builder
	b.WriteString("v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\nm=application 9 UDP/DTLS/SCTP webrtc-datachannel\r\nc=IN IP4 0.0.0.0\r\n")
	for _, a := range addrs {
		fmt.Fprintf(&b, "a=candidate:1 1 udp 2122260223 %s 9 typ host generation 0\r\n", a)
	}
	b.WriteString("a=ice-ufrag:abcd\r\na=ice-pwd:abcdefghijklmnopqrstuvwx\r\na=fingerprint:sha-256 00:11\r\na=setup:actpass\r\n")
	return b.String()
}

// TestRelayPermissionsOfConnections has a stranger trickle 300 candidates
// for no connection of its own and send offers of 32 candidates until one
// is refused, then a client join through the relay: the TURN server is
// asked to permit only the addresses of connections the listener took, 8
// of each at most.
func TestRelayPermissionsOfConnections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	srv := newTURNServer(t)
	l, _, sig := relayListener(t, ctx, srv)
	stranger := &memSignaling{id: "stranger", in: make(chan nethernet.Signal, 16), out: sig.out, closed: sig.closed}
	for i := range 300 {
		_ = stranger.WriteSignal(nethernet.Signal{Type: nethernet.SignalCandidate, ConnectionID: uint64(i),
			Data: fmt.Sprintf("candidate:1 1 udp 2122260223 10.1.%d.%d 9 typ host generation 0", i>>8, i&255)})
	}
	// The listener takes 4 connections opening of one sender.
	for o := range 5 {
		addrs := make([]netip.Addr, 32)
		for i := range addrs {
			addrs[i] = netip.AddrFrom4([4]byte{10, 2, byte(o), byte(i)})
		}
		_ = stranger.WriteSignal(nethernet.Signal{Type: nethernet.SignalOffer, ConnectionID: uint64(1000 + o), Data: strangerOffer(addrs)})
	}
	client, server := dialRelayed(t, ctx, l, sig)
	exchangeRelayed(t, client, server)

	// The permission of the client was asked for after those of the
	// stranger, and the client came through: the server read them all.
	var trickled, ofClient int
	byOffer := make([]int, 5)
	for _, a := range srv.asked() {
		b := a.As4()
		switch {
		case a == netip.MustParseAddr("127.0.0.1"):
			ofClient++
		case b[1] == 1:
			trickled++
		case b[1] == 2:
			byOffer[b[2]]++
		}
	}
	if trickled != 0 || ofClient != 1 || byOffer[4] != 0 || byOffer[0] != 8 || byOffer[1] != 8 || byOffer[2] != 8 || byOffer[3] != 8 {
		t.Fatalf("permissions asked for %d candidates trickled for no connection, %d of the client, %v of the addresses of each offer; want 0, 1, 8 of each taken and none of the refused", trickled, ofClient, byOffer)
	}
}
