package ecs_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/dm-vev/adamant/server/ecs"
)

type testStats struct {
	HP    int
	Label string
}

func TestKeyRegistrationAndDenseIDs(t *testing.T) {
	k1 := ecs.NewKey[int]("test:dense_a")
	k2 := ecs.NewKey[string]("test:dense_b")
	if k1.ID() == 0 || k2.ID() == 0 {
		t.Fatalf("expected non-zero IDs, got %d and %d", k1.ID(), k2.ID())
	}
	if k2.ID() != k1.ID()+1 {
		t.Fatalf("expected monotonic dense IDs, got %d followed by %d", k1.ID(), k2.ID())
	}
	if k1.Name() != "test:dense_a" || k2.Name() != "test:dense_b" {
		t.Fatalf("unexpected names: %q, %q", k1.Name(), k2.Name())
	}

	// Same name + same type returns identical key.
	k1Again := ecs.NewKey[int]("test:dense_a")
	if k1Again.ID() != k1.ID() {
		t.Fatalf("expected reused ID %d, got %d", k1.ID(), k1Again.ID())
	}

	// Same name + different type must panic.
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate key name with different type")
		}
	}()
	ecs.NewKey[float64]("test:dense_a")
}

func TestKeyEmptyNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on empty key name")
		}
	}()
	ecs.NewKey[int]("")
}

func TestBagCRUDAndZeroState(t *testing.T) {
	kStats := ecs.NewKey[testStats]("test:crud_stats")
	kFlag := ecs.NewKey[struct{}]("test:crud_flag")

	var b ecs.Bag
	if !b.Empty() {
		t.Fatal("fresh Bag should be empty")
	}
	if v, ok := ecs.Get(&b, kStats); ok || v != (testStats{}) {
		t.Fatalf("unexpected Get on empty Bag: %+v, %v", v, ok)
	}
	if ecs.Has(&b, kStats) {
		t.Fatal("Has should be false on empty Bag")
	}

	// Nil Bag / uninitialized Key safety on read/delete.
	var zeroKey ecs.Key[int]
	if v, ok := ecs.Get[int](nil, zeroKey); ok || v != 0 {
		t.Fatalf("expected (0, false), got (%d, %v)", v, ok)
	}
	if ecs.Has[int](nil, zeroKey) {
		t.Fatal("Has(nil, zeroKey) should be false")
	}
	ecs.Delete[int](nil, zeroKey)

	// Set and Get.
	ecs.Set(&b, kStats, testStats{HP: 20, Label: "alpha"})
	if b.Empty() {
		t.Fatal("Bag should not be empty after Set")
	}
	if !ecs.Has(&b, kStats) {
		t.Fatal("Has should be true after Set")
	}
	got, ok := ecs.Get(&b, kStats)
	if !ok || got != (testStats{HP: 20, Label: "alpha"}) {
		t.Fatalf("Get mismatch: got %+v, %v", got, ok)
	}

	// Update in place on existing slot.
	ecs.Update(&b, kStats, func(s *testStats) {
		s.HP += 5
		s.Label = "beta"
	})
	got, ok = ecs.Get(&b, kStats)
	if !ok || got != (testStats{HP: 25, Label: "beta"}) {
		t.Fatalf("Get after Update mismatch: got %+v, %v", got, ok)
	}

	// Update on absent slot creates zero value then invokes fn.
	ecs.Update(&b, kFlag, func(*struct{}) {})
	if !ecs.Has(&b, kFlag) {
		t.Fatal("Has should be true after Update on missing key")
	}

	// Delete.
	ecs.Delete(&b, kStats)
	if ecs.Has(&b, kStats) {
		t.Fatal("Has should be false after Delete")
	}
	if _, ok := ecs.Get(&b, kStats); ok {
		t.Fatal("Get should return false after Delete")
	}
	ecs.Delete(&b, kFlag)
	if !b.Empty() {
		t.Fatal("Bag should be empty after deleting all keys")
	}
}

func TestBagCloneIsolation(t *testing.T) {
	k := ecs.NewKey[testStats]("test:clone_stats")

	var empty ecs.Bag
	if c := empty.Clone(); !c.Empty() {
		t.Fatal("clone of empty Bag should be empty")
	}

	var b ecs.Bag
	ecs.Set(&b, k, testStats{HP: 10, Label: "orig"})
	cloned := b.Clone()

	ecs.Update(&cloned, k, func(s *testStats) {
		s.HP = 99
		s.Label = "cloned"
	})

	origVal, ok := ecs.Get(&b, k)
	if !ok || origVal != (testStats{HP: 10, Label: "orig"}) {
		t.Fatalf("original Bag mutated after Clone update: %+v", origVal)
	}
	clonedVal, ok := ecs.Get(&cloned, k)
	if !ok || clonedVal != (testStats{HP: 99, Label: "cloned"}) {
		t.Fatalf("cloned Bag mismatch: %+v", clonedVal)
	}

	// Deleted slots should clone as empty without error.
	ecs.Delete(&b, k)
	if c := b.Clone(); !c.Empty() {
		t.Fatal("clone of Bag with only deleted slots should be empty")
	}
}

