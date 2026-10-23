package portal_test

import (
	"sync/atomic"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/portal"
)

// countingGenerator counts the chunks it generates.
type countingGenerator struct {
	world.NopGenerator
	generated atomic.Int64
}

func (g *countingGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, context world.GenerationContext) world.GenerationResult {
	g.generated.Add(1)
	return g.NopGenerator.GenerateChunk(pos, c, context)
}

// TestFindNetherPortalGeneratesNoChunks checks that searching the 128-block
// Overworld radius reads the POI index rather than every block: arriving from
// the Nether used to generate up to 289 chunks on the destination tick.
func TestFindNetherPortalGeneratesNoChunks(t *testing.T) {
	gen := &countingGenerator{}
	w := world.Config{Synchronous: true, Generator: gen}.New()
	t.Cleanup(func() { _ = w.Close() })

	origin := cube.Pos{8, 10, 8}
	mustDo(t, w, func(tx *world.Tx) {
		buildVerticalFrame(tx, origin, cube.Z, 2, 3)
		if !portal.TryActivateNetherPortal(tx, origin) {
			t.Fatal("TryActivateNetherPortal() = false, want true")
		}
	})
	before := gen.generated.Load()
	mustDo(t, w, func(tx *world.Tx) {
		p, ok := portal.FindNetherPortal(tx, cube.Pos{100, 60, -90}, 128)
		if !ok {
			t.Fatal("FindNetherPortal() did not find the portal 92 blocks away")
		}
		if spawn := p.Spawn(); spawn[0] != origin[0] || spawn[2] < origin[2] || spawn[2] > origin[2]+1 {
			t.Fatalf("FindNetherPortal() spawn = %v, want the portal at %v", spawn, origin)
		}
		if _, ok := portal.FindNetherPortal(tx, cube.Pos{5000, 60, 5000}, 128); ok {
			t.Fatal("FindNetherPortal() found a portal in terrain that was never generated")
		}
	})
	if generated := gen.generated.Load() - before; generated != 0 {
		t.Fatalf("FindNetherPortal() generated %d chunks, want 0", generated)
	}
}

// TestFindNetherPortalPrefersNearest checks the nearest of two portals wins.
func TestFindNetherPortalPrefersNearest(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })

	near, far := cube.Pos{40, 10, 8}, cube.Pos{-60, 10, 8}
	mustDo(t, w, func(tx *world.Tx) {
		for _, origin := range []cube.Pos{near, far} {
			buildVerticalFrame(tx, origin, cube.Z, 2, 3)
			if !portal.TryActivateNetherPortal(tx, origin) {
				t.Fatalf("TryActivateNetherPortal(%v) = false, want true", origin)
			}
		}
		p, ok := portal.FindNetherPortal(tx, cube.Pos{20, 10, 8}, 128)
		if !ok || p.Spawn()[0] != near[0] {
			t.Fatalf("FindNetherPortal() = %v, %v; want the portal at %v", p.Spawn(), ok, near)
		}
	})
}

// TestLoadAreaReportsReadyChunks checks the channel of LoadArea is closed once
// every chunk of the area is ready.
func TestLoadAreaReportsReadyChunks(t *testing.T) {
	gen := &countingGenerator{}
	w := world.Config{Synchronous: true, Generator: gen}.New()
	t.Cleanup(func() { _ = w.Close() })

	var done <-chan struct{}
	mustDo(t, w, func(tx *world.Tx) {
		done = tx.LoadArea(cube.Pos{1000, 0, 1000}, cube.Pos{1040, 0, 1040})
	})
	select {
	case <-done:
	default:
		t.Fatal("LoadArea() did not report the area ready")
	}
	if generated := gen.generated.Load(); generated < 9 {
		t.Fatalf("LoadArea() generated %d chunks, want the 9 of the area", generated)
	}
}
