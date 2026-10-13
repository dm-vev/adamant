package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// newDropTestWorld returns a world in which broken blocks drop test items.
func newDropTestWorld(t *testing.T) *world.World {
	typ := dispenserTestEntityType{}
	entities := world.EntityRegistryConfig{Item: func(opts world.EntitySpawnOpts, _ any) *world.EntityHandle {
		return opts.New(typ, dispenserTestEntityConfig{})
	}}.New([]world.EntityType{typ})
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entities}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func TestBushesPlaceOnlyOnVegetationSoil(t *testing.T) {
	plants := []world.Block{Bush{}, FireflyBush{}, RedShrub{}}
	soils := []struct {
		soil world.Block
		ok   bool
	}{{Grass{}, true}, {Dirt{}, true}, {Podzol{}, true}, {MossBlock{}, true}, {Mud{}, true}, {Stone{}, false}, {Sand{}, false}}
	for _, plant := range plants {
		for _, s := range soils {
			w := newMultifaceTestWorld(t)
			var got world.Block
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{0, 0, 0}, s.soil, nil)
				ctx := &item.UseContext{}
				plant.(item.UsableOnBlock).UseOnBlock(cube.Pos{0, 0, 0}, cube.FaceUp, mgl64.Vec3{}, tx, &pistonTestUser{}, ctx)
				got = tx.Block(cube.Pos{0, 1, 0})
			})
			if placed := got == plant; placed != s.ok {
				t.Errorf("%T on %T: placed %v, want %v", plant, s.soil, placed, s.ok)
			}
		}
	}
}

func TestRedShrubBreaksWithoutSoil(t *testing.T) {
	w := newDropTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 0, 0}, Grass{}, nil)
		tx.SetBlock(cube.Pos{0, 1, 0}, RedShrub{}, nil)
		tx.SetBlock(cube.Pos{0, 0, 0}, Stone{}, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if b := tx.Block(cube.Pos{0, 1, 0}); b != (Air{}) {
			t.Fatalf("shrub on stone stayed as %#v", b)
		}
	})
}

func TestBushBoneMealSpreadsToFreeNeighbourOnSoil(t *testing.T) {
	for _, plant := range []world.Block{Bush{}, FireflyBush{}, RedShrub{}} {
		w := newMultifaceTestWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			centre := cube.Pos{0, 1, 0}
			tx.SetBlock(centre.Side(cube.FaceDown), Grass{}, nil)
			tx.SetBlock(centre, plant, nil)
			// Only the east neighbour is free and above soil: the west one
			// is blocked, and the others float over air.
			tx.SetBlock(cube.Pos{1, 0, 0}, Grass{}, nil)
			tx.SetBlock(cube.Pos{-1, 0, 0}, Grass{}, nil)
			tx.SetBlock(cube.Pos{-1, 1, 0}, Stone{}, nil)

			bm := plant.(item.BoneMealAffected)
			if got := bm.BoneMeal(centre, tx); got == item.BoneMealResultNone {
				t.Fatalf("%T: bone meal had no effect", plant)
			}
			if b := tx.Block(cube.Pos{1, 1, 0}); b != plant {
				t.Fatalf("%T: east neighbour is %#v, want the plant", plant, b)
			}
			if got := bm.BoneMeal(centre, tx); got != item.BoneMealResultNone {
				t.Fatalf("%T: bone meal spread without a free neighbour on soil", plant)
			}
		})
	}
}
