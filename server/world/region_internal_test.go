package world

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// regionProbeConfig makes a testEntity call tick when it ticks.
type regionProbeConfig struct {
	tick func(tx *Tx, e *testEntity)
}

func (c regionProbeConfig) Apply(data *EntityData) { data.Data = c }

// TestRegionPhaseKeepsRegionsApart ticks two entities far apart in regions
// and checks that each ticks in a region of its own, that neither finds the
// other, reads the other's columns or writes them, and that an entity moved
// or added into the other region's columns is put there by the owner after
// the phase. As without regions, an entity that moved in its tick changes
// columns when it ticks next.
func TestRegionPhaseKeepsRegionsApart(t *testing.T) {
	w := Config{Synchronous: true, Regions: true}.New()
	defer w.Close()

	far := mgl64.Vec3{1024 + 40.5, 4, 8.5}
	farBlock := cube.PosFromVec3(far).Side(cube.FaceDown)
	var (
		a, b, added *EntityHandle
		aRegion     *region
		bRegion     *region
		aTicks      int
		bTicks      int
	)
	a = EntitySpawnOpts{Position: mgl64.Vec3{8.5, 4, 8.5}}.New(testEntityType{}, regionProbeConfig{tick: func(tx *Tx, e *testEntity) {
		aTicks++
		aRegion = tx.region
		if aTicks > 1 {
			return
		}
		if _, ok := b.Entity(tx); ok {
			t.Error("entity of region A found the entity of region B")
		}
		for range tx.EntitiesWithin(cube.Box(1000, 0, 0, 1100, 10, 20)) {
			t.Error("region A found an entity in the columns of region B")
		}
		if _, ok := tx.BlockLoaded(farBlock); ok {
			t.Error("region A read a block of region B")
		}
		// Changing a block of region B is left to the owner.
		tx.SetBlock(farBlock, w.conf.Blocks.BlockByRuntimeIDOrAir(1), nil)
		// Moving into region B and adding an entity there as well.
		e.data.Pos = far
		added = tx.AddEntity(EntitySpawnOpts{Position: far.Add(mgl64.Vec3{1, 0, 0})}.New(testEntityType{}, testEntityConfig{})).H()
	}})
	b = EntitySpawnOpts{Position: mgl64.Vec3{1024 + 8.5, 4, 8.5}}.New(testEntityType{}, regionProbeConfig{tick: func(tx *Tx, e *testEntity) {
		bTicks++
		bRegion = tx.region
		if bTicks == 1 {
			if _, ok := a.Entity(tx); ok {
				t.Error("entity of region B found the entity of region A")
			}
		}
	}})
	// An entity staying in the first region keeps the regions apart once a
	// leaves it.
	stay := EntitySpawnOpts{Position: mgl64.Vec3{8.5, 4, 40.5}}.New(testEntityType{}, regionProbeConfig{tick: func(*Tx, *testEntity) {}})
	<-w.Exec(func(tx *Tx) {
		tx.chunk(chunkPosFromVec3(far))
		tx.AddEntity(a)
		tx.AddEntity(b)
		tx.AddEntity(stay)
	})
	w.AdvanceTick()

	if w.RegionPhases() != 1 {
		t.Fatalf("%d region phases, want 1", w.RegionPhases())
	}
	if aRegion == nil || bRegion == nil || aRegion == bRegion {
		t.Fatalf("entities ticked in regions %p and %p, want two", aRegion, bRegion)
	}
	if aTicks != 1 || bTicks != 1 {
		t.Fatalf("entities ticked %d and %d times, want once each", aTicks, bTicks)
	}
	farChunk := chunkPosFromVec3(far)
	listed := func(h *EntityHandle, want ChunkPos) {
		t.Helper()
		<-w.Exec(func(tx *Tx) {
			if h == nil || h.state == nil {
				t.Fatal("entity left the world")
			}
			if h.state.pos != want || !containsHandle(tx.chunk(want).Entities, h) {
				t.Errorf("entity listed in chunk %v, want %v", h.state.pos, want)
			}
		})
	}
	listed(added, farChunk)
	listed(a, ChunkPos{0, 0})
	<-w.Exec(func(tx *Tx) {
		if got := tx.Block(farBlock); w.conf.Blocks.BlockRuntimeID(got) != 1 {
			t.Errorf("block of region B set by region A is %v after the phase", got)
		}
		if _, ok := b.Entity(tx); !ok {
			t.Error("owner did not find the entity of region B")
		}
	})

	// The entity found in another region's chunk next tick is moved by the
	// owner instead of ticking there.
	w.AdvanceTick()
	if aTicks != 1 {
		t.Errorf("entity moving into another region ticked %d times, want 1", aTicks)
	}
	listed(a, farChunk)
	<-w.Exec(func(tx *Tx) {
		if containsHandle(tx.chunk(ChunkPos{0, 0}).Entities, a) {
			t.Error("moved entity still listed in its old chunk")
		}
	})
	// From then on it ticks in the region of its new chunk.
	w.AdvanceTick()
	if aTicks != 2 || aRegion == nil || aRegion != bRegion {
		t.Errorf("moved entity ticked %d times, in region %p, want twice in region %p", aTicks, aRegion, bRegion)
	}
	if w.RegionPhases() != 3 {
		t.Errorf("%d region phases in 3 ticks, want 3", w.RegionPhases())
	}
}

