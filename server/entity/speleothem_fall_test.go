package entity_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// hurtRecorder records the damage a player takes.
type hurtRecorder struct {
	player.NopHandler
	damage  []float64
	sources []world.DamageSource
}

func (h *hurtRecorder) HandleHurt(_ *player.Context, dmg *float64, _ bool, _ *time.Duration, src world.DamageSource) {
	h.damage, h.sources = append(h.damage, *dmg), append(h.sources, src)
}

// droppedDripstone counts pointed dripstone in item entities.
func droppedDripstone(tx *world.Tx) (n int) {
	_, items := deathDrops(tx)
	for _, it := range items {
		if _, ok := it.Item().(block.PointedDripstone); ok {
			n += it.Count()
		}
	}
	return n
}

func TestFallingStalactiteHurtsByColumnLengthAndShatters(t *testing.T) {
	w := newDeathTestWorld(t)
	var h *world.EntityHandle
	hurt := &hurtRecorder{}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 73, 0}, block.Stone{}, nil)
		// A seven block stalactite whose tip hangs two blocks above the floor.
		for y := 72; y >= 66; y-- {
			tx.SetBlock(cube.Pos{0, y, 0}, block.PointedDripstone{Hanging: true, Thickness: block.SpeleothemMiddle}, nil)
		}
		tx.SetBlock(cube.Pos{0, 66, 0}, block.PointedDripstone{Hanging: true, Thickness: block.SpeleothemTip}, nil)
		p := tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{0.5, 64, 0.5}, GameMode: world.GameModeSurvival})).(*player.Player)
		p.Handle(hurt)
		h = p.H()
	})
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 73, 0}, nil, nil) })
	for range 80 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		for y := 64; y <= 72; y++ {
			if b := tx.Block(cube.Pos{0, y, 0}); b != (block.Air{}) {
				t.Fatalf("y=%d still holds %#v", y, b)
			}
		}
		e, _ := h.Entity(tx)
		p := e.(*player.Player)
		// The player standing below picks up some of the shards.
		n := droppedDripstone(tx)
		for _, it := range p.Inventory().Items() {
			if _, ok := it.Item().(block.PointedDripstone); ok {
				n += it.Count()
			}
		}
		if n != 7 {
			t.Fatalf("shattered column dropped %d pointed dripstone, want 7", n)
		}
		// Only the tip hurts: it falls two blocks, ceil(2-1) = 1 or, with
		// float error, 2, times the seven block column length.
		if len(hurt.damage) != 1 || hurt.damage[0] != 7 && hurt.damage[0] != 14 {
			t.Fatalf("falling stalactite dealt %v, want one multiple of the column length 7", hurt.damage)
		}
		if src, ok := hurt.sources[0].(block.DamageSource); !ok || src.Block != (block.PointedDripstone{Hanging: true}) {
			t.Fatalf("damage source %#v, want the falling tip", hurt.sources[0])
		}
	})
}

func TestUnsupportedStalagmiteBreaksIntoItem(t *testing.T) {
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 64, 0}, block.PointedDripstone{Thickness: block.SpeleothemTip}, nil)
	})
	<-w.Exec(func(tx *world.Tx) { tx.SetBlock(cube.Pos{0, 63, 0}, nil, nil) })
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if b := tx.Block(cube.Pos{0, 64, 0}); b != (block.Air{}) {
			t.Fatalf("unsupported stalagmite left %#v", b)
		}
		if n := droppedDripstone(tx); n != 1 {
			t.Fatalf("broken stalagmite dropped %d pointed dripstone, want 1", n)
		}
	})
}
