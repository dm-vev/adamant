package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestSpawnNoCollisionAtTheEdgeOfAChunk checks Level.noCollision for a mob
// that NaturalSpawner.spawnMobsForChunkGeneration puts in the outermost row of
// a chunk. Java searches a block further than the box, and BlockCollisions
// skips the cells of chunks that are not loaded; a chunk that has just been
// generated may have none of its neighbours, so a mob at its edge on open
// ground must not find them solid.
func TestSpawnNoCollisionAtTheEdgeOfAChunk(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	pig, ok := DefaultRegistry.Lookup("minecraft:pig")
	if !ok {
		t.Fatal("no pig type")
	}
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(cube.Pos{0, 100, 0})
		for _, test := range []struct{ edge, beyond mgl64.Vec3 }{
			{mgl64.Vec3{8, 100, 15}, mgl64.Vec3{8, 100, 16.5}},
			{mgl64.Vec3{8, 100, 0}, mgl64.Vec3{8, 100, -0.5}},
			{mgl64.Vec3{15, 100, 8}, mgl64.Vec3{16.5, 100, 8}},
			{mgl64.Vec3{0, 100, 8}, mgl64.Vec3{-0.5, 100, 8}},
		} {
			if _, loaded := tx.BlockLoaded(cube.PosFromVec3(test.beyond)); loaded {
				t.Fatalf("the chunk of %v is loaded: the test needs it unloaded", test.beyond)
			}
			if !spawnNoCollision(tx, pig.BBox(nil).Translate(test.edge)) {
				t.Errorf("a pig at %v in empty air collides with the unloaded chunk beside it", test.edge)
			}
		}
	})
}
