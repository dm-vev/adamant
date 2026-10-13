package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestAnimalsPickGrassToStrollTo checks RandomPos with
// Animal.getWalkTargetValue: of the ten spots an animal considers it picks
// one on grass, so next to a field of grass it nearly always heads there,
// where picking spots at random would lead it there a third of the time.
func TestAnimalsPickGrassToStrollTo(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -20; x <= 20; x++ {
			for z := -20; z <= 20; z++ {
				for y := 56; y < 64; y++ {
					var b world.Block = block.Stone{}
					if y == 63 && x >= 0 {
						b = block.Grass{}
					}
					tx.SetBlock(cube.Pos{x, y, z}, b, nil)
				}
			}
		}
		cow := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{-3.5, 64, .5}})).(*Cow)
		m := newRandomPosMob(cow.Ent, tx)
		grass := 0
		const tries = 200
		for range tries {
			if pos, ok := m.randomPos(10, 7); ok && pos[0] >= 0 {
				grass++
			}
		}
		if grass < tries*3/4 {
			t.Fatalf("cow picked a spot on grass %d out of %d times, want it to prefer grass", grass, tries)
		}
	})
}
