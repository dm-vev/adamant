package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMobsWalkOnFrostWalker checks a mob wearing Frost Walker boots freezes
// the still water around it into frosted ice as it moves into a block, in
// the disk of 3 blocks of level I, and does not burn on magma.
func TestMobsWalkOnFrostWalker(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Depth: 8, Still: true}, nil)
			}
		}
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		zombie := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).(*Zombie)
		zombie.state().feet = item.NewStack(item.Boots{Tier: item.ArmourTierIron{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.FrostWalker, 1))
		FrostWalk(tx, zombie, zombie.Position())

		for _, c := range []struct {
			pos    cube.Pos
			frozen bool
		}{{cube.Pos{2, 63, 0}, true}, {cube.Pos{2, 63, 2}, true}, {cube.Pos{2, 63, 3}, false}, {cube.Pos{1, 63, 2}, true}, {cube.Pos{3, 63, 0}, false}} {
			_, frozen := tx.Block(c.pos).(block.FrostedIce)
			if frozen != c.frozen {
				t.Fatalf("water at %v frozen = %v, want %v for a disk of 3 around the zombie", c.pos, frozen, c.frozen)
			}
		}

		health := zombie.Health()
		block.Magma{}.EntityStepOn(cube.Pos{0, 63, 0}, tx, zombie)
		if zombie.Health() != health {
			t.Fatalf("zombie with Frost Walker took magma damage")
		}
	})
}
