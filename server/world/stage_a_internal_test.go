package world

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/ecs"
	"github.com/go-gl/mathgl/mgl64"
)

func TestStageAEntityNBTPersistence(t *testing.T) {
	t.Parallel()
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()

	type badge struct {
		Rank int32
		Tag  string
	}
	kBadge := ecs.NewKey("test:stagea_badge", ecs.WithCodec(
		func(b badge) map[string]any {
			return map[string]any{"rank": b.Rank, "tag": b.Tag}
		},
		func(m map[string]any) (badge, bool) {
			r, ok1 := m["rank"].(int32)
			tag, ok2 := m["tag"].(string)
			if !ok1 || !ok2 {
				return badge{}, false
			}
			return badge{Rank: r, Tag: tag}, true
		},
	))
	kTransient := ecs.NewKey[int]("test:stagea_transient")

	et := testEntityType{}
	h := EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 1.5}}.New(et, testEntityConfig{})
	ecs.Set(h.Components(), kBadge, badge{Rank: 3, Tag: "warden"})
	ecs.Set(h.Components(), kTransient, 999)

	saved := w.EntityNBT(h)
	restored := w.entityFromNBT(et, h.UniqueID(), saved)
	if restored == nil {
		t.Fatal("entityFromNBT returned nil")
	}
	got, ok := ecs.Get(restored.Components(), kBadge)
	if !ok || got != (badge{Rank: 3, Tag: "warden"}) {
		t.Fatalf("restored ECS badge = (%+v, %v), want Rank:3 warden", got, ok)
	}
	if ecs.Has(restored.Components(), kTransient) {
		t.Fatal("transient ECS component should not persist across NBT")
	}

	// Deleting coded component clears adamant:ecs even when NativeNBT retained the old one.
	ecs.Delete(restored.Components(), kBadge)
	savedAfterDelete := w.EntityNBT(restored)
	if _, exists := savedAfterDelete["adamant:ecs"]; exists {
		t.Fatalf("expected adamant:ecs deleted after removing coded component, got %v", savedAfterDelete["adamant:ecs"])
	}
}

func TestStageAChunkCacheAndReadViewTrack(t *testing.T) {
	t.Parallel()
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()

	chunks := []ChunkPos{{0, 0}, {1, 0}, {0, 1}, {1, 1}, {2, 0}}
	<-w.Exec(func(tx *Tx) {
		for i, pos := range chunks {
			tx.SetBlockRID(cube.Pos{int(pos[0])*16 + 1, 64, int(pos[1])*16 + 1}, uint32(i+1), nil)
		}
		// Rotating across 4 chunks hits Tx's 4-slot associative cache cleanly.
		for range 3 {
			for _, pos := range chunks[:4] {
				if _, ok := tx.BlockRID(cube.Pos{int(pos[0])*16 + 1, 64, int(pos[1])*16 + 1}); !ok {
					t.Fatalf("missing block at %v", pos)
				}
			}
		}

		v := tx.NewReadView()
		// Read {2, 0} before Track() so it sits in v.cache, then verify Track() only records chunks read after Track().
		if _, ok := v.BlockRID(cube.Pos{2*16 + 1, 64, 1}); !ok {
			t.Fatal("expected settled block in ReadView")
		}
		v.Track()
		for _, pos := range chunks[:4] {
			if _, ok := v.BlockRID(cube.Pos{int(pos[0])*16 + 1, 64, int(pos[1])*16 + 1}); !ok {
				t.Fatalf("ReadView missed settled chunk %v", pos)
			}
		}
		tracked := v.Tracked()
		if len(tracked) != 4 || slices.Contains(tracked, ChunkPos{2, 0}) {
			t.Fatalf("ReadView tracked = %v, want exactly first 4 chunks", tracked)
		}

		// Closing a cached chunk invalidates Tx's 4-slot chunk cache cleanly.
		c0 := w.chunks[ChunkPos{0, 0}]
		w.closeChunk(tx, ChunkPos{0, 0}, c0)
		if _, ok := tx.BlockRID(cube.Pos{1, 64, 1}); ok {
			t.Fatal("Tx chunk cache returned closed chunk")
		}
	})
}
