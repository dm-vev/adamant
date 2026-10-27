package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// TestLowestPlayerUpdatesMapFrames checks the living player with the lowest
// UUID updates the map frames, whatever other entities with an inventory
// and held items, as villagers, are about.
func TestLowestPlayerUpdatesMapFrames(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	id := func(b byte) uuid.UUID { return uuid.UUID{b, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15} }
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(entity.NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{2, 64, 2}, ID: id(1)}))
		low := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}, ID: id(5)}.New(Type, Config{Name: "Low", GameMode: world.GameModeCreative})).(*Player)
		high := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{4, 64, 0}, ID: id(9)}.New(Type, Config{Name: "High", GameMode: world.GameModeCreative})).(*Player)
		if !low.updatesMapFrames() {
			t.Error("player with the lowest UUID does not update the map frames beside a villager with a lower one")
		}
		if high.updatesMapFrames() {
			t.Error("player with a higher UUID than another updates the map frames")
		}
	})
}
