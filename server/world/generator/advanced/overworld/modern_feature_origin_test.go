package overworld

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestTreesCrossingChunkBordersAreWhole checks a feature reaching across a
// chunk border is the same in every chunk it reaches: every leaf block of the
// chunks along the border hangs on a log within leaf distance, so no chunk
// keeps a canopy whose trunk its neighbour never placed.
func TestTreesCrossingChunkBordersAreWhole(t *testing.T) {
	t.Parallel()
	world.DefaultBlockRegistry.Finalize()
	g := NewOverworld(0)
	chunks := make(map[world.ChunkPos]*chunk.Chunk)
	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 5; z++ {
			c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			g.GenerateChunk(world.ChunkPos{x, z}, c, world.GenerationContext{})
			chunks[world.ChunkPos{x, z}] = c
		}
	}
	name := func(p cube.Pos) string {
		c, ok := chunks[world.ChunkPos{int32(p[0] >> 4), int32(p[2] >> 4)}]
		if !ok || p[1] < c.Range().Min() || p[1] > c.Range().Max() {
			return ""
		}
		name, properties, _ := world.DefaultBlockRegistry.RuntimeIDToState(c.Block(uint8(p[0]&15), int16(p[1]), uint8(p[2]&15), 0))
		if strings.HasSuffix(name, "leaves") && properties["persistent_bit"] == true {
			return "persistent"
		}
		return name
	}
	leaves := make(map[cube.Pos]bool)
	var queue []cube.Pos
	distance := make(map[cube.Pos]int)
	for pos := range chunks {
		for x := range 16 {
			for z := range 16 {
				for y := 50; y < 140; y++ {
					p := cube.Pos{int(pos[0])*16 + x, y, int(pos[1])*16 + z}
					switch n := name(p); {
					case strings.HasSuffix(n, "leaves"):
						leaves[p] = true
					case strings.HasSuffix(n, "_log") || strings.HasSuffix(n, "_wood"):
						distance[p] = 0
						queue = append(queue, p)
					}
				}
			}
		}
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if distance[p] == 6 {
			continue
		}
		for _, face := range cube.Faces() {
			n := p.Side(face)
			if _, seen := distance[n]; leaves[n] && !seen {
				distance[n] = distance[p] + 1
				queue = append(queue, n)
			}
		}
	}
	orphans := 0
	for p := range leaves {
		inner := p[0] >= 2*16 && p[0] < 3*16 && p[2] >= 3*16 && p[2] < 5*16
		if _, attached := distance[p]; inner && !attached {
			orphans++
		}
	}
	if orphans != 0 {
		t.Fatalf("%d leaf blocks along the chunk border hang on no log", orphans)
	}
}

func syntheticOriginRecord(writes int) *originRecord {
	return &originRecord{
		steps: []originStep{{writes: make([]recordedWrite, writes)}},
	}
}

func TestOriginRecordCacheByteBudgetAndActiveProtection(t *testing.T) {
	t.Parallel()
	unit := estimateOriginRecordBytes(syntheticOriginRecord(100))
	cache := originRecordCache{
		capacity: 100,
		maxBytes: unit*2 + unit/2,
	}

	fillEntry := func(pos world.ChunkPos, writes int) *originRecordEntry {
		e, owner := cache.acquire(pos)
		if !owner {
			t.Fatalf("expected owner for %v", pos)
		}
		cache.fill(e, func() *originRecord { return syntheticOriginRecord(writes) })
		return e
	}

	e1 := fillEntry(world.ChunkPos{1, 0}, 100)
	e2 := fillEntry(world.ChunkPos{2, 0}, 100)
	// Keep e1 active while e2 becomes idle.
	cache.release(e2, 9)

	// Inserting e3 pushes over budget; e1 is active (must NOT be evicted), so e2 must be evicted.
	e3 := fillEntry(world.ChunkPos{3, 0}, 100)
	cache.mu.Lock()
	_, has1 := cache.entries[e1.origin]
	_, has2 := cache.entries[e2.origin]
	_, has3 := cache.entries[e3.origin]
	gotBytes := cache.bytes
	cache.mu.Unlock()
	if !has1 {
		t.Fatal("active entry e1 was evicted while in use")
	}
	if has2 {
		t.Fatal("idle entry e2 should have been evicted to stay within byte budget")
	}
	if !has3 {
		t.Fatal("new entry e3 missing from cache")
	}
	if gotBytes > cache.maxBytes {
		t.Fatalf("cache bytes %d exceeds budget %d", gotBytes, cache.maxBytes)
	}

	// Releasing e1 and e3 keeps both (2 * unit <= 2.5 * unit); adding e4 evicts LRU (e1).
	cache.release(e1, 9)
	cache.release(e3, 9)
	e4 := fillEntry(world.ChunkPos{4, 0}, 100)
	cache.release(e4, 9)

	cache.mu.Lock()
	_, has1 = cache.entries[e1.origin]
	_, has3 = cache.entries[e3.origin]
	_, has4 := cache.entries[e4.origin]
	gotBytes = cache.bytes
	cache.mu.Unlock()
	if has1 {
		t.Fatal("LRU idle entry e1 should have been evicted")
	}
	if !has3 || !has4 {
		t.Fatalf("expected e3 and e4 to remain cached (has3=%v has4=%v)", has3, has4)
	}
	if gotBytes > cache.maxBytes {
		t.Fatalf("cache bytes %d exceeds budget %d", gotBytes, cache.maxBytes)
	}

	// clear() must drop all ready entries and reset bytes to 0.
	cache.clear()
	cache.mu.Lock()
	remaining, remainingBytes := len(cache.entries), cache.bytes
	cache.mu.Unlock()
	if remaining != 0 || remainingBytes != 0 {
		t.Fatalf("after clear(): entries=%d bytes=%d, want 0, 0", remaining, remainingBytes)
	}
}

func TestOriginRecordCacheConcurrentByteBudget(t *testing.T) {
	t.Parallel()
	unit := estimateOriginRecordBytes(syntheticOriginRecord(64))
	cache := originRecordCache{
		capacity: 128,
		maxBytes: unit * 6,
	}
	done := make(chan struct{})
	const workers = 8
	errs := make(chan error, workers)
	for w := range workers {
		go func(id int) {
			for i := range 64 {
				pos := world.ChunkPos{int32((id*5 + i) % 24), int32(i % 3)}
				entry, owner := cache.acquire(pos)
				if owner {
					cache.fill(entry, func() *originRecord { return syntheticOriginRecord(64) })
				}
				rec := entry.wait()
				if rec == nil || len(rec.steps) != 1 {
					errs <- nil
				}
				cache.release(entry, 9)
			}
			done <- struct{}{}
		}(w)
	}
	for range workers {
		<-done
	}
	close(errs)
	for range errs {
		t.Fatal("unexpected nil record returned from concurrent originRecordCache")
	}
	cache.mu.Lock()
	gotBytes, maxBytes := cache.bytes, cache.maxBytes
	cache.mu.Unlock()
	if gotBytes > maxBytes {
		t.Fatalf("concurrent cache bytes %d exceeds budget %d", gotBytes, maxBytes)
	}
}
