package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBrainAnimalsPreferGrass checks that goats, armadillos and frogs, which
// keep Animal.getWalkTargetValue, like grass best to stroll to.
func TestBrainAnimalsPreferGrass(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Grass{}, nil)
		tx.SetBlock(cube.Pos{2, 63, 0}, block.Stone{}, nil)
		for _, h := range []*world.EntityHandle{
			NewGoat(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, 4.5}}),
			NewArmadillo(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, 4.5}}),
			NewFrog(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, 4.5}}),
		} {
			e := tx.AddEntity(h)
			m, ok := mobOf(e)
			if !ok {
				t.Fatalf("%v runs on no MobAI", e.H().Type().EncodeEntity())
			}
			if grass, stone := m.walkTargetValue(cube.Pos{0, 64, 0}), m.walkTargetValue(cube.Pos{2, 64, 0}); grass != 10 || stone >= grass {
				t.Fatalf("%v values grass at %v and stone at %v, want 10 and less", e.H().Type().EncodeEntity(), grass, stone)
			}
		}
	})
}
