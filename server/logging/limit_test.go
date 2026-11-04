package logging

import (
	"bytes"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock tests move by hand, with the functions AfterFunc
// scheduled on it.
type fakeClock struct {
	mu    sync.Mutex
	now   time.Time
	after []func()
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *fakeClock) AfterFunc(_ time.Duration, f func()) {
	c.mu.Lock()
	c.after = append(c.after, f)
	c.mu.Unlock()
}

// fire runs the functions scheduled.
func (c *fakeClock) fire() {
	c.mu.Lock()
	after := c.after
	c.after = nil
	c.mu.Unlock()
	for _, f := range after {
		f()
	}
}

// TestRateLimitDropsAndSummarises checks the limiter of the lines of a
// component: a burst of ten times the rate, then the rate, the lines past
// it dropped, counted and summed up before the first line let through once
// the delay of a summary passed, or by the summary scheduled if none comes.
func TestRateLimitDropsAndSummarises(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	var out bytes.Buffer
	dropped := map[string]uint64{}
	s := New(Options{RateLimit: 100, Logger: log.New(&out, "", 0), Dropped: func(c string, n uint64) { dropped[c] += n }})
	s.now, s.afterFunc = clock.Now, clock.AfterFunc
	l := s.Logger(World)
	other := s.Logger(Session)
	// 5000 lines over a second: the burst of 1000, then 100 more.
	for range 5000 {
		l.Info("flood")
		clock.advance(200 * time.Microsecond)
	}
	other.Info("quiet")
	if n := strings.Count(out.String(), "INFO flood\n"); n < 1099 || n > 1101 {
		t.Errorf("%d lines written, want 1100", n)
	}
	if !strings.Contains(out.String(), "INFO quiet\n") {
		t.Error("a line of another component was dropped")
	}
	if dropped[World] < 3899 || dropped[World] > 3901 || dropped[Session] != 0 {
		t.Errorf("dropped %v", dropped)
	}
	if strings.Contains(out.String(), "dropped") {
		t.Fatal("a summary within the delay of the first line dropped")
	}
	clock.advance(summaryDelay)
	l.Info("after")
	got := out.String()
	if !strings.Contains(got, "WARN Log lines dropped over the rate limit. component=world dropped=") || !strings.HasSuffix(got, "INFO after\n") {
		t.Errorf("summary or line after it: %s", got[max(0, len(got)-300):])
	}

	// A component that drops lines and then says nothing has its summary
	// written by the function scheduled.
	out.Reset()
	for range 2000 {
		other.Info("flood")
	}
	if strings.Contains(out.String(), "dropped") {
		t.Fatal("a summary before the flood ended")
	}
	clock.fire()
	if !strings.Contains(out.String(), "component=session dropped=1000 ") {
		t.Errorf("scheduled summary: %q", out.String()[max(0, out.Len()-200):])
	}
	out.Reset()
	clock.fire()
	clock.advance(time.Second)
	other.Info("next")
	if out.String() != "INFO next\n" {
		t.Errorf("after the summary: %q", out.String())
	}
}

// TestRateLimitOff checks that a RateLimit of 0 drops nothing.
func TestRateLimitOff(t *testing.T) {
	var out bytes.Buffer
	l := New(Options{Logger: log.New(&out, "", 0)}).Logger(World)
	for range 3000 {
		l.Info("x")
	}
	if n := strings.Count(out.String(), "\n"); n != 3000 {
		t.Errorf("%d lines, want 3000", n)
	}
}
