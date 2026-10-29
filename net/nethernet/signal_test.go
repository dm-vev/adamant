package nethernet

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseSignal(t *testing.T) {
	sdp := "v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\ns=-\r\n"
	for _, s := range []Signal{
		{Type: SignalOffer, ConnectionID: 17, Data: sdp},
		{Type: SignalCandidate, ConnectionID: 1<<64 - 1, Data: "candidate:1 1 udp 2122260223 10.0.0.2 51234 typ host generation 0"},
		{Type: SignalError, ConnectionID: 0, Data: "13"},
	} {
		got, err := ParseSignal(s.String())
		if err != nil || got != s {
			t.Fatalf("round trip of %q: %+v, %v", s.String(), got, err)
		}
	}
	for _, text := range []string{
		"", "CONNECTREQUEST", "CONNECTREQUEST 1", "HELLO 1 x", "CONNECTREQUEST -1 x", "CONNECTREQUEST 1x x",
		"CONNECTREQUEST 18446744073709551616 x", "CONNECTREQUEST 1 " + strings.Repeat("a", maxDescription+64),
	} {
		if _, err := ParseSignal(text); err == nil {
			t.Fatalf("parsed %.40q", text)
		}
	}
}

// signalRouter carries the signals of a few networks among them, as a
// signaling service does.
type signalRouter struct {
	mu    sync.Mutex
	inbox map[string]chan Signal
}

func (r *signalRouter) join(network string) *routedSignaling {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inbox == nil {
		r.inbox = map[string]chan Signal{}
	}
	r.inbox[network] = make(chan Signal, 16)
	return &routedSignaling{r: r, network: network}
}

func (r *signalRouter) close(network string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	close(r.inbox[network])
}

type routedSignaling struct {
	r       *signalRouter
	network string
}

func (s *routedSignaling) ReadSignal() (Signal, error) {
	s.r.mu.Lock()
	in := s.r.inbox[s.network]
	s.r.mu.Unlock()
	sig, ok := <-in
	if !ok {
		return Signal{}, io.EOF
	}
	return sig, nil
}

func (s *routedSignaling) WriteSignal(sig Signal) error {
	s.r.mu.Lock()
	to, ok := s.r.inbox[sig.NetworkID]
	s.r.mu.Unlock()
	if !ok {
		return errors.New("no such network")
	}
	sig.NetworkID = s.network
	to <- sig
	return nil
}

// TestServeSignaling joins a listener over a signaling service: the
// listener answers the offer with its candidates and the connection opens,
// while an offer it cannot read is answered with an error code.
func TestServeSignaling(t *testing.T) {
	l := listen(t, ListenConfig{})
	var router signalRouter
	server, client := router.join("1234"), router.join("5678")
	served := make(chan error, 1)
	go func() { served <- l.Serve(server) }()

	d := newDialer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cli, err := d.DialSignaling(ctx, client, "1234")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cli.Close() })
	srv := accept(t, l)
	if !srv.PublicKey().Equal(&d.Key.PublicKey) {
		t.Fatal("server conn does not carry the key of the client identity")
	}
	if err := cli.Send([]byte("over signaling")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, srv); string(got) != "over signaling" {
		t.Fatalf("read %q", got)
	}

	// An offer the listener cannot read.
	if err := client.WriteSignal(Signal{Type: SignalOffer, ConnectionID: 9, Data: "not sdp", NetworkID: "1234"}); err != nil {
		t.Fatal(err)
	}
	reply, err := client.ReadSignal()
	if err != nil || reply.Type != SignalError || reply.ConnectionID != 9 || reply.Data != "13" || reply.NetworkID != "1234" {
		t.Fatalf("reply to a malformed offer: %+v, %v", reply, err)
	}

	router.close("1234")
	select {
	case err := <-served:
		if !errors.Is(err, io.EOF) {
			t.Fatalf("Serve returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return once the signaling ended")
	}
}

// FuzzParseSignal parses arbitrary text: what parses formats back to the
// same signal.
func FuzzParseSignal(f *testing.F) {
	f.Add("CONNECTREQUEST 17 v=0\r\n")
	f.Add("CANDIDATEADD 1 candidate:1 1 udp 1 10.0.0.2 1 typ host")
	f.Add("CONNECTERROR 99 13")
	f.Fuzz(func(t *testing.T, text string) {
		s, err := ParseSignal(text)
		if err != nil {
			return
		}
		again, err := ParseSignal(s.String())
		if err != nil || again != s {
			t.Fatalf("%q parsed as %+v, which formats to %q", text, s, s.String())
		}
	})
}
