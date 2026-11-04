// Package anticheat holds what the checks the game runs on the clients of
// players are made of: their settings, as config.toml writes them and as
// they are parsed, the violation levels, rates and clocks they keep, and the
// envelopes of the movement of a player, which upper-bound what Java's
// physics lets it do in a tick. The checks themselves are in package
// player, which reads the state of the player and acts on violations.
//
// Nothing here knows a world or a protocol: everything is tested alone.
package anticheat

import "fmt"

// Check is one check of the anti-cheat. Its String is the name of its table
// in config.toml, below [AntiCheat].
type Check uint8

// The checks, movement first.
const (
	// MovedTooQuickly refuses a move no player makes in a tick, Java's
	// "moved too quickly": farther than a bound beyond the velocity the
	// server knows.
	MovedTooQuickly Check = iota
	// Speed bounds the horizontal speed by what Java's travel gives the
	// state of the player.
	Speed
	// Fly bounds hovering, Java's "floating too long", and refuses a flight
	// the game mode does not allow.
	Fly
	// Ascend bounds the vertical speed by a jump, a climb, a bounce or an
	// impulse, and catches the up-blips of NoFall.
	Ascend
	// NoClip refuses a move into blocks the player was not in.
	NoClip
	// Phase finds a move through a block that blocks its path, Java's
	// "moved wrongly".
	Phase
	// EntityReach bounds the reach of attacks and interactions with
	// entities.
	EntityReach
	// BlockReach bounds the reach of breaking, placing and using blocks.
	BlockReach
	// BreakTime holds breaking a block to the time it takes.
	BreakTime
	// Place bounds the rate of placing blocks and checks the face and point
	// clicked.
	Place
	// Attack bounds the rate of attacks and the targets of a tick.
	Attack
	// AttackAim checks that an attack is aimed at its target.
	AttackAim
	// FastUse bounds the rate of using items that have no cooldown.
	FastUse
	// Timer holds a client to real time: no more ticks than the time that
	// went by.
	Timer
	// Menu closes a menu out of reach and bounds the operations of a tick.
	Menu

	// checkCount is the number of checks.
	checkCount

	// All stands for every check, where an exemption is given.
	All Check = 255
)

var checkNames = [checkCount]string{
	"MovedTooQuickly", "Speed", "Fly", "Ascend", "NoClip", "Phase",
	"EntityReach", "BlockReach", "BreakTime", "Place", "Attack", "AttackAim",
	"FastUse", "Timer", "Menu",
}

// String returns the name of the table of the check in config.toml, "All"
// for All.
func (c Check) String() string {
	if c == All {
		return "All"
	}
	if c < checkCount {
		return checkNames[c]
	}
	return fmt.Sprintf("Check(%d)", uint8(c))
}

// Checks returns every check, in the order of their constants.
func Checks() []Check {
	checks := make([]Check, checkCount)
	for i := range checks {
		checks[i] = Check(i)
	}
	return checks
}

// Movement reports whether c judges the moves of a client: those of
// MovedTooQuickly to Phase, and Timer.
func (c Check) Movement() bool { return c <= Phase || c == Timer }

// Action is what the game does once a violation of a check passed the
// threshold of its rule.
type Action uint8

const (
	// Log only logs the violation.
	Log Action = iota
	// Setback puts the client back where the server has its player, or, for
	// Fly, where it last stood.
	Setback
	// Cancel refuses what the client asked: the break, the place, the
	// attack. For Ascend, it keeps the fall distance through the move.
	Cancel
	// Throttle has a client ahead of time wait: its move is handled in a
	// later tick. Only Timer throttles.
	Throttle
	// Kick disconnects the player.
	Kick
)

var actionNames = [...]string{"log", "setback", "cancel", "throttle", "kick"}

// String returns the name of the action as config.toml writes it.
func (a Action) String() string {
	if int(a) < len(actionNames) {
		return actionNames[a]
	}
	return fmt.Sprintf("Action(%d)", uint8(a))
}

// actions returns the actions a check takes.
func (c Check) actions() []Action {
	switch c {
	case Ascend:
		return []Action{Log, Setback, Cancel, Kick}
	case Timer:
		return []Action{Log, Throttle, Setback, Kick}
	}
	if c.Movement() {
		return []Action{Log, Setback, Kick}
	}
	return []Action{Log, Cancel, Kick}
}

// Violation is one violation of a check, as Handler.HandleViolation of
// package player is passed it.
type Violation struct {
	// Check is the check violated.
	Check Check
	// Kind names what of the check was violated, such as "envelope" or
	// "toggle".
	Kind string
	// Detail tells the numbers that made the violation, for the log.
	Detail string
	// Amount is how far past its bound the violation went, in the unit of
	// the check.
	Amount float64
	// VL is the violation level of the check after this violation. A
	// handler that changes nothing reads it as the level the action is
	// judged by.
	VL float64
	// Action is what the game does if the level passed the threshold of
	// the rule. A handler may change it.
	Action Action
	// Tick is the tick of the player the violation happened in.
	Tick int64
}

// NumChecks is the number of checks: Check values below it are checks.
const NumChecks = int(checkCount)
