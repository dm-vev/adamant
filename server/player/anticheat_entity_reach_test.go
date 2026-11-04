package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// withAttacker runs f with a checked player at the origin of a world,
// looking east, its client handling input, and a cow east at x on the same
// height.
func withAttacker(t *testing.T, conf *anticheat.Config, x float64, f func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder)) {
	w, _ := clientTestWorld(t)
	r := clienttest.New()
	withCheckedPlayer(t, w, r, conf, mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		h := &violationRecorder{}
		p.Handle(h)
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{x, 100, 0.5}}))
		p.data.Rot = cube.Rotation{-90, 0}
		r.SetHandlingInput(true)
		f(tx, p, cow, h)
	})
}

// TestEntityReachIsJavas checks Java's reach, from the eyes to the box of
// the target within 3 blocks and a buffer of 3: a cow 5.9 blocks to its box
// is hit, one 7.3 blocks is not, and the miss is a violation.
func TestEntityReachIsJavas(t *testing.T) {
	// The box of a cow is 0.9 wide: its position 0.45 beyond its box.
	withAttacker(t, anticheat.Default(), 0.5+5.9+0.45, func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder) {
		if !p.AttackEntity(cow) || len(h.violations) != 0 {
			t.Fatalf("a cow 5.9 blocks away: missed, or %+v", h.violations)
		}
	})
	withAttacker(t, anticheat.Default(), 8.25, func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder) {
		if p.AttackEntity(cow) || h.count(anticheat.EntityReach) != 1 {
			t.Fatalf("a cow 7.3 blocks to its box: hit, or %+v", h.violations)
		}
		if p.UseItemOnEntity(cow) || h.count(anticheat.EntityReach) != 2 {
			t.Fatalf("an interaction 7.3 blocks to the box: done, or %+v", h.violations)
		}
		p.SetGameMode(world.GameModeCreative)
		if !p.AttackEntity(cow) {
			t.Fatal("in creative, a cow 7.3 blocks to its box was missed: the reach is 8")
		}
	})
}

// TestEntityReachByPosition checks that Measure "position" keeps the reach
// of before: 8 blocks to the position of the target, as the old rule had.
func TestEntityReachByPosition(t *testing.T) {
	conf := anticheat.Default()
	conf.EntityReach = anticheat.ReachSettings{Measure: "position", Survival: 8, Creative: 14}
	withAttacker(t, conf, 8.25, func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder) {
		if !p.AttackEntity(cow) || len(h.violations) != 0 {
			t.Fatalf("by position, 7.9 blocks away: missed, or %+v", h.violations)
		}
	})
}

// TestEntityReachOffAndPlugins checks that with EntityReach off, and for a
// plugin's attack, the reach is that of before.
func TestEntityReachOffAndPlugins(t *testing.T) {
	off := withPolicy(anticheat.EntityReach, func(p *anticheat.Policy) { p.Enabled = false })
	withAttacker(t, off, 8.25, func(tx *world.Tx, p *Player, cow world.Entity, h *violationRecorder) {
		if !p.AttackEntity(cow) || len(h.violations) != 0 {
			t.Fatal("with EntityReach off, the reach is not 8 to the position")
		}
	})
	w, _ := clientTestWorld(t)
	withCheckedPlayer(t, w, clienttest.New(), anticheat.Default(), mgl64.Vec3{0.5, 100, 0.5}, func(tx *world.Tx, p *Player) {
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{8.25, 100, 0.5}}))
		if !p.AttackEntity(cow) {
			t.Fatal("a plugin's attack 7.9 blocks away missed")
		}
	})
}
