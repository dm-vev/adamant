package anticheat

import "time"

// TicksPerSecond is the rate a player ticks and a client simulates at.
const TicksPerSecond = 20

// Level is the violation level of a check: it grows by a violation and
// decays by the Decay of its Policy each second, lazily, as it is read. The
// zero Level is 0.
type Level struct {
	value float64
	tick  int64
}

// At returns the level at the tick passed, decayed by decay a second.
func (l *Level) At(tick int64, decay float64) float64 {
	if tick <= l.tick {
		return l.value
	}
	return max(0, l.value-decay*float64(tick-l.tick)/TicksPerSecond)
}

// Add decays the level to the tick passed, adds amount and returns the new
// level.
func (l *Level) Add(tick int64, decay, amount float64) float64 {
	l.value = l.At(tick, decay) + amount
	if tick > l.tick {
		l.tick = tick
	}
	return l.value
}

// Reset sets the level to 0.
func (l *Level) Reset() { *l = Level{} }

// Bucket is a token bucket: it holds up to a burst of tokens and gains them
// at a rate a second. The zero Bucket is full on first use.
type Bucket struct {
	tokens float64
	at     time.Time
	used   bool
}

// Take takes a token at now and reports whether one was there. A Bucket
// that lacks one is left as it was.
func (b *Bucket) Take(now time.Time, perSecond float64, burst int) bool {
	b.fill(now, perSecond, burst)
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// Tokens returns the tokens in the bucket at now.
func (b *Bucket) Tokens(now time.Time, perSecond float64, burst int) float64 {
	b.fill(now, perSecond, burst)
	return b.tokens
}

func (b *Bucket) fill(now time.Time, perSecond float64, burst int) {
	if !b.used {
		b.tokens, b.at, b.used = float64(burst), now, true
		return
	}
	if now.After(b.at) {
		b.tokens = min(float64(burst), b.tokens+now.Sub(b.at).Seconds()*perSecond)
		b.at = now
	}
}

// tickDuration is the time a tick takes at 20 ticks a second.
const tickDuration = time.Second / TicksPerSecond

// Clock is the balance of the timer of a client: how far its moves run
// ahead of the time that went by. Time is counted in ticks from a
// reference: the most of the wall clock at the arrival of the move, at 20
// ticks a second, and of the ticks of the player since. The wall clock
// keeps a lagging server from bunching the moves of a client into a
// violation; the ticks of the player keep a synchronous world, or a world
// sprinting ahead, generous.
type Clock struct {
	ref     time.Time
	refTick int64
	clock   float64
	set     bool
}

// Reset starts the clock again at the time and the tick passed, as when the
// player joins or changes world.
func (c *Clock) Reset(at time.Time, tick int64) {
	*c = Clock{ref: at, refTick: tick, set: true}
}

// Started reports whether the clock was reset since it was made.
func (c *Clock) Started() bool { return c.set }

// Now returns the ticks since the reference at the arrival time and the
// tick of the player passed. An arrival before the reference counts as the
// reference.
func (c *Clock) Now(arrived time.Time, tick int64) float64 {
	wall := 0.0
	if arrived.After(c.ref) {
		wall = float64(arrived.Sub(c.ref)) / float64(tickDuration)
	}
	return max(wall, float64(tick-c.refTick))
}

// Next returns the clock of the client after one more move at now: one
// tick on from where it was, or from now less maxBalance if it fell farther
// behind. Next does not count the move.
func (c *Clock) Next(now float64, maxBalance float64) float64 {
	return max(c.clock, now-maxBalance) + 1
}

// Ahead returns how many ticks the client would be ahead of time with one
// more move at now.
func (c *Clock) Ahead(now float64, maxBalance float64) float64 {
	return c.Next(now, maxBalance) - now
}

// Count counts a move at now.
func (c *Clock) Count(now float64, maxBalance float64) {
	c.clock = c.Next(now, maxBalance)
}
