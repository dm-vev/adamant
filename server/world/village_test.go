package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/google/uuid"
)

func TestNativePOITransitions(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	w := Config{Blocks: registry, Dim: End, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *Tx) {
		pos := cube.Pos{-1, 255, -17}
		put := func(name string, properties map[string]any) {
			rid, ok := registry.StateToRuntimeID(name, properties)
			if !ok { t.Fatalf("unknown native state %s %v", name, properties) }
			tx.SetBlock(pos, registry.BlockByRuntimeIDOrAir(rid), nil)
		}
		put("minecraft:bed", map[string]any{"head_piece_bit": false})
		if _, ok := tx.POI(pos); ok { t.Fatal("bed foot registered as home") }
		put("minecraft:bed", map[string]any{"head_piece_bit": true})
		if poi, ok := tx.POI(pos); !ok || poi.Kind != Home { t.Fatalf("bed head: %+v, found=%v", poi, ok) }
		chunkPos := chunkPosFromBlockPos(pos)
		w.indexPOIChunk(chunkPos, tx.chunk(chunkPos))
		if _, ok := tx.POI(pos); !ok { t.Fatal("reindex lost home at inclusive upper boundary") }
		for _, workstation := range []string{"blast_furnace", "smoker"} {
			put("minecraft:"+workstation, nil)
			owner := uuid.UUID{1}
			if !tx.ClaimPOI(pos, owner) { t.Fatalf("could not claim %s", workstation) }
			put("minecraft:lit_"+workstation, nil)
			poi, ok := tx.POI(pos)
			if !ok || poi.Kind != Workstation || poi.Type != "minecraft:"+workstation || !poi.HasOwner || poi.Owner != owner {
				t.Fatalf("lighting %s lost its claim: %+v, found=%v", workstation, poi, ok)
			}
			if tx.ClaimPOI(pos, uuid.UUID{2}) { t.Fatal("claimed occupied workstation") }
			tx.SetBlock(pos, nil, nil)
			if _, ok := tx.POI(pos); ok { t.Fatal("air retained a POI") }
		}
		put("minecraft:stonecutter_block", nil)
		if poi, ok := tx.POI(pos); !ok || poi.Kind != Workstation || poi.Type != "minecraft:stonecutter" { t.Fatalf("native stonecutter: %+v, found=%v", poi, ok) }
	})
}

// TestReadingLoadedChunkDoesNotRescanPOIs checks reading a block of a loaded
// chunk does no work per read, such as indexing the POIs of the chunk again:
// it allocates nothing, even in a chunk holding a POI.
func TestReadingLoadedChunkDoesNotRescanPOIs(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	w := Config{Blocks: registry, Dim: End, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *Tx) {
		pos := cube.Pos{3, 64, 3}
		rid, ok := registry.StateToRuntimeID("minecraft:stonecutter_block", nil)
		if !ok {
			t.Fatal("unknown stonecutter state")
		}
		tx.SetBlock(pos, registry.BlockByRuntimeIDOrAir(rid), nil)
		if _, ok := tx.POI(pos); !ok {
			t.Fatal("the stonecutter is no POI")
		}
		beside := pos.Side(cube.FaceEast)
		if allocs := testing.AllocsPerRun(100, func() { tx.Block(beside) }); allocs != 0 {
			t.Fatalf("reading a block of a loaded chunk allocated %v times", allocs)
		}
		if _, ok := tx.POI(pos); !ok {
			t.Fatal("reading blocks lost the POI")
		}
	})
}
