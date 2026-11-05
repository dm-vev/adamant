package player

import (
	"errors"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

// The tests of a server whose chunks are not all there: not loaded yet, or
// the empty stand-in of a failed load. A client that stands or walks on them
// is no cheat.

// failingProvider fails to load the columns it is told to.
type failingProvider struct {
	world.NopProvider
	fail map[world.ChunkPos]bool
}

func (f failingProvider) LoadColumn(pos world.ChunkPos, dim world.Dimension) (*chunk.Column, error) {
	if f.fail[pos] {
		return nil, errors.New("disk on fire")
	}
	return f.NopProvider.LoadColumn(pos, dim)
}

// edgeWorld returns a synchronous world that has loaded the chunks -5 to 5
// around the origin, with a floor of stone at floorY - 1 for x from 40 to
// 140, and whose chunk at (7, 0) fails to load, so that the world serves an
// empty stand-in.
func edgeWorld(t testing.TB) *world.World {
	t.Helper()
	provider := failingProvider{fail: map[world.ChunkPos]bool{{7, 0}: true}}
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry, Provider: provider}.New()
	viewer := clienttest.New()
	loader := world.NewLoader(5, w, viewer)
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0.5, floorY, 0.5})
		loader.Load(tx, 121)
		for x := 40; x < 96; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, floorY - 1, z}, block.Stone{}, nil)
			}
		}
	})
	return w
}

// chunkTracked reports whether w tracks the chunk at pos.
func chunkTracked(w *world.World, pos world.ChunkPos) (tracked bool) {
	<-w.Exec(func(tx *world.Tx) { tracked, _ = tx.ChunkState(pos) })
	return tracked
}

// TestMovementChecksDoNotLoadChunks checks that a client walking to the edge
// of the loaded chunks and a block beyond, which the server has not loaded,
// does not make the checks load or generate them: they read the blocks of
// the world as it is. A load of a chunk on the tick of the world waits for
// its generation.
func TestMovementChecksDoNotLoadChunks(t *testing.T) {
	w := edgeWorld(t)
	// The chunk 6 begins at x = 96: the client sprints from 94 to 102.
	start := mgl64.Vec3{94, floorY, 0.5}
	tr := acTrace{name: "to the edge of the loaded chunks", start: start, bedrock: true,
		positions: func(*world.Tx) []mgl64.Vec3 {
			// Sprinting from rest: the ground friction keeps 0.546 of a
			// move, and a tick gains 0.1274.
			ps := make([]mgl64.Vec3, 30)
			pos, step := start, 0.0
			for i := range ps {
				step = step*0.546 + 0.1274
				pos = pos.Add(mgl64.Vec3{step, 0, 0})
				ps[i] = pos
			}
			return ps
		},
		claims: func(int) bool { return true }}
	if res := runTrace(t, w, tr); len(res.violations) != 0 || res.corrections != 0 {
		t.Fatalf("%d violations, %d corrections: %+v", len(res.violations), res.corrections, res.violations)
	}
	if chunkTracked(w, world.ChunkPos{6, 0}) {
		t.Fatal("the chunk beyond the loaded ones was loaded by the checks")
	}
}

// TestNoSetbackOnAFailedChunk checks that a client standing on the stand-in
// of a chunk that failed to load, in the air as far as the server can tell,
// is not set back for floating: the client sees blocks the server lacks.
func TestNoSetbackOnAFailedChunk(t *testing.T) {
	w := edgeWorld(t)
	// The chunk 7 spans x from 112 to 128, and failed to load: the client
	// stands on blocks the server does not have.
	start := mgl64.Vec3{120.5, floorY, 0.5}
	hover := acTrace{name: "standing on a failed chunk", start: start, bedrock: true,
		positions: func(*world.Tx) []mgl64.Vec3 {
			ps := make([]mgl64.Vec3, 200)
			for i := range ps {
				ps[i] = start
			}
			return ps
		},
		claims: func(int) bool { return true }}
	res := runTrace(t, w, hover)
	if len(res.violations) != 0 || res.corrections != 0 {
		t.Fatalf("standing: %d violations, %d corrections: %+v", len(res.violations), res.corrections, res.violations)
	}
	walk := acTrace{name: "walking on a failed chunk", start: start, ticks: 60, bedrock: true, input: forward(east, true, true)}
	walk.start = mgl64.Vec3{113.5, floorY, 0.5}
	if res := runTrace(t, w, walk); len(res.violations) != 0 || res.corrections != 0 {
		t.Fatalf("walking: %d violations, %d corrections: %+v", len(res.violations), res.corrections, res.violations)
	}
}

// TestHoveringIsStillFlaggedWhereChunksAreThere checks that the same stand
// in the air on a chunk that did load is a violation of Fly: the exemption
// is for chunks the server lacks, not for hovering.
func TestHoveringIsStillFlaggedWhereChunksAreThere(t *testing.T) {
	w := edgeWorld(t)
	start := mgl64.Vec3{-60.5, floorY + 20, 0.5}
	hover := acTrace{name: "hovering in the air", start: start, bedrock: true,
		positions: func(*world.Tx) []mgl64.Vec3 {
			ps := make([]mgl64.Vec3, 200)
			for i := range ps {
				ps[i] = start
			}
			return ps
		}}
	res := runTrace(t, w, hover)
	if n, _ := res.count(anticheat.Fly); n == 0 || res.corrections == 0 {
		t.Fatalf("hovering: %d violations of Fly, %d corrections; want it flagged and set back", n, res.corrections)
	}
}
