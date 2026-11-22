package world

import (
	"runtime"
	"sync"
	"testing"
	"weak"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// assertWireCacheInvariants checks doubly-linked list symmetry, counts, and byte sums.
func assertWireCacheInvariants(t *testing.T, wc *wireCache) {
	t.Helper()
	wc.mu.Lock()
	defer wc.mu.Unlock()

	if wc.count == 0 {
		if wc.head != nil || wc.tail != nil || wc.bytes != 0 {
			t.Fatalf("empty wireCache has head=%v tail=%v bytes=%d", wc.head, wc.tail, wc.bytes)
		}
		return
	}
	if wc.head == nil || wc.tail == nil {
		t.Fatalf("non-empty wireCache (count=%d) has head=%v tail=%v", wc.count, wc.head, wc.tail)
	}
	if wc.head.wire.prev != nil {
		t.Fatal("head.prev != nil")
	}
	if wc.tail.wire.next != nil {
		t.Fatal("tail.next != nil")
	}

	var (
		n     int
		total int64
		prev  *Column
	)
	for cur := wc.head; cur != nil; cur = cur.wire.next {
		if !cur.wire.linked {
			t.Fatalf("node %d in list has linked=false", n)
		}
		if cur.wire.cache != wc {
			t.Fatalf("node %d has wrong cache pointer", n)
		}
		if cur.wire.prev != prev {
			t.Fatalf("node %d prev mismatch", n)
		}
		if cur.wire.bytes <= 0 {
			t.Fatalf("linked node %d has non-positive bytes=%d", n, cur.wire.bytes)
		}
		total += cur.wire.bytes
		prev = cur
		n++
		if n > wc.count+1 {
			t.Fatal("cycle detected in wireCache list")
		}
	}
	if prev != wc.tail {
		t.Fatal("tail does not match final node reached from head")
	}
	if n != wc.count {
		t.Fatalf("walked %d nodes, wc.count=%d", n, wc.count)
	}
	if total != wc.bytes {
		t.Fatalf("summed %d bytes, wc.bytes=%d", total, wc.bytes)
	}
}

// TestWireCacheBudgetAndTownVsExplorer verifies strict byte budget enforcement
// across 5,000 columns x 40 KiB (200 MiB inserted into a 64 MiB default budget)
// and checks that hot town columns promoted via cache hits survive explorer churn.
func TestWireCacheBudgetAndTownVsExplorer(t *testing.T) {
	var wc wireCache // default 64 MiB budget
	if wc.limit() != defaultWireCacheMaxBytes {
		t.Fatalf("default limit = %d, want %d", wc.limit(), defaultWireCacheMaxBytes)
	}

	const (
		townCount     = 100
		explorerCount = 5000
		subChunks     = 10
		subChunkSize  = 4 << 10 // 10 * 4 KiB = 40 KiB per column
	)
	payload := make([]byte, subChunkSize)

	town := make([]*Column, townCount)
	for i := range town {
		col := newColumn(nil)
		col.attachWireCache(&wc)
		for s := int16(0); s < subChunks; s++ {
			col.CacheNetworkSubChunkPayload(s, payload)
		}
		town[i] = col
	}

	explorers := make([]*Column, explorerCount)
	for i := range explorers {
		col := newColumn(nil)
		col.attachWireCache(&wc)
		for s := int16(0); s < subChunks; s++ {
			col.CacheNetworkSubChunkPayload(s, payload)
		}
		explorers[i] = col

		// Periodically read all town columns so they stay MRU while explorers churn.
		if i%500 == 499 {
			for _, tc := range town {
				if _, ok := tc.CachedNetworkSubChunkPayload(0); !ok {
					t.Fatalf("hot town column evicted at explorer %d", i)
				}
			}
		}
	}

	bytes, count := wc.stats()
	if bytes > defaultWireCacheMaxBytes {
		t.Fatalf("retained bytes = %d exceeds default budget %d", bytes, defaultWireCacheMaxBytes)
	}
	wantCols := int(defaultWireCacheMaxBytes / (subChunks * subChunkSize))
	if count != wantCols {
		t.Fatalf("retained columns = %d, want %d", count, wantCols)
	}
	assertWireCacheInvariants(t, &wc)

	// Every hot town column must still be cached.
	for i, tc := range town {
		if _, ok := tc.CachedNetworkSubChunkPayload(0); !ok {
			t.Fatalf("hot town column %d was evicted", i)
		}
	}
	// Earliest explorer columns must have been evicted and their maps released.
	for i := 0; i < 1000; i++ {
		if _, ok := explorers[i].CachedNetworkSubChunkPayload(0); ok {
			t.Fatalf("cold explorer column %d was not evicted", i)
		}
		if explorers[i].ownCache.subChunks != nil {
			t.Fatalf("evicted explorer column %d retained subChunks map", i)
		}
		if explorers[i].wire.linked || explorers[i].wire.bytes != 0 {
			t.Fatalf("evicted explorer column %d still linked=%v bytes=%d", i, explorers[i].wire.linked, explorers[i].wire.bytes)
		}
	}
	// Most recent explorer columns must still be retained.
	for i := explorerCount - 100; i < explorerCount; i++ {
		if _, ok := explorers[i].CachedNetworkSubChunkPayload(0); !ok {
			t.Fatalf("recent explorer column %d was unexpectedly evicted", i)
		}
	}
}

// TestWireCacheAllPayloadTypesAndHitPromotion verifies accounting, hit promotion,
// and complete eviction across every cached wire payload type on Column.
func TestWireCacheAllPayloadTypesAndHitPromotion(t *testing.T) {
	wc := wireCache{maxBytes: 1024}

	colA := newColumn(nil)
	colA.attachWireCache(&wc)
	colA.CacheSubChunkHeightMap(0, 1, make([]int8, 256))
	colA.CacheNetworkBiomePayload(make([]byte, 64))
	colA.CacheLimitedChunkPayload(make([]byte, 65))
	colA.CacheChunkBlockEntityPayload(false, make([]byte, 32))
	colA.CacheChunkBlockEntityPayload(true, make([]byte, 31))
	colA.CacheSubChunkBlockEntityPayload(0, make([]byte, 48))
	colA.CacheNetworkSubChunkPayload(0, make([]byte, 128))
	colA.CacheFullChunkPayload(make([]byte, 200))

	wantA := int64(256 + 64 + 65 + 32 + 31 + 48 + 128 + 200) // 824 bytes
	if got, count := wc.stats(); got != wantA || count != 1 {
		t.Fatalf("after colA: bytes=%d count=%d, want %d and 1", got, count, wantA)
	}

	colB := newColumn(nil)
	colB.attachWireCache(&wc)
	colB.CacheFullChunkPayload(make([]byte, 200)) // total = 1024, fits exactly

	if got, count := wc.stats(); got != 1024 || count != 2 {
		t.Fatalf("after colB: bytes=%d count=%d, want 1024 and 2", got, count)
	}

	// Promote colA via each getter type; colB becomes LRU tail.
	if _, _, ok := colA.CachedSubChunkHeightMap(0); !ok {
		t.Fatal("missing height map")
	}
	if _, ok := colA.CachedNetworkBiomePayload(); !ok {
		t.Fatal("missing biome payload")
	}
	if _, ok := colA.CachedLimitedChunkPayload(); !ok {
		t.Fatal("missing limited chunk payload")
	}
	if _, ok := colA.CachedChunkBlockEntityPayload(false); !ok {
		t.Fatal("missing chunk block entity payload")
	}
	if _, ok := colA.CachedChunkBlockEntityPayload(true); !ok {
		t.Fatal("missing chunk block entity no-border payload")
	}
	if _, ok := colA.CachedSubChunkBlockEntityPayload(0); !ok {
		t.Fatal("missing sub-chunk block entity payload")
	}
	if _, ok := colA.CachedNetworkSubChunkPayload(0); !ok {
		t.Fatal("missing sub-chunk payload")
	}
	if _, ok := colA.CachedFullChunkPayload(); !ok {
		t.Fatal("missing full chunk payload")
	}

	// Insert colC (100 bytes) -> must evict colB (tail), keeping colA (head).
	colC := newColumn(nil)
	colC.attachWireCache(&wc)
	colC.CacheFullChunkPayload(make([]byte, 100))

	assertWireCacheInvariants(t, &wc)
	if _, ok := colB.CachedFullChunkPayload(); ok {
		t.Fatal("LRU tail colB was not evicted")
	}
	if _, ok := colA.CachedFullChunkPayload(); !ok {
		t.Fatal("MRU colA was unexpectedly evicted")
	}

	// Now insert colD (950 bytes) -> must evict colC and colA completely.
	colD := newColumn(nil)
	colD.attachWireCache(&wc)
	colD.CacheFullChunkPayload(make([]byte, 950))

	assertWireCacheInvariants(t, &wc)
	if _, _, ok := colA.CachedSubChunkHeightMap(0); ok {
		t.Fatal("colA height map survived eviction")
	}
	if _, ok := colA.CachedNetworkBiomePayload(); ok {
		t.Fatal("colA biome payload survived eviction")
	}
	if _, ok := colA.CachedLimitedChunkPayload(); ok {
		t.Fatal("colA limited payload survived eviction")
	}
	if _, ok := colA.CachedChunkBlockEntityPayload(false); ok {
		t.Fatal("colA block entity payload survived eviction")
	}
	if _, ok := colA.CachedChunkBlockEntityPayload(true); ok {
		t.Fatal("colA block entity no-border payload survived eviction")
	}
	if _, ok := colA.CachedSubChunkBlockEntityPayload(0); ok {
		t.Fatal("colA sub-chunk block entity payload survived eviction")
	}
	if _, ok := colA.CachedNetworkSubChunkPayload(0); ok {
		t.Fatal("colA sub-chunk payload survived eviction")
	}
	if _, ok := colA.CachedFullChunkPayload(); ok {
		t.Fatal("colA full payload survived eviction")
	}
	if colA.subChunkHeightMaps != nil || colA.subChunkBlockEntityPayloads != nil || colA.ownCache.subChunks != nil {
		t.Fatal("colA retained map allocations after eviction")
	}
}

// TestWireCacheMultiSlotKeyAndOverwrites checks multi-SlotKey accounting,
// overwrite deltas (grow/shrink/zero), and eviction preserving cache handles.
func TestWireCacheMultiSlotKeyAndOverwrites(t *testing.T) {
	wc := wireCache{maxBytes: 1000}
	keyA, keyB := NewSlotKey("protoA"), NewSlotKey("protoB")

	col1 := newColumn(nil)
	col1.attachWireCache(&wc)
	cNil := col1.Cache(nil)
	cA := col1.Cache(keyA)
	cB := col1.Cache(keyB)

	cNil.CacheNetworkSubChunkPayload(0, make([]byte, 200))
	cA.CacheNetworkSubChunkPayload(0, make([]byte, 250))
	cB.CacheFullChunkPayload(make([]byte, 300))
	if got, count := wc.stats(); got != 750 || count != 1 {
		t.Fatalf("multi-key bytes=%d count=%d, want 750 and 1", got, count)
	}

	// Overwrite with smaller payload -> delta -100 -> total 650.
	cA.CacheNetworkSubChunkPayload(0, make([]byte, 150))
	if got, _ := wc.stats(); got != 650 {
		t.Fatalf("after shrink overwrite: bytes=%d, want 650", got)
	}
	// Overwrite with larger payload -> delta +50 -> total 700.
	cB.CacheFullChunkPayload(make([]byte, 350))
	if got, _ := wc.stats(); got != 700 {
		t.Fatalf("after grow overwrite: bytes=%d, want 700", got)
	}

	// Evict col1 by inserting col2 (500 bytes, 700+500 > 1000).
	col2 := newColumn(nil)
	col2.attachWireCache(&wc)
	col2.CacheFullChunkPayload(make([]byte, 500))

	assertWireCacheInvariants(t, &wc)
	if _, ok := cNil.CachedNetworkSubChunkPayload(0); ok {
		t.Fatal("col1 Cache(nil) was not evicted")
	}
	if _, ok := cA.CachedNetworkSubChunkPayload(0); ok {
		t.Fatal("col1 Cache(keyA) was not evicted")
	}
	if _, ok := cB.CachedFullChunkPayload(); ok {
		t.Fatal("col1 Cache(keyB) was not evicted")
	}
	if col1.Cache(keyA) != cA || col1.Cache(keyB) != cB {
		t.Fatal("eviction broke ColumnCache handle identity")
	}

	// Re-caching via the existing keyA handle re-links col1 and accounts bytes.
	cA.CacheNetworkSubChunkPayload(1, make([]byte, 400))
	if got, count := wc.stats(); got != 900 || count != 2 {
		t.Fatalf("after re-cache via existing handle: bytes=%d count=%d, want 900 and 2", got, count)
	}

	// Overwriting the only payload on col1 with 0 bytes unlinks col1.
	cA.CacheNetworkSubChunkPayload(1, nil)
	if got, count := wc.stats(); got != 500 || count != 1 {
		t.Fatalf("after zero overwrite: bytes=%d count=%d, want 500 and 1", got, count)
	}
	assertWireCacheInvariants(t, &wc)
}

// TestWireCacheInvalidationAndUnloadNoLeak verifies partial/full invalidations
// and confirms unloaded columns are unlinked with zero pointer retention.
func TestWireCacheInvalidationAndUnloadNoLeak(t *testing.T) {
	wc := wireCache{maxBytes: 1 << 20}
	key := NewSlotKey("alt")

	col := newColumn(nil)
	col.attachWireCache(&wc)
	col.CacheSubChunkHeightMap(0, 1, make([]int8, 256))
	col.CacheNetworkSubChunkPayload(0, make([]byte, 1000))
	col.Cache(key).CacheNetworkSubChunkPayload(0, make([]byte, 1200))
	col.CacheFullChunkPayload(make([]byte, 2000))
	col.CacheNetworkBiomePayload(make([]byte, 300))
	col.CacheLimitedChunkPayload(make([]byte, 301))
	col.CacheChunkBlockEntityPayload(false, make([]byte, 100))
	col.CacheSubChunkBlockEntityPayload(0, make([]byte, 50))

	want := int64(256 + 1000 + 1200 + 2000 + 300 + 301 + 100 + 50)
	if got, _ := wc.stats(); got != want {
		t.Fatalf("initial bytes=%d, want %d", got, want)
	}

	col.invalidateSubChunkHeightMaps()
	want -= 256
	if got, _ := wc.stats(); got != want {
		t.Fatalf("after heightmap invalidation: bytes=%d, want %d", got, want)
	}

	col.invalidateNetworkSubChunkPayloads()
	want -= 1000 + 1200 + 2000
	if got, _ := wc.stats(); got != want {
		t.Fatalf("after sub-chunk invalidation: bytes=%d, want %d", got, want)
	}

	col.invalidateBlockEntityPayloads()
	want -= 100 + 50
	if got, _ := wc.stats(); got != want {
		t.Fatalf("after block entity invalidation: bytes=%d, want %d", got, want)
	}

	col.invalidateNetworkBiomePayload()
	want -= 300 + 301
	if want != 0 {
		t.Fatalf("test math bug: want=%d", want)
	}
	if got, count := wc.stats(); got != 0 || count != 0 {
		t.Fatalf("after full invalidation: bytes=%d count=%d, want 0 and 0", got, count)
	}
	assertWireCacheInvariants(t, &wc)

	// Verify unload releases all pointers so GC collects the Column.
	var wp weak.Pointer[Column]
	func() {
		transient := newColumn(nil)
		transient.attachWireCache(&wc)
		transient.CacheFullChunkPayload(make([]byte, 8192))
		wp = weak.Make(transient)
		transient.releaseWireCache()
	}()
	assertWireCacheInvariants(t, &wc)
	for i := 0; i < 10 && wp.Value() != nil; i++ {
		runtime.GC()
	}
	if wp.Value() != nil {
		t.Fatal("wireCache leaked pointer to unloaded Column")
	}
}

// TestWireCacheStandaloneColumnAndTinyBudget verifies standalone nil-world
// Columns/ColumnCaches never panic and a single column exceeding a tiny budget
// is never self-evicted.
func TestWireCacheStandaloneColumnAndTinyBudget(t *testing.T) {
	// Zero-value Column without a World.
	col := &Column{}
	col.CacheSubChunkHeightMap(0, 1, make([]int8, 256))
	col.CacheNetworkBiomePayload([]byte{1, 2, 3})
	col.CacheLimitedChunkPayload([]byte{1, 2, 3, 0})
	col.CacheNetworkSubChunkPayload(0, []byte{4, 5})
	col.CacheFullChunkPayload([]byte{6, 7, 8, 9})
	col.CacheChunkBlockEntityPayload(false, []byte{10})
	col.CacheSubChunkBlockEntityPayload(0, []byte{11})

	if _, ok := col.CachedFullChunkPayload(); !ok {
		t.Fatal("standalone column lost full payload")
	}
	if col.wire.bytes != 256+3+4+2+4+1+1 {
		t.Fatalf("standalone column wire.bytes = %d", col.wire.bytes)
	}

	// Standalone zero-value ColumnCache without a Column.
	var standalone ColumnCache
	standalone.CacheNetworkSubChunkPayload(0, []byte{1, 2})
	standalone.CacheFullChunkPayload([]byte{3, 4})
	if _, ok := standalone.CachedNetworkSubChunkPayload(0); !ok {
		t.Fatal("standalone ColumnCache lost sub-chunk payload")
	}
	if _, ok := standalone.CachedFullChunkPayload(); !ok {
		t.Fatal("standalone ColumnCache lost full payload")
	}

	// Attaching a pre-populated standalone column adopts its bytes and enforces budget.
	wc := wireCache{maxBytes: 100}
	other := newColumn(nil)
	other.attachWireCache(&wc)
	other.CacheFullChunkPayload(make([]byte, 80))

	col.attachWireCache(&wc)
	assertWireCacheInvariants(t, &wc)
	if _, ok := other.CachedFullChunkPayload(); ok {
		t.Fatal("other column should have been evicted when oversized col attached")
	}
	// col itself exceeds the 100-byte tiny budget, so it must remain cached (not self-evict).
	if _, ok := col.CachedFullChunkPayload(); !ok {
		t.Fatal("oversized column self-evicted")
	}
}

// TestWireCacheConcurrentRegionInvalidation exercises concurrent invalidations
// across disjoint columns (as happens during parallel region ticks) under -race.
func TestWireCacheConcurrentRegionInvalidation(t *testing.T) {
	wc := wireCache{maxBytes: 64 << 20}
	const (
		workers = 8
		perW    = 64
	)
	cols := make([][]*Column, workers)
	payload := make([]byte, 2048)
	for w := range cols {
		cols[w] = make([]*Column, perW)
		for i := range cols[w] {
			col := newColumn(nil)
			col.attachWireCache(&wc)
			col.CacheNetworkSubChunkPayload(0, payload)
			col.CacheNetworkBiomePayload(payload[:256])
			col.CacheChunkBlockEntityPayload(false, payload[:128])
			col.CacheSubChunkHeightMap(0, 1, make([]int8, 256))
			cols[w][i] = col
		}
	}

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func(list []*Column) {
			defer wg.Done()
			for _, col := range list {
				col.invalidateSubChunkHeightMaps()
				col.invalidateNetworkSubChunkPayloads()
				col.invalidateBlockEntityPayloads()
				col.invalidateNetworkBiomePayload()
			}
		}(cols[w])
	}
	wg.Wait()

	if bytes, count := wc.stats(); bytes != 0 || count != 0 {
		t.Fatalf("after concurrent invalidation: bytes=%d count=%d, want 0 and 0", bytes, count)
	}
	assertWireCacheInvariants(t, &wc)
}

