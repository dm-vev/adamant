package proxy

import (
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

type discardNetConn struct {
	block chan struct{}
}

func (c *discardNetConn) Read([]byte) (int, error) {
	<-c.block
	return 0, io.EOF
}
func (c *discardNetConn) Write(p []byte) (int, error)      { return len(p), nil }
func (c *discardNetConn) Close() error                     { return nil }
func (c *discardNetConn) LocalAddr() net.Addr              { return nil }
func (c *discardNetConn) RemoteAddr() net.Addr             { return nil }
func (c *discardNetConn) SetDeadline(time.Time) error      { return nil }
func (c *discardNetConn) SetReadDeadline(time.Time) error  { return nil }
func (c *discardNetConn) SetWriteDeadline(time.Time) error { return nil }

func BenchmarkPlayerSend(b *testing.B) {
	dc := &discardNetConn{block: make(chan struct{})}
	link := newLink(dc, secret, false)
	defer func() {
		close(dc.block)
		_ = link.Close()
	}()
	p, err := link.Open(Token{Identity: json.RawMessage(`"bench"`), Addr: "10.0.0.1:1000"})
	if err != nil {
		b.Fatal(err)
	}
	payload := make([]byte, 512)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for b.Loop() {
		if err := p.Send(payload); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPlayerDeliverAndRead(b *testing.B) {
	dc := &discardNetConn{block: make(chan struct{})}
	link := newLink(dc, secret, false)
	defer func() {
		close(dc.block)
		_ = link.Close()
	}()
	p := newPlayer(link, 1, Token{})
	payload := make([]byte, 256)
	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ResetTimer()
	for b.Loop() {
		p.deliver(payload)
		got, err := p.ReadMessage()
		if err != nil || len(got) != len(payload) {
			b.Fatalf("ReadMessage: len=%d err=%v", len(got), err)
		}
	}
}

func BenchmarkVerifyToken(b *testing.B) {
	now := time.Now()
	tok := Token{Identity: json.RawMessage(`{"name":"Steve"}`), Addr: "1.2.3.4:5", IssuedAt: now.Unix()}
	raw, err := signToken(tok, secret)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := verifyToken(raw, secret, now); err != nil {
			b.Fatal(err)
		}
	}
}
