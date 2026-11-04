package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// sweepCase sets up a player facing east (+x) at 0.5, 100, 0.5 with held in
// its main hand, a cow two blocks ahead to hit, and three more: one beside
// it, whose box meets the target's grown by 1 sideways; one beyond it, whose
// box meets it too but which is more than 3 blocks from the player; and one
// 2.4 blocks to the side of the player, outside the grown box. charge ticks
// pass after it takes the item, then edit changes the player and it hits
// the cow. It returns the health of the three others.
func sweepCase(t *testing.T, held item.Stack, charge int, edit func(p *Player)) (beside, beyond, aside float64) {
	t.Helper()
	withAttacker(t, anticheat.Default(), 2.5, func(tx *world.Tx, p *Player, cow world.Entity, _ *violationRecorder) {
		cows := make([]entity.Living, 0, 3)
		for _, pos := range []mgl64.Vec3{{2.5, 100, 1.4}, {3.7, 100, 0.5}, {0.5, 100, 2.9}} {
			cows = append(cows, tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos})).(entity.Living))
		}
		p.SetHeldItems(held, item.Stack{})
		for range charge {
			p.tickAttackStrength()
		}
		p.onGround = true
		if edit != nil {
			edit(p)
		}
		if !p.AttackEntity(cow) {
			t.Fatal("the cow was not hit")
		}
		beside, beyond, aside = cows[0].Health(), cows[1].Health(), cows[2].Health()
	})
	return beside, beyond, aside
}

// TestSweepAttack checks Java's sweep attack (Player.isSweepAttack and
// doSweepAttack): a hit with a sword charged to full strength, on the
// ground and not sprinting, also hurts the living entity beside its target,
// by 1 times the strength without Sweeping Edge, and none beyond the grown
// box of the target or 3 blocks from the player. A hit that is not charged, made sprinting, in the
// air or with an axe does not sweep.
func TestSweepAttack(t *testing.T) {
	sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
	// The delay of a sword is 20/1.6 = 12.5 ticks: strength (11+0.5)/12.5
	// = 0.92 is full, (10+0.5)/12.5 = 0.84 is not. The first tick of a
	// newly held item counts none.
	// The cow beside takes 1 times the strength.
	if beside, beyond, aside := sweepCase(t, sword, 12, nil); !mgl64.FloatEqual(beside, 10-0.92) || beyond != 10 || aside != 10 {
		t.Errorf("swept with a charged sword: health %v, %v and %v, want 9.08, 10 and 10", beside, beyond, aside)
	}
	for name, c := range map[string]struct {
		held   item.Stack
		charge int
		edit   func(p *Player)
	}{
		"a sword charged to 0.84": {sword, 11, nil},
		"sprinting":               {sword, 12, func(p *Player) { p.sprinting = true }},
		"in the air":              {sword, 12, func(p *Player) { p.onGround = false }},
		"an axe":                  {item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1), 12, nil},
	} {
		if beside, _, _ := sweepCase(t, c.held, c.charge, c.edit); beside != 10 {
			t.Errorf("%s: the cow beside has health %v, want 10", name, beside)
		}
	}
}

// TestAttackStrengthRestarts checks that the count of the attack strength
// restarts when the main hand comes to hold another item, and after an
// attack or a punch at the air, but not when only the durability of the
// held item changes (ItemStack.isSameItem).
func TestAttackStrengthRestarts(t *testing.T) {
	w, _ := clientTestWorld(t)
	withClientPlayer(t, w, nil, func(tx *world.Tx, p *Player) {
		sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
		p.SetHeldItems(sword, item.Stack{})
		for range 5 {
			p.tickAttackStrength()
		}
		p.SetHeldItems(sword.Damage(3), item.Stack{})
		p.tickAttackStrength()
		if p.attackStrengthTicks != 5 {
			t.Fatalf("%d ticks after the sword was damaged, want 5", p.attackStrengthTicks)
		}
		p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1), item.Stack{})
		p.tickAttackStrength()
		if p.attackStrengthTicks != 0 {
			t.Fatalf("%d ticks after another sword was taken, want 0", p.attackStrengthTicks)
		}
		p.tickAttackStrength()
		p.PunchAir()
		if p.attackStrengthTicks != 0 {
			t.Fatalf("%d ticks after a punch at the air, want 0", p.attackStrengthTicks)
		}
	})
}
