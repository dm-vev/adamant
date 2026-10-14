package entity

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

// TestZombifiedPiglinSurvivesMCDBReopen checks a zombified piglin is decoded
// again when its chunk loads, instead of being dropped as an unknown actor.
func TestZombifiedPiglinSurvivesMCDBReopen(t *testing.T) {
	dir := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
	}
	pos := mgl64.Vec3{.5, 65, .5}
	w := open()
	var id int64
	if err := w.Do(func(tx *world.Tx) {
		h := DefaultRegistry.Config().SpawnMob("minecraft:zombified_piglin", world.EntitySpawnOpts{Position: pos})
		id = tx.AddEntity(h).H().UniqueID()
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Save()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = open()
	defer func() { _ = w.Close() }()
	found := false
	if err := w.Do(func(tx *world.Tx) {
		tx.Block(cube.PosFromVec3(pos))
		for e := range tx.Entities() {
			if e.H().UniqueID() == id {
				_, found = e.(*ZombifiedPiglin)
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("the zombified piglin was lost when its chunk loaded again")
	}
}
