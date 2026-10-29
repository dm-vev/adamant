package interop

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/dm-vev/adamant/net/nethernet"
	"github.com/dm-vev/adamant/net/turn"
	pionturn "github.com/pion/turn/v5"
)

// memSignaling is one end of a signaling channel held in memory: what one
// end writes, the other reads, named by the network ID of the writer.
type memSignaling struct {
	id      string
	in, out chan nethernet.Signal
	rewrite func(nethernet.Signal) nethernet.Signal
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
		return nil
	case <-m.closed:
		return net.ErrClosed
	}
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

// TestListenerRelay has a client join a listener through the relay the
// listener allocated on the TURN server of pion, with the candidates of
// the listener's sockets taken out of the answer.
func TestListenerRelay(t *testing.T) {
	turnSock, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := pionturn.NewServer(pionturn.ServerConfig{
		Realm: "adamant",
		AuthHandler: func(ra *pionturn.RequestAttributes) (string, []byte, bool) {
			return ra.Username, pionturn.GenerateAuthKey("user", "adamant", "secret"), ra.Username == "user"
		},
		PacketConnConfigs: []pionturn.PacketConnConfig{{
			PacketConn:            turnSock,
			RelayAddressGenerator: &pionturn.RelayAddressGeneratorStatic{RelayAddress: net.ParseIP("127.0.0.1"), Address: "127.0.0.1"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	l, err := nethernet.ListenConfig{AllowAnonymous: true}.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := l.Relay(ctx, turn.Config{Server: addrPort(turnSock.LocalAddr()), Username: "user", Password: "secret"}); err != nil {
		t.Fatal(err)
	}
	srvSig, cliSig := memSignalingPair()
	defer close(srvSig.closed)
	srvSig.rewrite = onlyRelayCandidates
	go func() { _ = l.Serve(srvSig) }()

	// The client trickles the candidate of its socket, as libwebrtc does,
	// which the server permits on the relay; the dialer offers none.
	cliSig.rewrite = func(s nethernet.Signal) nethernet.Signal {
		if s.Type == nethernet.SignalOffer {
			go func() {
				_ = cliSig.WriteSignal(nethernet.Signal{Type: nethernet.SignalCandidate, ConnectionID: s.ConnectionID,
					Data: "candidate:1 1 udp 2122260223 127.0.0.1 9 typ host generation 0"})
			}()
		}
		return s
	}
	c, err := nethernet.Dialer{}.DialSignaling(ctx, cliSig, "server")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	accepted, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()
	srv := accepted.(*nethernet.Conn)
	if err := c.Send([]byte("through the relay")); err != nil {
		t.Fatal(err)
	}
	if msg, err := srv.ReadMessage(); err != nil || string(msg) != "through the relay" {
		t.Fatalf("server read %q, %v", msg, err)
	}
	if err := srv.Send([]byte("and back")); err != nil {
		t.Fatal(err)
	}
	if msg, err := c.ReadMessage(); err != nil || string(msg) != "and back" {
		t.Fatalf("client read %q, %v", msg, err)
	}
}