func containsHandle(handles []*EntityHandle, h *EntityHandle) bool {
	for _, x := range handles {
		if x == h {
			return true
		}
	}
	return false
}

// TestRegionLockNests checks the shared lock of a region may be taken again
// by the region holding it and is free once every lock is undone.
func TestRegionLockNests(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	r := &region{}
	tx := &Tx{w: w, region: r}
	tx.lockShared()
	tx.lockShared()
	tx.unlockShared()
	if w.regions.mu.TryLock() {
		t.Fatal("shared lock free while still held once")
	}
	tx.unlockShared()
	if !w.regions.mu.TryLock() {
		t.Fatal("shared lock still held after every unlock")
	}
	w.regions.mu.Unlock()
	// A transaction of the owner takes no lock.
	owner := &Tx{w: w}
	owner.lockShared()
	if !w.regions.mu.TryLock() {
		t.Fatal("owner transaction took the shared lock")
	}
	w.regions.mu.Unlock()
	owner.unlockShared()
}

// TestRegionOwnersNeverMeet checks no column is owned by two regions for
// random layouts of columns.
func TestRegionOwnersNeverMeet(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	seed := uint64(1)
	next := func(n int) int32 {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int32((seed>>33)%uint64(n)) - int32(n/2)
	}
	for layout := 0; layout < 200; layout++ {
		var refs []columnRef
		for range 1 + layout%12 {
			refs = append(refs, columnRef{pos: ChunkPos{next(160), next(160)}})
		}
		groups := regionsOf(refs)
		cells := map[regionCellPos]*region{}
		regions := make([]*region, len(groups))
		for i, g := range groups {
			regions[i] = &region{index: i}
			for _, ref := range g {
				cells[regionCellOf(ref.pos)] = regions[i]
			}
		}
		w.regions.cells = cells
		for i, g := range groups {
			for _, ref := range g {
				if owner := w.regionOwner(ref.pos); owner != regions[i] {
					t.Fatalf("layout %d: column %v of region %d owned by %v", layout, ref.pos, i, owner)
				}
			}
		}
		for x := int32(-100); x <= 100; x += 3 {
			for z := int32(-100); z <= 100; z += 3 {
				owners := 0
				for _, r := range regions {
					if w.regionOwnerAmong(ChunkPos{x, z}, r) {
						owners++
					}
				}
				if owners > 1 {
					t.Fatalf("layout %d: column %v owned by %d regions", layout, ChunkPos{x, z}, owners)
				}
			}
		}
	}
	w.regions.cells = nil
}

