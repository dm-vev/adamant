package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// buildSulfurSpring walls in water sources of the depth passed over potent
// sulfur at y 63, with the block passed below it, leaving the gas source
// above the water open.
func buildSulfurSpring(tx *world.Tx, below world.Block, depth int) {
	tx.SetBlock(cube.Pos{0, 62, 0}, below, nil)
	for y := 63; y <= 64+depth; y++ {
		for _, f := range cube.HorizontalFaces() {
			tx.SetBlock(cube.Pos{0, y, 0}.Side(f), block.Stone{}, nil)
		}
	}
	tx.SetBlock(cube.Pos{0, 63, 0}, block.NewPotentSulfur(), nil)
	for y := 64; y < 64+depth; y++ {
		tx.SetBlock(cube.Pos{0, y, 0}, block.Water{Depth: 8, Still: true}, nil)
	}
}

func TestNoxiousGasGivesNauseaToEntitiesSurfacingAboveSulfur(t *testing.T) {
	w := newDeathTestWorld(t)
	var near, far *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		buildSulfurSpring(tx, block.Stone{}, 1)
		// Standing on the sulfur in one block of water, with the eyes in
		// the gas above it.
		near = tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Position: mgl64.Vec3{0.5, 64, 0.5}})).H()
		tx.SetBlock(cube.Pos{4, 63, 0}, block.Stone{}, nil)
		far = tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Position: mgl64.Vec3{4.5, 64, 0.5}})).H()
	})
	for range 20 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if tx.Block(cube.Pos{0, 63, 0}).(block.PotentSulfur).State != block.PotentSulfurWet {
			t.Fatalf("sulfur under water is %#v", tx.Block(cube.Pos{0, 63, 0}))
		}
		n, _ := near.Entity(tx)
		got, ok := n.(*player.Player).Effect(effect.Nausea)
		if !ok || !got.Ambient() || got.Duration() <= 0 {
			t.Fatalf("surfacing player has nausea %v (%v)", got, ok)
		}
		f, _ := far.Entity(tx)
		if _, ok := f.(*player.Player).Effect(effect.Nausea); ok {
			t.Fatal("player away from the spring got nausea")
		}
	})
}

// highestItemY drops an item into a two block deep spring over the block
// passed and returns the highest it gets within two seconds.
func highestItemY(t *testing.T, below world.Block) float64 {
	t.Helper()
	w := newDeathTestWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		buildSulfurSpring(tx, below, 2)
		h = tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64.2, 0.5}}, item.NewStack(item.Stick{}, 1))).H()
	})
	highest := 0.0
	for range 40 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := h.Entity(tx); ok {
				highest = max(highest, e.Position()[1])
			}
		})
	}
	return highest
}

func TestEruptingGeyserLaunchesEntitiesUpward(t *testing.T) {
	calm := highestItemY(t, block.Stone{})
	geyser := highestItemY(t, block.Lava{Depth: 8, Still: true})
	// Two water blocks give a geyser 12 blocks high, pushing up to 0.5
	// blocks per tick.
	if geyser < calm+3 {
		t.Fatalf("item over a geyser rose to %.2f, over calm sulfur to %.2f", geyser, calm)
	}
}