func TestBagNBTPersistence(t *testing.T) {
	kPersisted := ecs.NewKey("test:nbt_stats", ecs.WithCodec(
		func(s testStats) map[string]any {
			return map[string]any{"hp": int32(s.HP), "label": s.Label}
		},
		func(m map[string]any) (testStats, bool) {
			hp, ok1 := m["hp"].(int32)
			label, ok2 := m["label"].(string)
			if !ok1 || !ok2 {
				return testStats{}, false
			}
			return testStats{HP: int(hp), Label: label}, true
		},
	))
	kRuntimeOnly := ecs.NewKey[int]("test:nbt_runtime")

	var b ecs.Bag
	if m := ecs.EncodeNBT(&b); m != nil {
		t.Fatalf("expected nil NBT for empty Bag, got %v", m)
	}

	ecs.Set(&b, kRuntimeOnly, 123)
	if m := ecs.EncodeNBT(&b); m != nil {
		t.Fatalf("expected nil NBT when only non-codec keys present, got %v", m)
	}

	ecs.Set(&b, kPersisted, testStats{HP: 42, Label: "knight"})
	encoded := ecs.EncodeNBT(&b)
	if len(encoded) != 1 {
		t.Fatalf("expected 1 encoded entry, got %d (%v)", len(encoded), encoded)
	}

	// Add unknown and malformed entries to ensure DecodeNBT ignores them safely.
	encoded["test:unknown_key"] = map[string]any{"x": int32(1)}
	encoded["test:malformed"] = "not-a-map"

	var restored ecs.Bag
	ecs.DecodeNBT(&restored, encoded)
	got, ok := ecs.Get(&restored, kPersisted)
	if !ok || got != (testStats{HP: 42, Label: "knight"}) {
		t.Fatalf("restored mismatch: %+v, %v", got, ok)
	}
	if ecs.Has(&restored, kRuntimeOnly) {
		t.Fatal("runtime-only key should not be restored from NBT")
	}
}

func TestConcurrentRegistrationAndBags(t *testing.T) {
	const workers = 16
	var wg sync.WaitGroup
	keys := make([]ecs.Key[int], workers)
	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("test:race_key_%d", idx)
			k := ecs.NewKey[int](name)
			keys[idx] = k
			// Concurrent lookup of existing key.
			if k2 := ecs.NewKey[int](name); k2.ID() != k.ID() {
				t.Errorf("ID mismatch for %s: %d vs %d", name, k2.ID(), k.ID())
			}
		}(i)
	}
	wg.Wait()

	for i := range workers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var bag ecs.Bag
			for j := range 200 {
				ecs.Set(&bag, keys[idx], j)
				ecs.Update(&bag, keys[idx], func(v *int) { *v += 1 })
				got, ok := ecs.Get(&bag, keys[idx])
				if !ok || got != j+1 {
					t.Errorf("unexpected value %d (ok=%v), want %d", got, ok, j+1)
				}
			}
			clone := bag.Clone()
			if got, ok := ecs.Get(&clone, keys[idx]); !ok || got != 200 {
				t.Errorf("clone value %d (ok=%v), want 200", got, ok)
			}
		}(i)
	}
	wg.Wait()
}

func TestZeroAllocHotPaths(t *testing.T) {
	k := ecs.NewKey[testStats]("test:alloc_stats")
	var b ecs.Bag
	ecs.Set(&b, k, testStats{HP: 10, Label: "init"})

	if n := testing.AllocsPerRun(100, func() {
		v, ok := ecs.Get(&b, k)
		if !ok || v.HP != 10 {
			t.Fatal("unexpected value")
		}
	}); n != 0 {
		t.Fatalf("Get allocs = %v, want 0", n)
	}

	if n := testing.AllocsPerRun(100, func() {
		ecs.Set(&b, k, testStats{HP: 10, Label: "updated"})
	}); n != 0 {
		t.Fatalf("Set overwrite allocs = %v, want 0", n)
	}

	if n := testing.AllocsPerRun(100, func() {
		ecs.Update(&b, k, func(s *testStats) {
			s.HP = 10
		})
	}); n != 0 {
		t.Fatalf("Update allocs = %v, want 0", n)
	}

	if n := testing.AllocsPerRun(100, func() {
		if !ecs.Has(&b, k) {
			t.Fatal("expected Has true")
		}
	}); n != 0 {
		t.Fatalf("Has allocs = %v, want 0", n)
	}

	var empty ecs.Bag
	if n := testing.AllocsPerRun(100, func() {
		_ = empty.Clone()
		_ = ecs.EncodeNBT(&empty)
	}); n != 0 {
		t.Fatalf("empty Clone/EncodeNBT allocs = %v, want 0", n)
	}
}

func BenchmarkBagGetSetUpdate(b *testing.B) {
	k := ecs.NewKey[testStats]("test:bench_stats")
	var bag ecs.Bag
	ecs.Set(&bag, k, testStats{HP: 100, Label: "bench"})

	b.Run("Get", func(b *testing.B) {
		b.ReportAllocs()
		var sum int
		for range b.N {
			v, _ := ecs.Get(&bag, k)
			sum += v.HP
		}
		_ = sum
	})
	b.Run("SetOverwrite", func(b *testing.B) {
		b.ReportAllocs()
		v := testStats{HP: 42, Label: "bench"}
		for range b.N {
			ecs.Set(&bag, k, v)
		}
	})
	b.Run("Update", func(b *testing.B) {
		b.ReportAllocs()
		for range b.N {
			ecs.Update(&bag, k, func(s *testStats) {
				s.HP++
			})
		}
	})
}
