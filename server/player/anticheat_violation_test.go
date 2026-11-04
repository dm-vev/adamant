package player

import (
	"bytes"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// violationRecorder is a Handler that records the violations of its player
// and may cancel them or change their action.
type violationRecorder struct {
	NopHandler
	violations []anticheat.Violation
	cancel     bool
	action     *anticheat.Action
}

func (h *violationRecorder) HandleViolation(ctx *Context, v *anticheat.Violation) {
	h.violations = append(h.violations, *v)
	if h.cancel {
		ctx.Cancel()
	}
	if h.action != nil {
		v.Action = *h.action
	}
}

// count returns the violations of the check passed.
func (h *violationRecorder) count(c anticheat.Check) int {
	n := 0
	for _, v := range h.violations {
		if v.Check == c {
			n++
		}
	}
	return n
}

// withPolicy returns the default anti-cheat with the policy of c edited.
func withPolicy(c anticheat.Check, edit func(p *anticheat.Policy)) *anticheat.Config {
	conf := anticheat.Default()
	p := conf.Policy(c)
	edit(&p)
	conf.SetPolicy(c, p)
	return conf
}

// farMove moves the client of p 12 blocks along Z, farther than a tick
// allows.
func farMove(p *Player) client.MoveResult {
	return p.ClientMove(client.Move{Position: p.Position().Add(mgl64.Vec3{0, 0, 12})})
}

// TestViolationLevelsAndActions checks that a check acts once its level
// passed ActAt: with ActAt 1, the first far move is logged and applied, the
// second set back.
func TestViolationLevelsAndActions(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	conf := withPolicy(anticheat.MovedTooQuickly, func(p *anticheat.Policy) { p.ActAt = 1 })
	withCheckedPlayer(t, w, r, conf, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		h := &violationRecorder{}
		p.Handle(h)
		if got := farMove(p); got != client.MoveApplied {
			t.Fatalf("the first far move: %v, want applied", got)
		}
		if got := farMove(p); got != client.MoveRefused {
			t.Fatalf("the second far move: %v, want refused", got)
		}
		if n := h.count(anticheat.MovedTooQuickly); n != 2 || h.violations[1].VL != 2 || h.violations[1].Kind != "distance" {
			t.Fatalf("violations %+v", h.violations)
		}
		if len(r.CallsTo("CorrectMove")) != 1 {
			t.Fatalf("corrections %v, want one", r.CallsTo("CorrectMove"))
		}
		if got := p.AntiCheatLevel(anticheat.MovedTooQuickly); got != 2 {
			t.Fatalf("level %v, want 2", got)
		}
		// A second later, one has decayed.
		for i := int64(0); i < 20; i++ {
			p.Tick(tx, i)
		}
		if got := p.AntiCheatLevel(anticheat.MovedTooQuickly); got < 0.99 || got > 1.01 {
			t.Fatalf("level a second later %v, want 1", got)
		}
		p.ResetAntiCheat(anticheat.All)
		if got := p.AntiCheatLevel(anticheat.MovedTooQuickly); got != 0 {
			t.Fatalf("level after ResetAntiCheat %v, want 0", got)
		}
	})
}

// TestViolationHandler checks that a handler may ignore a violation, which
// then does nothing, or change what the game does of it.
func TestViolationHandler(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withCheckedPlayer(t, w, r, anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		h := &violationRecorder{cancel: true}
		p.Handle(h)
		if got := farMove(p); got != client.MoveApplied || p.AntiCheatLevel(anticheat.MovedTooQuickly) != 0 {
			t.Fatalf("a cancelled violation: %v, level %v; want applied, 0", got, p.AntiCheatLevel(anticheat.MovedTooQuickly))
		}
		log := anticheat.Log
		h.cancel, h.action = false, &log
		if got := farMove(p); got != client.MoveApplied || p.AntiCheatLevel(anticheat.MovedTooQuickly) != 1 {
			t.Fatalf("a violation the handler only logs: %v, level %v; want applied, 1", got, p.AntiCheatLevel(anticheat.MovedTooQuickly))
		}
		if len(r.CallsTo("CorrectMove")) != 0 {
			t.Fatal("a move was corrected")
		}
	})
}

// TestViolationKicks checks that a player whose level reaches KickAt is
// disconnected with the kick message of the check.
func TestViolationKicks(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	conf := withPolicy(anticheat.MovedTooQuickly, func(p *anticheat.Policy) { p.KickAt = 2 })
	withCheckedPlayer(t, w, r, conf, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		farMove(p)
		if len(r.CallsTo("Disconnect")) != 0 {
			t.Fatal("kicked at a level of 1")
		}
		if got := farMove(p); got != client.MoveRefused {
			t.Fatalf("the move that kicked: %v, want refused", got)
		}
		calls := r.CallsTo("Disconnect")
		if len(calls) != 1 || argText(t, calls[0], 0).Plain() != "Invalid move player packet received" {
			t.Fatalf("disconnects %v", calls)
		}
	})
}

