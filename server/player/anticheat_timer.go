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

// maxServerLagTicks is the most ticks late the server is credited with: 10 s.
const maxServerLagTicks = 10 * anticheat.TicksPerSecond

// nextTick counts a tick of the player, and keeps when it came, for how
// late the server is.
func (p *Player) nextTick() {
	p.tickCount++
	if ac := p.acState; ac != nil {
		ac.prevTickAt, ac.tickAt = ac.tickAt, ac.wallNow()
	}
}

// serverLag returns how many ticks late the server is, from the time
// between the last ticks of the player and the one now under way. A server
// that lags hands the inputs of a client in bursts, whatever time they
// arrived at: the timer credits the client with the ticks the server lost,
// which no client can make, and only a cheat that makes the server lag
// would use.
func (ac *acState) serverLag() float64 {
	if ac.tickAt.IsZero() {
		return 0
	}
	gap := ac.wallNow().Sub(ac.tickAt)
	if !ac.prevTickAt.IsZero() {
		gap = max(gap, ac.tickAt.Sub(ac.prevTickAt))
	}
	return min(max(0, float64(gap)/float64(anticheatTick)-1), maxServerLagTicks)
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
// ticks it fell behind, and run Tolerance ticks ahead, both and MaxQueue
// longer by the ticks the server is late; a move beyond that is ahead of
// time. checkTimer reports whether it decided what becomes of
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
	lag := ac.serverLag()
	balance, tolerance := float64(conf.MaxBalance)+lag, float64(conf.Tolerance)+lag
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
	if waited > int64(conf.MaxQueue)+int64(lag) {
		act := p.violate(ac, anticheat.Timer, "queue", float64(waited-int64(conf.MaxQueue)), detail)
		if act == anticheat.Kick {
			return client.MoveRefused, true
		}
		if act != anticheat.Log && !ac.setbackPending {
			p.setBack(ac, p.Position(), m.Tick)
		}
		return client.MoveRefused, true
	}
	if waited > int64(tolerance) && !ac.timer.episode {
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
