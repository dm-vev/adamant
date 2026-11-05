package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/player/anticheat"
)

// violationLog is what the anti-cheat logs of a check of a player: the
// first violation at once, then a summary every LogInterval ticks.
type violationLog struct {
	logged bool
	last   int64
	count  int
	maxVL  float64
}

// checks reports whether the check c judges what the client of the player
// asks: the anti-cheat and the check are on, and the player is not exempt.
func (p *Player) checks(ac *acState, c anticheat.Check) bool {
	return ac != nil && !p.exemptFrom(ac, c)
}

// exemptFrom reports whether the player skips the check c: the check is
// off, the game mode of the player is one it exempts, ExemptAntiCheat
// exempted the player, it is an operator and operators are exempt, it is
// listed in ExemptPlayers, or the hook of the server says so.
func (p *Player) exemptFrom(ac *acState, c anticheat.Check) bool {
	if !ac.conf.Active(c) || ac.conf.Policy(c).Exempts(p.GameMode()) {
		return true
	}
	if now := p.tickCount; ac.exemptUntil[c] > now || ac.exemptUntil[anticheat.NumChecks] > now {
		return true
	}
	if ac.conf.ExemptOperators && cmd.IsOperator(p.UUID()) {
		return true
	}
	if p.listedExempt(ac) {
		return true
	}
	return ac.exempt != nil && ac.exempt(p, c)
}

// listedExempt reports whether the player is one of the ExemptPlayers, by
// its name or its XUID.
func (p *Player) listedExempt(ac *acState) bool {
	if len(ac.conf.ExemptPlayers) == 0 {
		return false
	}
	name, _ := identity.OfName(p.Name())
	xuid, err := identity.OfXUID(p.XUID())
	for _, k := range ac.conf.ExemptPlayers {
		if k == name || (err == nil && k == xuid) {
			return true
		}
	}
	return false
}

// violate judges a violation of the check c, which the caller checks: the
// handler of the player may cancel it or change its action; otherwise the
// level of the check grows by one, the violation is logged, and violate
// returns the action the caller takes, Log if the level is at most ActAt.
// At KickAt, or with the action Kick, the player is disconnected and Kick
// returned: the caller then leaves the player alone.
func (p *Player) violate(ac *acState, c anticheat.Check, kind string, amount float64, detail string) anticheat.Action {
	pol := ac.conf.Policy(c)
	now := p.tickCount
	v := &anticheat.Violation{Check: c, Kind: kind, Detail: detail, Amount: amount, Action: pol.Action, Tick: now}
	v.VL = ac.levels[c].At(now, pol.Decay) + 1
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleViolation(ctx, v); ctx.Cancelled() {
		return anticheat.Log
	}
	vl := ac.levels[c].Add(now, pol.Decay, 1)
	act := anticheat.Log
	if vl > pol.ActAt {
		act = v.Action
	}
	if pol.KickAt > 0 && vl >= pol.KickAt {
		act = anticheat.Kick
	}
	p.logViolation(ac, v, vl, act)
	if act == anticheat.Kick {
		p.Disconnect(pol.KickMessage)
	}
	return act
}

// logViolation logs a violation at Info, to the logger of the player: the
// first of a check at once, then one line every LogInterval ticks with
// how many came since and the highest level among them.
func (p *Player) logViolation(ac *acState, v *anticheat.Violation, vl float64, act anticheat.Action) {
	l := &ac.logs[v.Check]
	l.count++
	l.maxVL = max(l.maxVL, vl)
	if l.logged && v.Tick-l.last < int64(ac.conf.LogInterval) && act != anticheat.Kick {
		return
	}
	logging.Sub(p.log, logging.AntiCheat).Info("anti-cheat", "check", v.Check.String(), "vl", round2(vl), "kind", v.Kind, "detail", v.Detail,
		"action", act.String(), "violations", l.count, "max-vl", round2(l.maxVL))
	l.logged, l.last, l.count, l.maxVL = true, v.Tick, 0, 0
}

// round2 rounds v to two decimals, for the log.
func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

// ExemptAntiCheat exempts the player from the check c for the duration
// passed, from every check for anticheat.All. A duration of 0 or less ends
// such an exemption.
func (p *Player) ExemptAntiCheat(c anticheat.Check, d time.Duration) {
	ac := p.anticheat()
	if ac == nil {
		return
	}
	until := p.tickCount + int64((d+anticheatTick-1)/anticheatTick)
	if c == anticheat.All {
		ac.exemptUntil[anticheat.NumChecks] = until
		return
	}
	if int(c) < anticheat.NumChecks {
		ac.exemptUntil[c] = until
	}
}

// AntiCheatLevel returns the violation level of the check c of the player:
// how many violations it made of late, less what decayed since. It is 0
// for a player that is not checked.
func (p *Player) AntiCheatLevel(c anticheat.Check) float64 {
	ac := p.anticheat()
	if ac == nil || int(c) >= anticheat.NumChecks {
		return 0
	}
	return ac.levels[c].At(p.tickCount, ac.conf.Policy(c).Decay)
}

// ResetAntiCheat sets the violation level of the check c of the player to
// 0, of every check for anticheat.All.
func (p *Player) ResetAntiCheat(c anticheat.Check) {
	ac := p.anticheat()
	if ac == nil {
		return
	}
	for _, ch := range anticheat.Checks() {
		if c == anticheat.All || c == ch {
			ac.levels[ch].Reset()
		}
	}
}

// startGrace has the movement checks skip the moves of the player for the
// ticks passed.
func (p *Player) startGrace(ticks int) {
	if ac := p.anticheat(); ac != nil {
		ac.graceUntil = max(ac.graceUntil, p.tickCount+int64(ticks))
		ac.resetMovement()
	}
}

// graceOf returns the grace period of the anti-cheat of the player that
// f picks, 0 if it is not checked.
func (p *Player) graceOf(f func(g anticheat.Grace) int) int {
	if ac := p.anticheat(); ac != nil {
		return f(ac.conf.Grace)
	}
	return 0
}

// inGrace reports whether the movement checks skip the moves of the player.
func (p *Player) inGrace(ac *acState) bool {
	return ac.graceUntil > p.tickCount
}

// resetMovement forgets what the movement checks know of the moves of the
// player, as after a setback or a teleport: its client starts from rest.
func (ac *acState) resetMovement() {
	ac.last = lastMove{}
	ac.hover = 0
}
