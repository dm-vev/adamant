package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/go-gl/mathgl/mgl64"
)

// acState is what the anti-cheat keeps of a player with a client. A player
// without a client, or whose Config has no AntiCheat, has none.
type acState struct {
	conf   *anticheat.Config
	exempt func(p *Player, c anticheat.Check) bool

	// levels are the violation levels of the checks, and logs what they
	// logged.
	levels [anticheat.NumChecks]anticheat.Level
	logs   [anticheat.NumChecks]violationLog
	// exemptUntil holds the tick ExemptAntiCheat exempts the player from
	// each check until, that of every check last.
	exemptUntil [anticheat.NumChecks + 1]int64
	// depth counts the checked actions running: an action a handler of one
	// asks for is the plugin's, not the client's.
	depth int

	// graceUntil is the tick until which the movement checks skip moves.
	// moved is set once the client moved the player, clientMove while a
	// move of the client is applied.
	graceUntil        int64
	moved, clientMove bool
	// setbackPending is set from a setback until its acknowledgement, which
	// starts no grace.
	setbackPending bool
	// last is what the movement checks keep of the last move of the client.
	last lastMove

	// impulses are the velocities the server gave the player that its
	// client may still apply, each until a tick of the player.
	impulses []impulse
	// speed, jumpBoost and levitation hold the most of those over the
	// round trip to the client, which learns that they fell a round trip
	// later.
	speed, jumpBoost, levitation held
}

// impulse is a velocity the server gave a player with a client, until a
// tick of the player.
type impulse struct {
	vel   mgl64.Vec3
	until int64
}

// held is the most of a value over a window of ticks: a value that falls is
// held until the window passed since it was last at its height.
type held struct {
	value float64
	until int64
}

// observe records the value v at tick now and returns the most it was over
// the window.
func (h *held) observe(now int64, v float64, window int64) float64 {
	if v >= h.value || now > h.until {
		h.value, h.until = v, now+window
	}
	return h.value
}

// anticheat returns the anti-cheat state of the player, nil if it is not
// checked: it has no client, or its Config no AntiCheat.
func (p *Player) anticheat() *acState {
	if p.acState == nil {
		if p.c == nil || p.acConf == nil {
			return nil
		}
		p.acState = &acState{conf: p.acConf, exempt: p.acExempt}
	}
	return p.acState
}

// latencyTicks returns the ticks of a round trip to the client of the
// player, capped by MaxLatencyTicks, and the grace of an impulse on top:
// how long the client may take to apply what the server tells it.
func (p *Player) latencyTicks(ac *acState) int64 {
	rtt := 2 * p.client().Latency()
	ticks := int64((rtt + anticheatTick - 1) / anticheatTick)
	return min(ticks, int64(ac.conf.MaxLatencyTicks)) + int64(ac.conf.Grace.Impulse)
}

// anticheatTick is the time a tick takes at 20 ticks a second.
const anticheatTick = time.Second / anticheat.TicksPerSecond

// recordImpulse keeps a velocity the server gave the player, as its client
// applies it within a round trip.
func (p *Player) recordImpulse(vel mgl64.Vec3) {
	ac := p.anticheat()
	if ac == nil || vel == (mgl64.Vec3{}) {
		return
	}
	now := p.tickCount
	ac.impulses = append(ac.impulses[:0:0], ac.liveImpulses(now)...)
	ac.impulses = append(ac.impulses, impulse{vel: vel, until: now + p.latencyTicks(ac)})
}

// liveImpulses returns the impulses that have not expired at tick now.
func (ac *acState) liveImpulses(now int64) []impulse {
	live := ac.impulses[:0]
	for _, i := range ac.impulses {
		if i.until >= now {
			live = append(live, i)
		}
	}
	ac.impulses = live
	return live
}

// pendingImpulse returns the largest impulse the client of the player may
// still apply: the most of their horizontal lengths as X, the most of their
// upward speeds as Y.
func (p *Player) pendingImpulse(ac *acState) mgl64.Vec3 {
	var largest mgl64.Vec3
	for _, i := range ac.liveImpulses(p.tickCount) {
		largest[0] = max(largest[0], mgl64.Vec2{i.vel[0], i.vel[2]}.Len())
		largest[1] = max(largest[1], i.vel[1])
	}
	return largest
}

// heldSpeed returns the most the speed of the player was over the round
// trip to its client.
func (p *Player) heldSpeed(ac *acState) float64 {
	return ac.speed.observe(p.tickCount, p.Speed(), p.latencyTicks(ac))
}

// heldEffect returns the most the level of an effect of the player was
// over the round trip to its client.
func (p *Player) heldEffect(ac *acState, h *held, t effect.Type) int {
	level := 0
	if e, ok := p.Effect(t); ok {
		level = e.Level()
	}
	return int(h.observe(p.tickCount, float64(level), p.latencyTicks(ac)))
}
