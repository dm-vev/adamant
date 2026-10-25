package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// naturalSpawnWorld returns a world with a stone floor 112 blocks wide at
// y=63 around a survival player at the origin.
func naturalSpawnWorld(t *testing.T, time int) (*world.World, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(world.DifficultyNormal)
	w.SetTime(time)
	w.StopTime()
	var p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -56; x < 56; x++ {
			for z := -56; z < 56; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		p = addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5}).H()
	})
	return w, p
}

// spawnRecorder records where entities spawn.
type spawnRecorder struct {
	world.NopHandler
	spawned map[string][]mgl64.Vec3
}

func (r *spawnRecorder) HandleEntitySpawn(_ *world.Tx, e world.Entity) {
	if _, living := e.(entity.Living); !living {
		// Arrows the skeletons shoot spawn too.
		return
	}
	r.spawned[e.H().Type().EncodeEntity()] = append(r.spawned[e.H().Type().EncodeEntity()], e.Position())
}

// TestMonstersSpawnInTheDark checks NaturalSpawner: at night monsters spawn on
// dark ground around a player, never within 24 blocks of it, up to the
// monster cap.
func TestMonstersSpawnInTheDark(t *testing.T) {
	w, _ := naturalSpawnWorld(t, 18000)
	rec := &spawnRecorder{spawned: map[string][]mgl64.Vec3{}}
	w.Handle(rec)
	for range 400 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		for id, positions := range rec.spawned {
			for _, pos := range positions {
				if d := pos.Sub(mgl64.Vec3{.5, 64, .5}); d.Len() <= 24 {
					t.Errorf("%s spawned %v from the player, within 24 blocks", id, d)
				}
			}
		}
		monsters := 0
		for e := range tx.Entities() {
			if _, ok := e.(entity.Living); ok && e.H().Type().EncodeEntity() != "minecraft:player" {
				monsters++
			}
		}
		if monsters == 0 {
			t.Fatal("no monster spawned on dark ground around a player in 20 seconds of night")
		}
		// The cap is checked before each group, which may add up to four,
		// and chicken jockeys bring their chickens.
		if monsters > 80 {
			t.Fatalf("%d mobs spawned around one player, well above the monster cap of 70", monsters)
		}
	})
}

// TestMonstersDoNotSpawnInDaylight checks Monster.isDarkEnoughToSpawn: by day
// no monster spawns on ground open to the sky.
func TestMonstersDoNotSpawnInDaylight(t *testing.T) {
	w, _ := naturalSpawnWorld(t, 6000)
	for range 400 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		for e := range tx.Entities() {
			// Animals do not spawn on stone either.
			if _, ok := e.(entity.Living); ok && e.H().Type().EncodeEntity() != "minecraft:player" {
				t.Fatalf("%s spawned at %v in daylight", e.H().Type().EncodeEntity(), e.Position())
			}
		}
	})
}
