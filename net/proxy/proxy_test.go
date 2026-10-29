package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"testing"
	"time"
)

var secret = []byte("a secret of the proxy and server")

func TestToken(t *testing.T) {
	tok := Token{Identity: json.RawMessage(`{"name":"Steve"}`), Addr: "1.2.3.4:5", IssuedAt: time.Now().Unix()}
	b, err := signToken(tok, secret)
	if err != nil {
		t.Fatal(err)
	}
	got, err := verifyToken(b, secret, time.Now())
	if err != nil || got.Addr != tok.Addr || string(got.Identity) != string(tok.Identity) {
		t.Fatalf("%+v, %v", got, err)
	}
	if _, err := verifyToken(b, []byte("another secret of sixteen"), time.Now()); !errors.Is(err, errToken) {
		t.Fatalf("a token of another secret verified: %v", err)
	}
	tampered := bytes.Replace(b, []byte("1.2.3.4"), []byte("6.6.6.6"), 1)
	if _, err := verifyToken(tampered, secret, time.Now()); !errors.Is(err, errToken) {
		t.Fatalf("a tampered token verified: %v", err)
	}
	if _, err := verifyToken(b, secret, time.Now().Add(2*time.Minute)); !errors.Is(err, errTokenExpired) {
		t.Fatalf("an old token verified: %v", err)
	}
}

// TestLink opens players on a link: the server reads who they are from
// the token, batches go both ways in order, and a close on either end
// reaches the other.
func TestLink(t *testing.T) {
	l, err := Listen("127.0.0.1:0", secret)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	link, err := Dial(l.Addr().String(), secret)
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	a, err := link.Open(Token{Identity: json.RawMessage(`"a"`), Addr: "10.0.0.1:1000", Protocol: 2193})
	if err != nil {
		t.Fatal(err)
	}
	b, err := link.Open(Token{Identity: json.RawMessage(`"b"`), Addr: "10.0.0.2:2000"})
	if err != nil {
		t.Fatal(err)
	}
	accepted := map[string]*Player{}
	for range 2 {
		p, err := l.Accept()
		if err != nil {
			t.Fatal(err)
		}
		accepted[string(p.Token().Identity)] = p
	}
	sa, sb := accepted[`"a"`], accepted[`"b"`]
	if sa == nil || sb == nil || sa.Token().Protocol != 2193 || sa.RemoteAddr().String() != "10.0.0.1:1000" {
		t.Fatalf("accepted %+v", accepted)
	}
	for i := range 3 {
		if err := a.Send([]byte{byte(i)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Send([]byte("b")); err != nil {
		t.Fatal(err)
	}
	_ = sa.SetReadDeadline(time.Now().Add(5 * time.Second))
	for i := range 3 {
		if msg, err := sa.ReadMessage(); err != nil || !bytes.Equal(msg, []byte{byte(i)}) {
			t.Fatalf("read %v, %v", msg, err)
		}
	}
	got := make(chan []byte, 1)
	sb.SetReceiver(func(msg []byte) { got <- msg })
	if msg := <-got; string(msg) != "b" {
		t.Fatalf("receiver got %q", msg)
	}
	if err := sa.Send([]byte("back")); err != nil {
		t.Fatal(err)
	}
	_ = a.SetReadDeadline(time.Now().Add(5 * time.Second))
	if msg, err := a.ReadMessage(); err != nil || string(msg) != "back" {
		t.Fatalf("proxy read %q, %v", msg, err)
	}
	closed := make(chan error, 1)
	sa.OnClose(func(err error) { closed <- err })
	_ = a.Close()
	select {
	case err := <-closed:
		var ce *ClosedError
		if !errors.As(err, &ce) {
			t.Fatalf("closed with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the close did not reach the server")
	}
	_ = sb.Close()
	_ = b.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := b.ReadMessage(); err == nil {
		t.Fatal("a player the server closed still reads")
	}
}

// TestLinkOfAnotherSecret ends a link whose proxy signs with another
// secret.
func TestLinkOfAnotherSecret(t *testing.T) {
	l, err := Listen("127.0.0.1:0", secret)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	link, err := Dial(l.Addr().String(), []byte("not the secret of the server"))
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	p, err := link.Open(Token{Addr: "1.1.1.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	_ = p.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := p.ReadMessage(); err == nil || errors.Is(err, errDeadline) {
		t.Fatalf("the link of another secret lives: %v", err)
	}
}

var errDeadline = errors.New("deadline")

func FuzzLinkFrames(f *testing.F) {
	tok, _ := signToken(Token{IssuedAt: time.Now().Unix()}, secret)
	frame := func(typ byte, id uint64, payload []byte) []byte {
		var b bytes.Buffer
		head := []byte{typ}
		head = appendUvarint(head, id)
		b.Write(appendUvarint(nil, uint64(len(head)+len(payload))))
		b.Write(head)
		b.Write(payload)
		return b.Bytes()
	}
	f.Add(frame(frameOpen, 1, tok))
	f.Add(append(frame(frameOpen, 1, tok), frame(frameBatch, 1, []byte("x"))...))
	f.Add(frame(frameClose, 9, []byte("why")))
	f.Fuzz(func(t *testing.T, b []byte) {
		server, client := net.Pipe()
		link := newLink(server, secret, true)
		go func() {
			_, _ = client.Write(b)
			_ = client.Close()
		}()
		<-link.done
	})
}

func appendUvarint(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}