// TestWireCacheWorldLifecycle verifies end-to-end integration with World:
// placeColumn attachment, eviction, block-edit invalidation, replacement, and closeChunk unload.
func TestWireCacheWorldLifecycle(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	w.wireCache.setMaxBytes(8 << 10)

	done := w.Exec(func(tx *Tx) {
		payload := make([]byte, 4<<10)
		cols := make([]*Column, 4)
		for i := range cols {
			pos := ChunkPos{int32(i), 0}
			col := newColumn(chunk.New(w.conf.Blocks, w.Range()))
			col.markReady()
			col.lightReady.Store(true)
			w.placeColumn(pos, col)
			col.CacheFullChunkPayload(payload)
			cols[i] = col
		}
		if bytes, count := w.wireCache.stats(); bytes != 8<<10 || count != 2 {
			t.Fatalf("after 4x4KiB with 8KiB budget: bytes=%d count=%d, want 8192 and 2", bytes, count)
		}
		// cols[0] and cols[1] evicted; cols[2] and cols[3] retained.
		if _, ok := cols[0].CachedFullChunkPayload(); ok {
			t.Fatal("cols[0] should be evicted")
		}
		if _, ok := cols[3].CachedFullChunkPayload(); !ok {
			t.Fatal("cols[3] should be retained")
		}

		// Block edit invalidates cols[3] and updates world wireCache bytes.
		cols[3].SetBlock(1, 10, 1, 0, w.conf.Blocks.AirRuntimeID()+1)
		tx.SetBlock(cube.Pos{3<<4 + 1, 10, 1}, nil, &SetOpts{DisableBlockUpdates: true})
		if _, ok := cols[3].CachedFullChunkPayload(); ok {
			t.Fatal("cols[3] should be invalidated after SetBlock")
		}
		if bytes, count := w.wireCache.stats(); bytes != 4<<10 || count != 1 {
			t.Fatalf("after SetBlock on cols[3]: bytes=%d count=%d, want 4096 and 1", bytes, count)
		}

		// Replacing cols[2] via placeColumn releases its cached bytes.
		replacement := newColumn(chunk.New(w.conf.Blocks, w.Range()))
		replacement.markReady()
		replacement.lightReady.Store(true)
		w.placeColumn(ChunkPos{2, 0}, replacement)
		if bytes, count := w.wireCache.stats(); bytes != 0 || count != 0 {
			t.Fatalf("after placeColumn replacement: bytes=%d count=%d, want 0 and 0", bytes, count)
		}

		// Cache on replacement and unload via closeChunk.
		replacement.CacheFullChunkPayload(payload)
		if bytes, count := w.wireCache.stats(); bytes != 4<<10 || count != 1 {
			t.Fatalf("after caching replacement: bytes=%d count=%d, want 4096 and 1", bytes, count)
		}
		w.closeChunk(tx, ChunkPos{2, 0}, replacement)
		if bytes, count := w.wireCache.stats(); bytes != 0 || count != 0 {
			t.Fatalf("after closeChunk: bytes=%d count=%d, want 0 and 0", bytes, count)
		}
	})
	<-done
	assertWireCacheInvariants(t, &w.wireCache)
}

