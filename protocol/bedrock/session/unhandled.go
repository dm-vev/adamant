package session

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// unhandledInterval is how often a session logs a packet it has no handler
// for: a client that sends many floods the log with no more than one a
// second, and the next line counts those left out.
const unhandledInterval = time.Second

// unhandledLog paces the lines a session logs of the packets it has no
// handler for.
type unhandledLog struct {
	mu      sync.Mutex
	last    time.Time
	skipped int
}

// logUnhandled logs, at debug level, the type and the size of a packet the
// session has no handler for, at most once every unhandledInterval. Nothing
// of the packet is looked at unless the logger logs debug.
func (s *Session) logUnhandled(pk proto.Packet) {
	log := s.conf.Log
	if !log.Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	u := &s.unhandled
	now := time.Now()
	u.mu.Lock()
	if !u.last.IsZero() && now.Sub(u.last) < unhandledInterval {
		u.skipped++
		u.mu.Unlock()
		return
	}
	skipped := u.skipped
	u.last, u.skipped = now, 0
	u.mu.Unlock()
	log.Debug("unhandled packet", "packet", fmt.Sprintf("%T", pk), "id", pk.ID(), "size", packetSize(pk), "skipped", skipped)
}

// packetSize returns the size of the encoded fields of a packet, -1 if it
// does not encode.
func packetSize(pk proto.Packet) (n int) {
	defer func() {
		if recover() != nil {
			n = -1
		}
	}()
	var w wire.Writer
	pk.Encode(&w)
	return len(w.B)
}
