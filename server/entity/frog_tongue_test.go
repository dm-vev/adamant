package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestFrogEatsSmallMagmaCubeForFroglight(t *testing.T) {
	cases := []struct {
		name  string
		size  int
		eaten bool
	}{
		{"size 1 is eaten", 1, true},
		{"size 2 is too big", 2, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newDeathTestWorld(t)
			var cubeHandle *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				// A walled pen, higher than a magma cube hops, keeps it
				// within reach.
				for x := -4; x <= 4; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
						if x == -4 || x == 4 || z == -4 || z == 4 {
							for y := 64; y <= 67; y++ {
								tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
							}
						}
					}
				}
				tx.AddEntity(entity.NewFrog(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
				// A size 2 cube is over a block wide, so it starts clear of
				// the wall it would slip through.
				cubeHandle = tx.AddEntity(entity.NewMagmaCubeWithSize(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, 0.5}}, c.size)).H()
			})
			// A croaking or leaping frog, or one that found the hopping cube out of
			// reach, leaves it alone for a few seconds.
			for range 600 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				_, alive := cubeHandle.Entity(tx)
				if alive == c.eaten {
					t.Fatalf("magma cube of size %d alive=%v after 600 ticks", c.size, alive)
				}
				froglights := 0
				_, items := deathDrops(tx)
				for _, it := range items {
					if name, _ := it.Item().EncodeItem(); name == "minecraft:ochre_froglight" {
						froglights += it.Count()
					}
				}
				if want := map[bool]int{true: 1, false: 0}[c.eaten]; froglights != want {
					t.Fatalf("temperate frog left %d ochre froglights, want %d", froglights, want)
				}
			})
		})
	}
}
