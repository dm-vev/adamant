package player

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMoveTooFarIsRefused moves a client farther than a player goes in a
// tick, as far as the world goes: the move is refused, and the client put
// back where the server has the player, rather than taking the player
// there, which froze its world as it looked for ground along the whole way.
func TestMoveTooFarIsRefused(t *testing.T) {
	w, _ := clientTestWorld(t)
	for name, to := range map[string]mgl64.Vec3{
		"far below": {0.5, -2e7, 0.5},
		"far out":   {3e7, 100, 0.5},
		"11 blocks": {11.5, 100, 0.5},
	} {
		r := clienttest.New()
		withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
			start := p.Position()
			if got := p.ClientMove(client.Move{Position: to, Tick: 9}); got != client.MoveRefused {
				t.Errorf("%s: %v, want MoveRefused", name, got)
			}
			if p.Position() != start {
				t.Errorf("%s: the player moved to %v", name, p.Position())
			}
			calls := r.CallsTo("CorrectMove")
			if len(calls) != 1 || calls[0].Args[1] != start || calls[0].Args[2] != (mgl64.Vec3{}) || calls[0].Args[3] != uint64(9) {
				t.Errorf("%s: corrections %v, want one to %v at rest for tick 9", name, calls, start)
			}
		})
	}
	r := clienttest.New()
	withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
		start := p.Position()
		if got := p.ClientMove(client.Move{Position: start.Add(mgl64.Vec3{5, 0, 0})}); got != client.MoveApplied {
			t.Fatalf("a move of 5 blocks: %v", got)
		}
		if math.Abs(p.Position()[0]-start[0]-5) > 1e-9 || len(r.CallsTo("CorrectMove")) != 0 {
			t.Fatalf("a move of 5 blocks moved the player to %v, corrected %v", p.Position(), r.CallsTo("CorrectMove"))
		}
		// Gliding, Java allows 17 blocks.
		p.gliding = true
		if got := p.ClientMove(client.Move{Position: p.Position().Add(mgl64.Vec3{15, 0, 0})}); got != client.MoveApplied {
			t.Fatalf("a glide of 15 blocks: %v", got)
		}
	})
}

// TestClientMoveIgnores checks the moves the game has nothing to apply of:
// one awaiting a teleport, one that changes nothing, and one of a player
// that rides, which only turns it.
func TestClientMoveIgnores(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, clienttest.New(), func(tx *world.Tx, p *Player) {
		start := p.Position()
		if got := p.ClientMove(client.Move{Position: start.Add(mgl64.Vec3{1, 0, 0}), AwaitingTeleport: true}); got != client.MoveIgnored || p.Position() != start {
			t.Errorf("a move awaiting a teleport: %v, at %v", got, p.Position())
		}
		if got := p.ClientMove(client.Move{Position: start, Rotation: p.Rotation()}); got != client.MoveIgnored {
			t.Errorf("an unchanged move: %v", got)
		}
		got := p.ClientMove(client.Move{Position: start.Add(mgl64.Vec3{1, 0, 0}), Rotation: cube.Rotation{90, 10}, Riding: true})
		if got != client.MoveApplied || p.Position() != start || p.Rotation() != (cube.Rotation{90, 10}) {
			t.Errorf("a move riding: %v, at %v looking %v; want turned only", got, p.Position(), p.Rotation())
		}
	})
}

// TestClientFlightIsRefused checks that a client that asks to fly in a game
// mode without flight, or to stop in one that flies for good, is refused
// and shown its abilities again, as the session refused it before; a
// plugin's call is not.
func TestClientFlightIsRefused(t *testing.T) {
	w, _ := clientTestWorld(t)
	for _, tc := range []struct {
		name               string
		mode               world.GameMode
		startFlying, stops bool
	}{
		{"spectator", world.GameModeSpectator, true, false},
		{"survival", world.GameModeSurvival, false, true},
		{"creative", world.GameModeCreative, true, true},
	} {
		r := clienttest.New()
		withClientPlayer(t, w, r, func(tx *world.Tx, p *Player) {
			p.SetGameMode(tc.mode)
			r.Reset()
			r.SetHandlingInput(true)
			p.StartFlying()
			if p.Flying() != tc.startFlying {
				t.Errorf("%s: flying %v after StartFlying, want %v", tc.name, p.Flying(), tc.startFlying)
			}
			if refused := len(r.CallsTo("SendAbilities")) == 1; refused != !tc.mode.AllowsFlying() {
				t.Errorf("%s: abilities resent %v, want %v", tc.name, refused, !tc.mode.AllowsFlying())
			}
			r.Reset()
			p.flying = true
			p.StopFlying()
			if p.Flying() == tc.stops {
				t.Errorf("%s: flying %v after StopFlying, want %v", tc.name, p.Flying(), !tc.stops)
			}
			if refused := len(r.CallsTo("SendAbilities")) == 1; refused != !tc.mode.HasCollision() {
				t.Errorf("%s: abilities resent on stop %v, want %v", tc.name, refused, !tc.mode.HasCollision())
			}
			// A plugin asks without the client: nothing is resent.
			r.SetHandlingInput(false)
			r.Reset()
			p.flying = false
			if tc.mode == world.GameModeSurvival {
				p.StartFlying()
				if len(r.CallsTo("SendAbilities")) != 0 || p.Flying() {
					t.Errorf("%s: a plugin's StartFlying resent the abilities or flew", tc.name)
				}
			}
		})
	}
}
