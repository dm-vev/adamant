package testfixture

import (
	"context"
	"log/slog"
	"sync"
)

// CaptureAddr returns a logger that passes what it logs on to next (a logger
// that discards if nil) and keeps the value of the "addr" attribute of the
// first record whose message is msg, and a function that returns it, "" if
// no such record was logged.
//
// A test that has a listener listen on a port of the system, "127.0.0.1:0",
// takes the address from the line the listener logs about it, rather than
// listening on a port to find a free one, closing it and having the listener
// listen on it: by then another test or another process may hold the port.
func CaptureAddr(next slog.Handler, msg string) (*slog.Logger, func() string) {
	if next == nil {
		next = slog.DiscardHandler
	}
	c := &addrCapture{Handler: next, msg: msg, shared: new(addrShared)}
	return slog.New(c), func() string {
		c.shared.mu.Lock()
		defer c.shared.mu.Unlock()
		return c.shared.addr
	}
}

// addrShared is what the handlers made from one another keep together.
type addrShared struct {
	mu   sync.Mutex
	addr string
}

// addrCapture is the slog.Handler of CaptureAddr.
type addrCapture struct {
	slog.Handler
	msg    string
	shared *addrShared
}

// Enabled reports every level as enabled, as the record of the address is
// of the info level and next may discard it.
func (c *addrCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *addrCapture) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == c.msg {
		r.Attrs(func(a slog.Attr) bool {
			if a.Key != "addr" {
				return true
			}
			c.shared.mu.Lock()
			if c.shared.addr == "" {
				c.shared.addr = a.Value.String()
			}
			c.shared.mu.Unlock()
			return false
		})
	}
	if c.Handler.Enabled(ctx, r.Level) {
		return c.Handler.Handle(ctx, r)
	}
	return nil
}

func (c *addrCapture) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &addrCapture{Handler: c.Handler.WithAttrs(attrs), msg: c.msg, shared: c.shared}
}

func (c *addrCapture) WithGroup(name string) slog.Handler {
	return &addrCapture{Handler: c.Handler.WithGroup(name), msg: c.msg, shared: c.shared}
}
