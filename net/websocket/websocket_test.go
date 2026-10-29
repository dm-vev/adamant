package websocket

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// echoServer answers every message with itself and closes as asked.
func echoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "MCToken test" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c, err := Upgrade(w, r)
		if err != nil {
			return
		}
		defer c.Close()
		for {
			text, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if text && string(data) == "close" {
				_ = c.closeWith(CloseGoingAway, "bye")
				return
			}
			if err := c.WriteMessage(text, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, srv *httptest.Server) *Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), http.Header{"Authorization": {"MCToken test"}}, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestEcho(t *testing.T) {
	c := dial(t, echoServer(t))
	big := bytes.Repeat([]byte{0xab}, 70000)
	for _, m := range []struct {
		text bool
		data []byte
	}{{true, []byte(`{"Type":0}`)}, {false, []byte{1, 2, 3}}, {false, big}, {true, nil}} {
		if err := c.WriteMessage(m.text, m.data); err != nil {
			t.Fatal(err)
		}
		text, data, err := c.ReadMessage()
		if err != nil || text != m.text || !bytes.Equal(data, m.data) {
			t.Fatalf("echo of %d bytes: %v %d bytes, %v", len(m.data), text, len(data), err)
		}
	}
	if err := c.WriteMessage(true, []byte("close")); err != nil {
		t.Fatal(err)
	}
	var ce *CloseError
	if _, _, err := c.ReadMessage(); !errors.As(err, &ce) || ce.Code != CloseGoingAway || ce.Reason != "bye" {
		t.Fatalf("close: %v", err)
	}
	if err := c.WriteMessage(true, []byte("late")); !errors.Is(err, ErrClosed) {
		t.Fatalf("write after close: %v", err)
	}
}

func TestDialRefused(t *testing.T) {
	srv := echoServer(t)
	_, resp, err := Dial(context.Background(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil, nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dial without token: %v, %+v", err, resp)
	}
}

// pipeConn returns a client Conn reading what is written to the returned
// connection, and that connection.
func pipeConn(t *testing.T, client bool) (*Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	go func() {
		// Drain what the Conn writes: pongs and close frames.
		buf := make([]byte, 1024)
		for {
			if _, err := b.Read(buf); err != nil {
				return
			}
		}
	}()
	return &Conn{conn: a, br: bufio.NewReader(a), client: client, readLimit: 1024}, b
}

// frame encodes a frame as a server sends it: unmasked.
func frame(first byte, payload []byte) []byte {
	out := []byte{first}
	switch n := len(payload); {
	case n <= 125:
		out = append(out, byte(n))
	case n <= 0xffff:
		out = append(out, 126)
		out = binary.BigEndian.AppendUint16(out, uint16(n))
	default:
		out = append(out, 127)
		out = binary.BigEndian.AppendUint64(out, uint64(n))
	}
	return append(out, payload...)
}

func TestFragmentsAndPings(t *testing.T) {
	c, peer := pipeConn(t, true)
	go func() {
		w := bufio.NewWriter(peer)
		_, _ = w.Write(frame(opText, []byte("hel")))
		_, _ = w.Write(frame(0x80|opPing, []byte("p")))
		_, _ = w.Write(frame(opContinuation, []byte("lo ")))
		_, _ = w.Write(frame(0x80|opContinuation, []byte("world")))
		_ = w.Flush()
	}()
	text, data, err := c.ReadMessage()
	if err != nil || !text || string(data) != "hello world" {
		t.Fatalf("fragmented message: %v %q %v", text, data, err)
	}
}

// TestProtocolErrors feeds frames a peer must not send: each fails the
// connection rather than being read.
func TestProtocolErrors(t *testing.T) {
	long := bytes.Repeat([]byte{'a'}, 1025)
	for _, test := range []struct {
		name   string
		client bool
		data   []byte
		want   error
	}{
		{"reserved bits", true, frame(0xc0|opText, []byte("x")), errProtocol},
		{"masked server frame", true, append([]byte{0x80 | opText, 0x81, 1, 2, 3, 4}, 'x'), errProtocol},
		{"unmasked client frame", false, frame(0x80|opText, []byte("x")), errProtocol},
		{"long ping", true, frame(0x80|opPing, bytes.Repeat([]byte{1}, 126)), errProtocol},
		{"fragmented ping", true, frame(opPing, []byte("x")), errProtocol},
		{"continuation first", true, frame(0x80|opContinuation, []byte("x")), errProtocol},
		{"unknown opcode", true, frame(0x80|0x3, []byte("x")), errProtocol},
		{"message over the limit", true, frame(0x80|opBinary, long), ErrTooLarge},
		{"fragments over the limit", true, append(frame(opBinary, long[:1000]), frame(0x80|opContinuation, long[:100])...), ErrTooLarge},
		{"text not UTF-8", true, frame(0x80|opText, []byte{0xff, 0xfe}), errProtocol},
		{"close code 1005", true, frame(0x80|opClose, []byte{0x03, 0xed}), errProtocol},
		{"one byte close", true, frame(0x80|opClose, []byte{1}), errProtocol},
		{"length not minimal", true, []byte{0x80 | opBinary, 126, 0, 5, 1, 2, 3, 4, 5}, errProtocol},
		{"length past 63 bits", true, []byte{0x80 | opBinary, 127, 0x80, 0, 0, 0, 0, 0, 0, 0}, errProtocol},
		{"message inside a message", true, append(frame(opText, []byte("a")), frame(0x80|opText, []byte("b"))...), errProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, peer := pipeConn(t, test.client)
			go func() { _, _ = peer.Write(test.data) }()
			_, _, err := c.ReadMessage()
			if !errors.Is(err, test.want) {
				t.Fatalf("got %v, want %v", err, test.want)
			}
		})
	}
}

// FuzzReadMessage reads whatever a peer sends: a connection fails or
// returns messages within the limit, and never panics.
func FuzzReadMessage(f *testing.F) {
	f.Add(frame(0x80|opText, []byte("hello")), true)
	f.Add(append(frame(opText, []byte("a")), frame(0x80|opContinuation, []byte("b"))...), true)
	f.Add(frame(0x80|opClose, []byte{0x03, 0xe8, 'x'}), true)
	f.Add([]byte{0x80 | opBinary, 0x82, 1, 2, 3, 4, 5, 6}, false)
	f.Fuzz(func(t *testing.T, data []byte, client bool) {
		a, b := net.Pipe()
		defer a.Close()
		go func() {
			_, _ = b.Write(data)
			_ = b.Close()
		}()
		go func() {
			buf := make([]byte, 512)
			for {
				if _, err := b.Read(buf); err != nil {
					return
				}
			}
		}()
		c := &Conn{conn: a, br: bufio.NewReader(a), client: client, readLimit: 4096}
		for range 64 {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if len(msg) > 4096 {
				t.Fatalf("message of %d bytes past the limit", len(msg))
			}
		}
	})
}
