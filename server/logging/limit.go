package logging

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// summaryDelay is how long after the first line it dropped a limiter of a
// component that writes nothing more logs how many it dropped.
const summaryDelay = 10 * time.Second

// limiter is a bucket of the lines of a component: rate a second, with a
// burst of ten times as many. A line past it is dropped and counted. The
// limiter logs how many it dropped since when summaryDelay after the first
// it dropped, or before the first line it lets through after that, so that
// a flood is summed up once every summaryDelay at most.
type limiter struct {
	set  *Set
	comp string
	rate float64

	mu      sync.Mutex
	tokens  float64
	last    time.Time
	dropped uint64
	since   time.Time
	timer   bool // whether a summary is scheduled
}

// newLimiter returns the limiter of a component, or nil if rate is 0.
func (s *Set) newLimiter(component string, rate int) *limiter {
	if rate <= 0 {
		return nil
	}
	return &limiter{set: s, comp: component, rate: float64(rate), tokens: float64(rate) * 10, last: s.now()}
}

// allow reports whether a line may be written now. Before a line it lets
// through, it logs the lines dropped since the last it let through.
func (l *limiter) allow(ctx context.Context) bool {
	now := l.set.now()
	l.mu.Lock()
	if elapsed := now.Sub(l.last); elapsed > 0 {
		l.tokens = min(l.rate*10, l.tokens+elapsed.Seconds()*l.rate)
		l.last = now
	}
	if l.tokens < 1 {
		l.dropped++
		schedule := !l.timer
		if l.dropped == 1 {
			l.since = now
		}
		l.timer = true
		l.mu.Unlock()
		if schedule {
			l.set.afterFunc(summaryDelay, func() { l.flush(context.Background()) })
		}
		if l.set.dropped != nil {
			l.set.dropped(l.comp, 1)
		}
		return false
	}
	l.tokens--
	var n uint64
	since := l.since
	if l.dropped > 0 && now.Sub(l.since) >= summaryDelay {
		n, l.dropped = l.dropped, 0
	}
	l.mu.Unlock()
	if n > 0 {
		l.set.summarise(ctx, l.comp, n, since)
	}
	return true
}

// flush logs the lines dropped that no line let through since logged.
func (l *limiter) flush(ctx context.Context) {
	l.mu.Lock()
	n, since := l.dropped, l.since
	l.dropped, l.timer = 0, false
	l.mu.Unlock()
	if n > 0 {
		l.set.summarise(ctx, l.comp, n, since)
	}
}

// summarise logs that n lines of a component were dropped since the time
// passed, past the limiter of the component.
func (s *Set) summarise(ctx context.Context, component string, n uint64, since time.Time) {
	r := slog.NewRecord(s.now(), slog.LevelWarn, "Log lines dropped over the rate limit.", 0)
	r.AddAttrs(slog.String("component", component), slog.Uint64("dropped", n), slog.Time("since", since.Round(time.Millisecond)))
	if s.format == JSON {
		// The field component of the line is the attribute.
		_ = s.json.Handle(ctx, r)
		return
	}
	_ = s.text.write(ctx, s.logger, s.text.h, r)
}