// regionOwnerAmong reports whether r owns the column at pos by the cells
// of r alone, regardless of the other regions.
func (w *World) regionOwnerAmong(pos ChunkPos, r *region) bool {
	for cell, owner := range w.regions.cells {
		if owner != r {
			continue
		}
		minX, minZ := cell[0]*regionCell-regionReach, cell[1]*regionCell-regionReach
		maxX, maxZ := cell[0]*regionCell+regionCell-1+regionReach, cell[1]*regionCell+regionCell-1+regionReach
		if pos[0] >= minX && pos[0] <= maxX && pos[1] >= minZ && pos[1] <= maxZ {
			return true
		}
	}
	return false
}

// TestRegionPhasesSplitCrowdedRegions checks a crowded region ticks its
// cells in phases in which no two regions own columns that meet, and that
// regions are split only where it shortens the tick.
func TestRegionPhasesSplitCrowdedRegions(t *testing.T) {
	ref := func(cx, cz int32, entities int) columnRef {
		pos := ChunkPos{cx*regionCell + 3, cz*regionCell + 5}
		return columnRef{pos: pos, col: &Column{Entities: make([]*EntityHandle, entities)}}
	}
	check := func(name string, refs []columnRef, phases [][][]columnRef) {
		t.Helper()
		seen := map[ChunkPos]int{}
		for _, regions := range phases {
			for i, a := range regions {
				for _, ref := range a {
					seen[ref.pos]++
				}
				for _, b := range regions[i+1:] {
					for _, ra := range a {
						for _, rb := range b {
							ca, cb := regionCellOf(ra.pos), regionCellOf(rb.pos)
							if max(abs32(ca[0]-cb[0]), abs32(ca[1]-cb[1])) < 2 {
								t.Fatalf("%s: regions of one phase hold cells %v and %v next to each other", name, ca, cb)
							}
						}
					}
				}
			}
		}
		for _, ref := range refs {
			if seen[ref.pos] != 1 {
				t.Fatalf("%s: column %v in %d regions, want 1", name, ref.pos, seen[ref.pos])
			}
		}
	}
	regions := func(phases [][][]columnRef) []int {
		var out []int
		for _, regions := range phases {
			out = append(out, len(regions))
		}
		return out
	}

	var crowd []columnRef
	for x := range int32(3) {
		for z := range int32(3) {
			crowd = append(crowd, ref(x, z, 100))
		}
	}
	phases := regionPhasesOf(crowd, 16)
	check("crowd", crowd, phases)
	if got := regions(phases); len(got) != 4 || !slices.Equal(slices.Sorted(slices.Values(got)), []int{1, 2, 2, 4}) {
		t.Errorf("crowd of 3 by 3 cells ticks in regions %v by phase, want 4, 2, 2 and 1", got)
	}

	pair := []columnRef{ref(0, 0, 100), ref(1, 0, 100)}
	phases = regionPhasesOf(pair, 16)
	check("pair", pair, phases)
	if got := regions(phases); !slices.Equal(got, []int{1}) {
		t.Errorf("two cells side by side tick in regions %v by phase, want one region", got)
	}

	apart := []columnRef{ref(0, 0, 100), ref(5, 0, 80), ref(0, 5, 60)}
	phases = regionPhasesOf(apart, 16)
	check("apart", apart, phases)
	if got := regions(phases); !slices.Equal(got, []int{3}) {
		t.Errorf("regions far apart tick in regions %v by phase, want 3 in one phase", got)
	}

	// The cells of a crowd split, a region far away ticks along with a
	// phase rather than in one of its own.
	mixed := append(slices.Clone(crowd), ref(10, 10, 100))
	phases = regionPhasesOf(mixed, 16)
	check("mixed", mixed, phases)
	if got := regions(phases); len(got) != 4 {
		t.Errorf("crowd and a region far away tick in regions %v by phase, want four phases", got)
	}

	// A region far away with more entities than the crowd keeps the crowd
	// whole: splitting it would leave the workers waiting.
	heavy := append(slices.Clone(crowd), ref(10, 10, 5000))
	phases = regionPhasesOf(heavy, 16)
	check("heavy", heavy, phases)
	if got := regions(phases); !slices.Equal(got, []int{2}) {
		t.Errorf("crowd beside a heavier region ticks in regions %v by phase, want both whole in one phase", got)
	}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