// TestViolationLogs checks that a check logs its first violation at once,
// then a summary every LogInterval ticks.
func TestViolationLogs(t *testing.T) {
	w, _ := clientTestWorld(t)
	var logged bytes.Buffer
	log := slog.New(slog.NewTextHandler(&logged, nil))
	conf := withPolicy(anticheat.MovedTooQuickly, func(p *anticheat.Policy) { p.Action = anticheat.Log })
	withCheckedPlayer(t, w, clienttest.New(), conf, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		for range 5 {
			farMove(p)
		}
		if n := strings.Count(logged.String(), "msg=anti-cheat"); n != 1 {
			t.Fatalf("logged %d lines of five violations in a tick, want 1:\n%s", n, logged.String())
		}
		if !strings.Contains(logged.String(), "check=MovedTooQuickly vl=1 kind=distance") || !strings.Contains(logged.String(), "action=log") {
			t.Fatalf("logged %s", logged.String())
		}
		for i := int64(1); i <= 20; i++ {
			p.Tick(tx, i)
		}
		farMove(p)
		if n := strings.Count(logged.String(), "msg=anti-cheat"); n != 2 || !strings.Contains(logged.String(), "violations=5") {
			t.Fatalf("logged after the interval:\n%s", logged.String())
		}
	}, func(c *Config) { c.Log = log })
}

// TestExempt checks the exemptions: a game mode, a listed player by name
// and by XUID, the hook of the server, ExemptAntiCheat and operators.
func TestExempt(t *testing.T) {
	steve, _ := identity.OfName("cheater")
	xuid, _ := identity.OfXUID("2535400000000042")
	for _, tc := range []struct {
		name string
		conf func(c *anticheat.Config)
		edit func(c *Config)
		do   func(p *Player)
	}{
		{"game mode", func(c *anticheat.Config) {
			p := c.Policy(anticheat.MovedTooQuickly)
			p.ExemptGameModes = []world.GameMode{world.GameModeCreative}
			c.SetPolicy(anticheat.MovedTooQuickly, p)
		}, nil, func(p *Player) { p.SetGameMode(world.GameModeCreative) }},
		{"listed by name", func(c *anticheat.Config) { c.ExemptPlayers = []identity.Key{steve} }, nil, nil},
		{"listed by XUID", func(c *anticheat.Config) { c.ExemptPlayers = []identity.Key{xuid} }, nil, nil},
		{"the hook", nil, func(c *Config) {
			c.AntiCheatExempt = func(_ *Player, ch anticheat.Check) bool { return ch == anticheat.MovedTooQuickly }
		}, nil},
		{"ExemptAntiCheat of the check", nil, nil, func(p *Player) { p.ExemptAntiCheat(anticheat.MovedTooQuickly, time.Second) }},
		{"ExemptAntiCheat of every check", nil, nil, func(p *Player) { p.ExemptAntiCheat(anticheat.All, time.Second) }},
		{"disabled", func(c *anticheat.Config) {
			p := c.Policy(anticheat.MovedTooQuickly)
			p.Enabled = false
			c.SetPolicy(anticheat.MovedTooQuickly, p)
		}, nil, nil},
		{"everything disabled", func(c *anticheat.Config) { c.Enabled = false }, nil, nil},
	} {
		w, _ := clientTestWorld(t)
		conf := anticheat.Default()
		if tc.conf != nil {
			tc.conf(conf)
		}
		var edits []func(c *Config)
		if tc.edit != nil {
			edits = append(edits, tc.edit)
		}
		withCheckedPlayer(t, w, clienttest.New(), conf, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
			h := &violationRecorder{}
			p.Handle(h)
			if tc.do != nil {
				tc.do(p)
			}
			if got := farMove(p); got != client.MoveApplied || len(h.violations) != 0 {
				t.Errorf("%s: %v with violations %+v; want applied, none", tc.name, got, h.violations)
			}
		}, edits...)
	}
	// ExemptAntiCheat lasts its duration.
	w, _ := clientTestWorld(t)
	withCheckedPlayer(t, w, clienttest.New(), anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		p.ExemptAntiCheat(anticheat.MovedTooQuickly, 100*time.Millisecond)
		for i := int64(0); i < 2; i++ {
			p.Tick(tx, i)
		}
		if got := farMove(p); got != client.MoveRefused {
			t.Errorf("after the exemption: %v, want refused", got)
		}
	})
}

// TestExemptOperators checks that operators skip the checks only with
// ExemptOperators.
func TestExemptOperators(t *testing.T) {
	var op atomic.Pointer[uuid.UUID]
	cmd.SetOperatorChecker(func(u uuid.UUID) bool { id := op.Load(); return id != nil && *id == u })
	t.Cleanup(func() { cmd.SetOperatorChecker(func(uuid.UUID) bool { return false }) })
	for _, exempt := range []bool{false, true} {
		w, _ := clientTestWorld(t)
		conf := anticheat.Default()
		conf.ExemptOperators = exempt
		withCheckedPlayer(t, w, clienttest.New(), conf, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
			id := p.UUID()
			op.Store(&id)
			if got := farMove(p) == client.MoveApplied; got != exempt {
				t.Errorf("ExemptOperators %v: an operator's far move applied %v", exempt, got)
			}
		})
	}
}