func BenchmarkWireCacheHitHead(b *testing.B) {
	wc := wireCache{maxBytes: defaultWireCacheMaxBytes}
	col := newColumn(nil)
	col.attachWireCache(&wc)
	col.CacheNetworkSubChunkPayload(0, make([]byte, 4<<10))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = col.CachedNetworkSubChunkPayload(0)
	}
}

func BenchmarkWireCacheHitMove(b *testing.B) {
	wc := wireCache{maxBytes: defaultWireCacheMaxBytes}
	const n = 256
	cols := make([]*Column, n)
	payload := make([]byte, 4<<10)
	for i := range cols {
		col := newColumn(nil)
		col.attachWireCache(&wc)
		col.CacheNetworkSubChunkPayload(0, payload)
		cols[i] = col
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cols[i&(n-1)].CachedNetworkSubChunkPayload(0)
	}
}

func BenchmarkWireCacheInsertAndEvict(b *testing.B) {
	wc := wireCache{maxBytes: 1 << 20} // 1 MiB -> holds 256 x 4 KiB
	const poolSize = 512
	cols := make([]*Column, poolSize)
	for i := range cols {
		col := newColumn(nil)
		col.attachWireCache(&wc)
		cols[i] = col
	}
	payload := make([]byte, 4<<10)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cols[i&(poolSize-1)].CacheFullChunkPayload(payload)
	}
}
