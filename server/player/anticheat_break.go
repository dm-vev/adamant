package player

import (
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player/anticheat"
)

// breakState is what BreakTime keeps of the block the player breaks.
type breakState struct {
	// progress is the part of the break done, from the break time of each
	// tick: tools switched and effects that changed count as they did.
	progress float64
	// since is when the break started, by the wall clock, which a lagging
	// server does not slow, and arrivedSince when the input that started
	// it reached the server, which a lagging server does not delay.
	since, arrivedSince time.Time
	// delayed is set once an early finish was delayed, Java's delayed
	// destroy: the block breaks once its time is served.
	delayed bool
	// instant holds the blocks a player may break at once.
	instant anticheat.Bucket
}

// breakTicks returns the ticks breaking the block at pos takes now, at
// least a tick's worth: a block that breaks at once takes 0, a tick.
func (p *Player) breakTicks(pos cube.Pos) float64 {
	return max(float64(p.breakTime(pos))/float64(anticheatTick), 0)
}

// tickProgress returns the part of the break of the block at pos a tick
// does now: 1 for a block that breaks in a tick.
func (p *Player) tickProgress(pos cube.Pos) float64 {
	ticks := p.breakTicks(pos)
	if ticks <= 1 {
		return 1
	}
	return 1 / ticks
}

// startBreakTime starts the progress of a break.
func (p *Player) startBreakTime() {
	if ac := p.anticheat(); ac != nil {
		ac.breaking = breakState{since: ac.wallNow(), arrivedSince: p.inputArrival(ac), instant: ac.breaking.instant}
	}
}

// continueBreakTime adds the progress of a tick to the break, and, if an
// early finish was delayed, breaks the block once its time is served. It
// reports whether it broke the block.
func (p *Player) continueBreakTime() bool {
	ac := p.anticheat()
	if ac == nil || !p.breaking {
		return false
	}
	ac.breaking.progress += p.tickProgress(p.breakingPos)
	if ac.breaking.delayed && p.breakProgress(ac) >= 1 {
		ac.breaking.delayed = false
		p.AbortBreaking()
		p.BreakBlock(p.breakingPos)
		return true
	}
	return false
}

// inputArrival returns when the input of the client that the player handles
// in this tick reached the server, the wall clock now if there is none or it
// does not say. A server that lags hands the inputs of a client together, in
// a tick, though they arrived apart: the time between them is the client's
// own.
func (p *Player) inputArrival(ac *acState) time.Time {
	if ac.inputAt.IsZero() || ac.inputTick != p.tickCount {
		return ac.wallNow()
	}
	return ac.inputAt
}

// breakProgress returns the part of the break done: the most of what the
// ticks added and of the time since the start over the break time now, by
// the wall clock when the inputs were handled and by the time they arrived,
// the later of the two: a server that lags handles the start and the finish
// of a break in one tick.
func (p *Player) breakProgress(ac *acState) float64 {
	wall := 0.0
	if ticks := p.breakTicks(p.breakingPos); ticks > 0 {
		elapsed := max(ac.wallNow().Sub(ac.breaking.since), p.inputArrival(ac).Sub(ac.breaking.arrivedSince))
		wall = float64(elapsed) / float64(anticheatTick) / ticks
	}
	return max(ac.breaking.progress, wall)
}

// finishBreakEarly judges a finish of the break the client of the player
// asks for: as Java, a finish whose progress with the tick it comes in is
// at least Tolerance breaks the block. It reports whether the finish is
// refused: the block is resent, as BDS corrects a prediction it disagrees
// with, or, with Delay, it breaks once its time is served. Creative
// players break at once.
func (p *Player) finishBreakEarly() (refused bool) {
	ac, end := p.beginAction()
	defer end()
	if !p.checks(ac, anticheat.BreakTime) || p.GameMode().CreativeInventory() || !p.breaking {
		return false
	}
	progress := p.breakProgress(ac) + p.tickProgress(p.breakingPos)
	tolerance := ac.conf.BreakTime.Tolerance
	if progress >= tolerance {
		return false
	}
	name, _ := p.tx.Block(p.breakingPos).EncodeBlock()
	act := p.violate(ac, anticheat.BreakTime, "early", tolerance-progress, fmt.Sprintf("%s at %.2f of %.1f ticks", name, progress, p.breakTicks(p.breakingPos)))
	switch act {
	case anticheat.Log:
		return false
	case anticheat.Kick:
		return true
	}
	if ac.conf.BreakTime.Delay {
		ac.breaking.delayed = true
		return true
	}
	pos := p.breakingPos
	p.AbortBreaking()
	p.resendNearbyBlock(pos)
	return true
}

// DestroyBlock breaks the block at pos at once, as the client of the player
// did. In survival, BreakTime allows only a block that breaks in a tick,
// and InstantPerSecond of those, with a burst of InstantBurst: a client
// that breaks others at once is refused, and the block resent. Creative
// players break any block at once, as BDS lets creative clients do.
func (p *Player) DestroyBlock(pos cube.Pos) {
	if p.refuseDestroy(pos) {
		p.resendNearbyBlocks(pos)
		return
	}
	p.BreakBlock(pos)
}

// refuseDestroy reports whether BreakTime refuses the instant break of the
// block at pos the client of the player asks for.
func (p *Player) refuseDestroy(pos cube.Pos) bool {
	ac, end := p.beginAction()
	defer end()
	if !p.checks(ac, anticheat.BreakTime) || p.GameMode().CreativeInventory() {
		return false
	}
	name, _ := p.tx.Block(pos).EncodeBlock()
	conf := ac.conf.BreakTime
	var act anticheat.Action
	switch {
	case p.breakTicks(pos) > 1:
		act = p.violate(ac, anticheat.BreakTime, "instant", p.breakTicks(pos), fmt.Sprintf("%s takes %.1f ticks", name, p.breakTicks(pos)))
	case !ac.breaking.instant.Take(p.inputArrival(ac), conf.InstantPerSecond, conf.InstantBurst):
		act = p.violate(ac, anticheat.BreakTime, "rate", 1, fmt.Sprintf("%s beyond %v a second", name, conf.InstantPerSecond))
	default:
		return false
	}
	return act != anticheat.Log
}
