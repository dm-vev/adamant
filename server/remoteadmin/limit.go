package remoteadmin

import (
	"net/netip"
	"sync"
	"time"
)

// maxAddresses is how many addresses the limiter of failures keeps at most;
// the least recently seen goes first.
const maxAddresses = 4096

// globalFailures is how many failures a minute, from all addresses, block
// every address that is not loopback for a minute.
const globalFailures = 60

// blockFor is how long an address, or all, is blocked once past its limit.
const blockFor = time.Minute

// limiter counts the failed sign-ins of each address in a bucket of
// MaxFailures tokens a minute: past it, the address is refused for a
// minute without its token checked, so that a guesser learns nothing.
type limiter struct {
	mu          sync.Mutex
	perMinute   int
	addrs       map[netip.Addr]*addrState
	windowStart time.Time
	windowCount int
	globalUntil time.Time
}

// addrState is what the limiter keeps of an address.
type addrState struct {
	tokens       float64
	last         time.Time
	blockedUntil time.Time
	// unreported failures, recorded at most once a minute.
	unreported uint64
	reported   time.Time
}

func newLimiter(perMinute int) *limiter {
	return &limiter{perMinute: perMinute, addrs: map[netip.Addr]*addrState{}}
}

// blocked reports whether a request from a is refused now, and until when.
func (l *limiter) blocked(a netip.Addr, now time.Time) (time.Time, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !a.IsLoopback() && now.Before(l.globalUntil) {
		return l.globalUntil, true
	}
	if s, ok := l.addrs[a]; ok && now.Before(s.blockedUntil) {
		return s.blockedUntil, true
	}
	return time.Time{}, false
}

// fail counts a failure of a at now. It returns how many failures of a to
// record now, 0 if they wait for the summary of the minute.
func (l *limiter) fail(a netip.Addr, now time.Time) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.windowStart) >= time.Minute {
		l.windowStart, l.windowCount = now, 0
	}
	l.windowCount++
	if l.windowCount >= globalFailures {
		l.globalUntil = now.Add(blockFor)
	}
	s, ok := l.addrs[a]
	if !ok {
		l.evict()
		s = &addrState{tokens: float64(l.perMinute), last: now}
		l.addrs[a] = s
	}
	s.tokens = min(float64(l.perMinute), s.tokens+now.Sub(s.last).Minutes()*float64(l.perMinute))
	s.last = now
	s.tokens--
	if s.tokens < 1 {
		s.blockedUntil = now.Add(blockFor)
	}
	s.unreported++
	if now.Sub(s.reported) < time.Minute {
		return 0
	}
	n := s.unreported
	s.unreported, s.reported = 0, now
	return n
}

// evict drops the address seen least recently, if the limiter is full.
func (l *limiter) evict() {
	if len(l.addrs) < maxAddresses {
		return
	}
	var oldest netip.Addr
	var at time.Time
	for a, s := range l.addrs {
		if at.IsZero() || s.last.Before(at) {
			oldest, at = a, s.last
		}
	}
	delete(l.addrs, oldest)
}
