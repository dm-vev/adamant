package entity_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

func TestMobEffectsSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	open := func() (*world.World, *mcdb.DB) {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		w := world.Config{Provider: db, Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
		return w, db
	}
	w, _ := open()
	var cow, trader *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Stone{}, nil)
		cow = spawnAnimal(t, tx, "minecraft:cow", .5, nil)
		e, _ := cow.Entity(tx)
		e.(entity.Living).AddEffect(effect.New(effect.Speed, 2, 30*time.Second))
		trader = addTrader(tx, mgl64.Vec3{.5, 64, 2.5}).H()
		e, _ = trader.Entity(tx)
		e.(entity.Living).AddEffect(effect.NewInfinite(effect.Invisibility, 1))
	})
	for range 20 {
		w.AdvanceTick()
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	w, _ = open()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(cube.Pos{0, 63, 0})
		found := map[string][]effect.Effect{}
		for e := range tx.Entities() {
			if l, ok := e.(entity.Living); ok {
				found[e.H().Type().EncodeEntity()] = l.Effects()
			}
		}
		speed := found["minecraft:cow"]
		if len(speed) != 1 || speed[0].Type() != effect.Speed || speed[0].Level() != 2 {
			t.Fatalf("cow came back with %v, want Speed II", speed)
		}
		if d := speed[0].Duration(); d > 29*time.Second || d < 28*time.Second {
			t.Errorf("cow's Speed has %v left, want the 29 seconds left at the save", d)
		}
		invisible := found["minecraft:wandering_trader"]
		if len(invisible) != 1 || invisible[0].Type() != effect.Invisibility || !invisible[0].Infinite() {
			t.Fatalf("trader came back with %v, want infinite invisibility", invisible)
		}
	})
}
