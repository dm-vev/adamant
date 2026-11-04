package player

import (
	"log/slog"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// withCheckedPlayer runs f with a player in w that has the client r and is
// checked by the anti-cheat conf, at pos. edit, if passed, edits the Config
// of the player first.
func withCheckedPlayer(t *testing.T, w *world.World, r *clienttest.Recorder, conf *anticheat.Config, pos mgl64.Vec3, f func(tx *world.Tx, p *Player), edit ...func(c *Config)) {
	t.Helper()
	c := Config{Position: pos, GameMode: world.GameModeSurvival, Client: r, AntiCheat: conf, Name: "Cheater", XUID: "2535400000000042",
		Log: slog.New(slog.DiscardHandler)}
	for _, e := range edit {
		e(&c)
	}
	handle := world.EntitySpawnOpts{Position: pos, ID: uuid.New()}.New(Type, c)
	<-w.Exec(func(tx *world.Tx) {
		f(tx, tx.AddEntity(handle).(*Player))
	})
}

// TestImpulseCarriesMovedTooQuickly checks that a move the server threw the
// player into, as an explosion does, is not set back as too quick while
// the client may still apply the impulse: the hole of the adapter's rule,
// which subtracted a velocity the server zeroes every tick.
func TestImpulseCarriesMovedTooQuickly(t *testing.T) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withCheckedPlayer(t, w, r, anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		far := func() client.MoveResult {
			return p.ClientMove(client.Move{Position: p.Position().Add(mgl64.Vec3{0, 0, 12})})
		}
		if got := far(); got != client.MoveRefused {
			t.Fatalf("12 blocks without an impulse: %v, want refused", got)
		}
		p.SetVelocity(mgl64.Vec3{0, 0.5, 12})
		if got := far(); got != client.MoveApplied {
			t.Fatalf("12 blocks after an impulse of 12: %v, want applied", got)
		}
		// The impulse lasts the round trip, 0 here, and the grace of 10
		// ticks.
		for i := int64(0); i < 11; i++ {
			p.Tick(tx, i)
		}
		if got := far(); got != client.MoveRefused {
			t.Fatalf("12 blocks 11 ticks after the impulse: %v, want refused", got)
		}
	})
}

// TestImpulseWindowFollowsLatency checks that an impulse lasts the round
// trip to the client and the grace, the round trip capped by
// MaxLatencyTicks.
func TestImpulseWindowFollowsLatency(t *testing.T) {
	for _, tc := range []struct {
		latency time.Duration
		ticks   int64
	}{
		{0, 10}, {150 * time.Millisecond, 16}, {500 * time.Millisecond, 30}, {time.Second, 50}, {2 * time.Second, 50},
	} {
		w, _ := clientTestWorld(t)
		r := clienttest.New()
		r.Answers.Latency = tc.latency
		withCheckedPlayer(t, w, r, anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
			p.SetVelocity(mgl64.Vec3{1, 0, 0})
			ac := p.anticheat()
			if got := ac.impulses[0].until - p.tickCount; got != tc.ticks {
				t.Errorf("latency %v: the impulse lasts %d ticks, want %d", tc.latency, got, tc.ticks)
			}
			if got := p.pendingImpulse(ac); got != (mgl64.Vec3{1, 0, 0}) {
				t.Errorf("latency %v: pending impulse %v", tc.latency, got)
			}
		})
	}
}

// TestImpulseOnlyWithAntiCheat checks that a player not checked keeps no
// impulses: its moves are judged as the adapter judged them.
func TestImpulseOnlyWithAntiCheat(t *testing.T) {
	w, _ := clientTestWorld(t)
	withCheckedPlayer(t, w, clienttest.New(), nil, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		p.SetVelocity(mgl64.Vec3{0, 0, 12})
		if p.anticheat() != nil {
			t.Fatal("a player without AntiCheat has an anti-cheat state")
		}
		if got := p.ClientMove(client.Move{Position: p.Position().Add(mgl64.Vec3{0, 0, 12})}); got != client.MoveRefused {
			t.Fatalf("12 blocks: %v, want refused", got)
		}
	})
}

// TestImpulseHoldsSpeedAndEffects checks that the speed and the effects
// that fell are held over the round trip, as the client learns of the fall
// a round trip later.
func TestImpulseHoldsSpeedAndEffects(t *testing.T) {
	w, _ := clientTestWorld(t)
	withCheckedPlayer(t, w, clienttest.New(), anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		ac := p.anticheat()
		p.SetSpeed(0.2)
		p.AddEffect(effect.New(effect.JumpBoost, 2, time.Minute))
		if p.heldSpeed(ac) != 0.2 || p.heldEffect(ac, &ac.jumpBoost, effect.JumpBoost) != 2 {
			t.Fatal("the speed or Jump Boost is not held as set")
		}
		p.SetSpeed(0.1)
		p.RemoveEffect(effect.JumpBoost)
		for i := int64(0); i < 5; i++ {
			p.Tick(tx, i)
		}
		if p.heldSpeed(ac) != 0.2 || p.heldEffect(ac, &ac.jumpBoost, effect.JumpBoost) != 2 {
			t.Fatal("the speed or Jump Boost is not held 5 ticks after they fell")
		}
		for i := int64(5); i < 12; i++ {
			p.Tick(tx, i)
		}
		if got := p.heldSpeed(ac); got != 0.1 {
			t.Fatalf("held speed 12 ticks after it fell: %v, want 0.1", got)
		}
		if got := p.heldEffect(ac, &ac.jumpBoost, effect.JumpBoost); got != 0 {
			t.Fatalf("held Jump Boost 12 ticks after it ended: %v, want 0", got)
		}
	})
}
