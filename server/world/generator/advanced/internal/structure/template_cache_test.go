package structure

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func syntheticTemplate(blocks int) *Template {
	return &Template{
		Size:     [3]int32{1, 1, 1},
		Palettes: [][]Block{make([]Block, blocks)},
	}
}

func TestTemplateCacheByteBudgetAndLRU(t *testing.T) {
	t.Parallel()
	unit := estimateTemplateBytes(syntheticTemplate(10))
	cache := newTemplateCache(unit*2 + unit/2)

	var loads atomic.Int32
	makeLoader := func(blocks int) func() (*Template, error) {
		return func() (*Template, error) {
			loads.Add(1)
			return syntheticTemplate(blocks), nil
		}
	}

	t1, err := cache.load("a", makeLoader(10))
	if err != nil {
		t.Fatal(err)
	}
	t2, err := cache.load("b", makeLoader(10))
	if err != nil {
		t.Fatal(err)
	}
	if cache.bytes > cache.maxBytes {
		t.Fatalf("cache bytes %d exceeds budget %d", cache.bytes, cache.maxBytes)
	}

	// Touch "a" so "b" becomes least recently used.
	gotA, err := cache.load("a", makeLoader(10))
	if err != nil || gotA != t1 {
		t.Fatalf("expected cached pointer for a")
	}
	if loads.Load() != 2 {
		t.Fatalf("expected 2 loads after cache hit, got %d", loads.Load())
	}

	// Loading "c" pushes over budget and must evict "b", keeping "a" and "c".
	if _, err := cache.load("c", makeLoader(10)); err != nil {
		t.Fatal(err)
	}
	if cache.bytes > cache.maxBytes {
		t.Fatalf("cache bytes %d exceeds budget %d after eviction", cache.bytes, cache.maxBytes)
	}
	if _, ok := cache.entries["b"]; ok {
		t.Fatal("expected least-recently-used entry b to be evicted")
	}
	if _, ok := cache.entries["a"]; !ok {
		t.Fatal("expected recently-used entry a to remain cached")
	}
	if _, ok := cache.entries["c"]; !ok {
		t.Fatal("expected newly-loaded entry c to be cached")
	}

	// Reloading "b" re-decodes cleanly without affecting the old t2 pointer.
	t2Again, err := cache.load("b", makeLoader(10))
	if err != nil || t2Again == t2 {
		t.Fatalf("expected fresh decode for evicted b")
	}

	// Oversized template must be returned without violating maxBytes.
	oversized, err := cache.load("huge", makeLoader(100))
	if err != nil || len(oversized.Palettes[0]) != 100 {
		t.Fatalf("unexpected oversized template load result: %v", err)
	}
	if cache.bytes > cache.maxBytes {
		t.Fatalf("oversized template broke budget: bytes=%d max=%d", cache.bytes, cache.maxBytes)
	}
}

func TestTemplateCacheConcurrentSingleflightAndEviction(t *testing.T) {
	t.Parallel()
	unit := estimateTemplateBytes(syntheticTemplate(16))
	cache := newTemplateCache(unit * 4)

	var singleLoads atomic.Int32
	gate := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]*Template, 12)
	for i := range results {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tmpl, err := cache.load("shared", func() (*Template, error) {
				singleLoads.Add(1)
				<-gate
				return syntheticTemplate(16), nil
			})
			if err != nil {
				t.Errorf("load failed: %v", err)
				return
			}
			results[idx] = tmpl
		}(i)
	}
	close(gate)
	wg.Wait()
	if n := singleLoads.Load(); n != 1 {
		t.Fatalf("singleflight decoded %d times, want 1", n)
	}
	for i := 1; i < len(results); i++ {
		if results[i] != results[0] {
			t.Fatalf("concurrent callers received different *Template pointers")
		}
	}

	// Hammer concurrent loads across many keys under a tight byte budget.
	for g := range 8 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := range 64 {
				key := fmt.Sprintf("k-%d", (worker*7+i)%16)
				tmpl, err := cache.load(key, func() (*Template, error) {
					return syntheticTemplate(16), nil
				})
				if err != nil || tmpl == nil {
					t.Errorf("concurrent load(%s): %v", key, err)
				}
			}
		}(g)
	}
	wg.Wait()

	cache.mu.Lock()
	gotBytes, maxBytes := cache.bytes, cache.maxBytes
	cache.mu.Unlock()
	if gotBytes > maxBytes {
		t.Fatalf("cache bytes %d exceeds budget %d after concurrent load", gotBytes, maxBytes)
	}
}

func TestLoadSharesCachedTemplate(t *testing.T) {
	t.Parallel()
	const name = "igloo/top"
	a, err := Load(name)
	if err != nil {
		t.Fatalf("Load(%q): %v", name, err)
	}
	b, err := LoadOrCreate(name)
	if err != nil {
		t.Fatalf("LoadOrCreate(%q): %v", name, err)
	}
	if a != b {
		t.Fatalf("Load and LoadOrCreate did not share cached *Template for %q", name)
	}
}