// TestNestedActionIsNotChecked checks that an action a plugin asks of a
// player from the handler of a checked action of its client is not
// checked, and that only actions of the client are.
func TestNestedActionIsNotChecked(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withCheckedPlayer(t, w, r, anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		if ac, end := p.beginAction(); ac != nil {
			t.Fatal("an action without the client's input is checked")
		} else {
			end()
		}
		r.SetHandlingInput(true)
		outer, endOuter := p.beginAction()
		inner, endInner := p.beginAction()
		if outer == nil || inner != nil {
			t.Fatalf("outer action checked %v, inner %v; want the outer only", outer != nil, inner != nil)
		}
		endInner()
		endOuter()
		if again, end := p.beginAction(); again == nil {
			t.Fatal("an action after the nested ones ended is not checked")
		} else {
			end()
		}
	})
}

// TestGrace checks what starts the grace of the movement checks: the first
// move after the player spawned, a teleport and its acknowledgement, a
// change of game mode, a dismount, a respawn and a server move; a setback's
// acknowledgement starts none.
func TestGrace(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withCheckedPlayer(t, w, r, anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		ac := p.anticheat()
		tick := func(n int) {
			for range n {
				p.Tick(tx, p.tickCount)
			}
		}
		p.ClientMove(client.Move{Position: p.Position().Add(mgl64.Vec3{0.1, 0, 0})})
		if !p.inGrace(ac) {
			t.Fatal("no grace after the first move")
		}
		tick(61)
		if p.inGrace(ac) {
			t.Fatal("still in grace 61 ticks after joining")
		}
		for name, start := range map[string]func(){
			"teleport":    func() { p.Teleport(p.Position().Add(mgl64.Vec3{5, 0, 0})) },
			"game mode":   func() { p.SetGameMode(world.GameModeAdventure) },
			"server move": func() { p.Move(mgl64.Vec3{0.5, 0, 0}, 0, 0) },
			"scale":       func() { p.SetScale(0.5) },
			"acknowledged teleport": func() {
				p.ClientMove(client.Move{Position: p.Position(), Acknowledged: true})
			},
			"dismount": func() {
				p.data.Riding = world.EntitySpawnOpts{}.New(Type, Config{})
				p.SetRiding(nil)
			},
		} {
			tick(41)
			if p.inGrace(ac) {
				t.Fatalf("%s: in grace before", name)
			}
			start()
			if !p.inGrace(ac) {
				t.Errorf("%s: no grace after", name)
			}
		}
		tick(41)
		ac.setbackPending = true
		p.ClientMove(client.Move{Position: p.Position(), Acknowledged: true})
		if p.inGrace(ac) {
			t.Error("the acknowledgement of a setback started a grace")
		}
	})
}

// TestMovedTooQuicklyFollowsTheGameRules checks that MovedTooQuickly is
// off with player_movement_check false, and while gliding with
// elytra_movement_check false, as Java's movement checks are; and while
// the client loads a world.
func TestMovedTooQuicklyFollowsTheGameRules(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rule    string
		gliding bool
		loading bool
		applied bool
	}{
		{"by default", "", false, false, false},
		{"player_movement_check false", "player_movement_check", false, false, true},
		{"elytra_movement_check false, walking", "elytra_movement_check", false, false, false},
		{"elytra_movement_check false, gliding", "elytra_movement_check", true, false, true},
		{"loading", "", false, true, true},
	} {
		w, _ := clientTestWorld(t)
		if tc.rule != "" {
			if err := w.SetGameRule(tc.rule, false); err != nil {
				t.Fatal(err)
			}
		}
		withCheckedPlayer(t, w, clienttest.New(), anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
			if tc.gliding {
				p.Armour().SetChestplate(item.NewStack(item.Elytra{}, 1))
				p.StartGliding()
			}
			dist := 12.0
			if tc.gliding {
				dist = 20
			}
			got := p.ClientMove(client.Move{Position: p.Position().Add(mgl64.Vec3{0, 0, dist}), Loading: tc.loading})
			if (got == client.MoveApplied) != tc.applied {
				t.Errorf("%s: %v, want applied %v", tc.name, got, tc.applied)
			}
		})
	}
}

// TestGraceOfAWorldChange checks that a change of world, which Tick sees,
// starts the grace of the movement checks and the clock of the timer
// again.
func TestGraceOfAWorldChange(t *testing.T) {
	w, _ := clientTestWorld(t)
	other := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = other.Close() })
	withCheckedPlayer(t, w, clienttest.New(), anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		standThroughJoin(tx, p)
		ac := p.anticheat()
		if p.inGrace(ac) || !ac.timer.clock.Started() {
			t.Fatal("in grace, or no clock, after the join")
		}
		p.prevWorld = other
		p.Tick(tx, p.tickCount)
		if !p.inGrace(ac) || ac.timer.clock.Started() {
			t.Fatalf("after a change of world: grace %v, clock started %v; want grace, a clock to start", p.inGrace(ac), ac.timer.clock.Started())
		}
	})
}
