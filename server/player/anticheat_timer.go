package player

import (
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player/anticheat"
)

// timerState is what Timer keeps of the moves of a client: its clock and
// whether a throttle episode was counted.
type timerState struct {
	clock anticheat.Clock
	// episode is set once a throttle episode was counted as a violation,
	// until a move passes that did not wait.
	episode bool
}

// arrivalOf returns when the move m reached the server, now if it does not
// say.
func (ac *acState) arrivalOf(m client.Move) time.Time {
	if m.Arrived.IsZero() {
		return ac.wallNow()
	}
	return m.Arrived
}

// wallNow returns the time of the wall clock, which a test may set.
func (ac *acState) wallNow() time.Time {
	if ac.now != nil {
		return ac.now()
	}
	return time.Now()
}

// timerNow returns the clock of the timer of the player at the move m,
// started at the move if it was not.
func (p *Player) timerNow(ac *acState, m client.Move) float64 {
	arrived := ac.arrivalOf(m)
	if !ac.timer.clock.Started() {
		ac.timer.clock.Reset(arrived, p.tickCount)
	}
	return ac.timer.clock.Now(arrived, p.tickCount)
}

// countTimer counts a move m of the client on the clock of the timer
// without judging it: a move awaiting a teleport, which the game does not
// apply, is a tick of the client still.
func (p *Player) countTimer(ac *acState, m client.Move) {
	if !p.checks(ac, anticheat.Timer) {
		return
	}
	ac.timer.clock.Count(p.timerNow(ac, m), float64(ac.conf.Timer.MaxBalance))
}

// checkTimer holds a client to real time: a client may make up MaxBalance
// ticks it fell behind, and run Tolerance ticks ahead; a move beyond that
// is ahead of time. checkTimer reports whether it decided what becomes of
// the move m, and what.
//
// With the action throttle, the default, a move ahead of time is deferred:
// the client hands it again, with what came after it, in a later tick, as
// BDS queues inputs. A move that waited longer than Tolerance ticks counts
// as a violation, once a throttling episode, and one that waited longer
// than MaxQueue is dropped and the player set back. With setback, a move
// ahead of time is dropped and the player set back; with log, it is
// applied.
func (p *Player) checkTimer(ac *acState, m client.Move) (client.MoveResult, bool) {
	if !p.checks(ac, anticheat.Timer) {
		return 0, false
	}
	conf := ac.conf.Timer
	balance, tolerance := float64(conf.MaxBalance), float64(conf.Tolerance)
	now := p.timerNow(ac, m)
	ahead := ac.timer.clock.Ahead(now, balance)
	waited := p.waitedTicks(ac, m)
	if ahead <= tolerance {
		ac.timer.clock.Count(now, balance)
		if waited <= 1 {
			ac.timer.episode = false
		}
		return 0, false
	}
	detail := fmt.Sprintf("%.1f ticks ahead, waited %d", ahead, waited)
	if ac.conf.Policy(anticheat.Timer).Action != anticheat.Throttle {
		switch p.violate(ac, anticheat.Timer, "ahead", ahead-tolerance, detail) {
		case anticheat.Setback, anticheat.Throttle:
			p.setBack(ac, p.Position(), m.Tick)
			return client.MoveRefused, true
		case anticheat.Kick:
			return client.MoveRefused, true
		}
		ac.timer.clock.Count(now, balance)
		return 0, false
	}
	if waited > int64(conf.MaxQueue) {
		act := p.violate(ac, anticheat.Timer, "queue", float64(waited-int64(conf.MaxQueue)), detail)
		if act == anticheat.Kick {
			return client.MoveRefused, true
		}
		if act != anticheat.Log && !ac.setbackPending {
			p.setBack(ac, p.Position(), m.Tick)
		}
		return client.MoveRefused, true
	}
	if waited > int64(conf.Tolerance) && !ac.timer.episode {
		ac.timer.episode = true
		switch p.violate(ac, anticheat.Timer, "ahead", ahead-tolerance, detail) {
		case anticheat.Kick:
			return client.MoveRefused, true
		case anticheat.Log:
			ac.timer.clock.Count(now, balance)
			return 0, false
		}
	}
	return client.MoveDeferred, true
}

// waitedTicks returns how many ticks the move m waited since it arrived,
// by the wall clock, so that the ticks of a server running fast or slow
// over hours do not count.
func (p *Player) waitedTicks(ac *acState, m client.Move) int64 {
	return max(0, int64(ac.wallNow().Sub(ac.arrivalOf(m))/anticheatTick))
}

// resetTimer starts the clock of the timer of the player again, as when it
// changed world.
func (p *Player) resetTimer() {
	if ac := p.anticheat(); ac != nil {
		ac.timer = timerState{}
	}
}
