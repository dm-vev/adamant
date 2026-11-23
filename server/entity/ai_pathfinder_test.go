package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func testSpawnMob(t *testing.T, tx *world.Tx, h *world.EntityHandle) *Mob {
	t.Helper()
	e := tx.AddEntity(h)
	if ticker, ok := e.(interface{ Tick(*world.Tx, int64) }); ok {
		ticker.Tick(tx, 1)
	}
	var ent *Ent
	switch v := e.(type) {
	case *Zombie:
		ent = v.Ent
	case *Parrot:
		ent = v.Ent
	case *Dolphin:
		ent = v.Ent
	case *Frog:
		ent = v.Ent
	case *Cow:
		ent = v.Ent
	default:
		t.Fatalf("unsupported mob type %T", e)
	}
	ai, _ := ent.mobAI()
	if ai == nil {
		t.Fatalf("entity %T has no MobAI", e)
	}
	m := ai.Mob(ent, e)
	ai.Goals.Remove(m, func(Goal) bool { return true })
	return m
}

func TestPathTablesResetGrowAndWrap(t *testing.T) {
	t.Parallel()

	t.Run("pathNodeTable", func(t *testing.T) {
		var tbl pathNodeTable
		var arena pathNodeArena
		tbl.reset()
		// Force growth beyond initial 256 capacity and multiple arena slabs.
		seen := make(map[cube.Pos]*pathNodeData, 300)
		for i := range 300 {
			n := tbl.getOrAlloc(i, 64, -i, &arena)
			if n.x != i || n.y != 64 || n.z != -i || n.heapIdx != -1 {
				t.Fatalf("unexpected node %+v", n)
			}
			seen[cube.Pos{i, 64, -i}] = n
		}
		for pos, want := range seen {
			if got := tbl.getOrAlloc(pos[0], pos[1], pos[2], &arena); got != want {
				t.Fatalf("lookup mismatch for %v: got %p want %p", pos, got, want)
			}
		}
		// Simulate uint32 generation wraparound.
		tbl.gen = math.MaxUint32
		arena.reset()
		tbl.reset()
		if tbl.gen != 1 {
			t.Fatalf("expected gen wrap to 1, got %d", tbl.gen)
		}
		n := tbl.getOrAlloc(0, 64, 0, &arena)
		if n.x != 0 || n.y != 64 || n.z != 0 {
			t.Fatalf("post-wrap alloc failed: %+v", n)
		}
	})

	t.Run("pathTypeTable", func(t *testing.T) {
		var tbl pathTypeTable
		tbl.reset()
		for i := range 400 {
			k := pathNodeKey(i, 64, i*2)
			tbl.put(k, PathType(i%int(pathTypeCount)))
		}
		for i := range 400 {
			k := pathNodeKey(i, 64, i*2)
			got, ok := tbl.get(k)
			if !ok || got != PathType(i%int(pathTypeCount)) {
				t.Fatalf("key %d: got (%v, %v), want (%v, true)", i, got, ok, PathType(i%int(pathTypeCount)))
			}
		}
		tbl.gen = math.MaxUint32
		tbl.reset()
		if _, ok := tbl.get(pathNodeKey(0, 64, 0)); ok {
			t.Fatal("stale entry survived generation wrap")
		}
	})

	t.Run("pathFloorTable", func(t *testing.T) {
		var tbl pathFloorTable
		tbl.reset()
		for i := range 200 {
			tbl.put(pathNodeKey(i, 10, -i), float64(i)+0.5)
		}
		for i := range 200 {
			got, ok := tbl.get(pathNodeKey(i, 10, -i))
			if !ok || got != float64(i)+0.5 {
				t.Fatalf("floor %d: got (%v, %v)", i, got, ok)
			}
		}
		tbl.gen = math.MaxUint32
		tbl.reset()
		if _, ok := tbl.get(pathNodeKey(0, 10, 0)); ok {
			t.Fatal("stale floor entry survived generation wrap")
		}
	})

	t.Run("groundNodeTableAndWalkTable", func(t *testing.T) {
		var nt groundNodeTable
		var arena groundNodeArena
		nt.reset()
		for i := range 300 {
			pos := cube.Pos{i, 1, -i}
			nt.put(pos, arena.alloc(pos, float64(i), float64(i), nil))
		}
		for i := range 300 {
			n := nt.get(cube.Pos{i, 1, -i})
			if n == nil || n.g != float64(i) {
				t.Fatalf("ground node %d lost value: got %v", i, n)
			}
		}
		nt.gen = math.MaxUint32
		arena.reset()
		nt.reset()
		if n := nt.get(cube.Pos{0, 1, 0}); n != nil {
			t.Fatalf("stale ground node survived wrap: %+v", n)
		}
	})
}

func TestPathScratchReentrantFallback(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		s1, c1 := acquirePathScratch(tx)
		s2, c2 := acquirePathScratch(tx)
		if s1 == s2 {
			t.Fatal("re-entrant acquirePathScratch returned the same scratch instance")
		}
		if !s2.fromPool || s1.fromPool {
			t.Fatalf("unexpected pool flags: s1.fromPool=%v s2.fromPool=%v", s1.fromPool, s2.fromPool)
		}
		if c1 == nil || c1 != c2 {
			t.Fatal("expected shared per-world pathTypeCache")
		}
		releasePathScratch(s2)
		releasePathScratch(s1)

		// Next acquisition should reuse the world-local scratch.
		s3, _ := acquirePathScratch(tx)
		if s3 != s1 || s3.fromPool {
			t.Fatal("world-local scratch was not reused after release")
		}
		releasePathScratch(s3)
	})
}

func TestPathTypeCacheInvalidationAndNeighbourOrder(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		// Build floor spanning chunk boundary at x=15..16 and z=15..16, plus interior x=5, z=5.
		for x := 3; x <= 18; x++ {
			for z := 3; z <= 18; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}

		positions := []cube.Pos{
			{5, 64, 5},   // interior
			{15, 64, 5},  // +x chunk boundary
			{16, 64, 5},  // -x chunk boundary
			{15, 64, 15}, // corner chunk boundary
			{16, 64, 16}, // corner chunk boundary
		}

		// Verify all positions match the uncached fallback reader on clean terrain.
		for _, pos := range positions {
			var cached, uncached pathStateReader
			cached.initFromWorld(tx)
			uncached.init(tx, nil)
			got := cached.pathTypeStatic(pos)
			want := uncached.pathTypeStatic(pos)
			if got != want || got != PathWalkable {
				t.Fatalf("pos %v: cached=%v uncached=%v want=PathWalkable", pos, got, want)
			}
		}

		// Place hazards across chunk boundary (x=16, z=15) and interior (x=4, z=5)
		// and verify cache invalidates immediately and preserves exact priority order.
		tx.SetBlock(cube.Pos{4, 64, 4}, block.Cactus{}, nil)          // dx=-1, dz=-1 -> PathDamagingInNeighbour
		tx.SetBlock(cube.Pos{6, 64, 6}, block.Fire{}, nil)            // dx=+1, dz=+1 -> PathFireInNeighbour (lower scan order)
		tx.SetBlock(cube.Pos{16, 64, 15}, block.Water{Depth: 8}, nil) // across +x chunk border from (15,64,15)
		tx.SetBlock(cube.Pos{16, 64, 14}, block.Lava{Depth: 8}, nil)  // earlier dz than water for (15,64,15)
		tx.SetBlock(cube.Pos{17, 64, 16}, block.Fire{}, nil)          // across boundary from (16,64,16)

		for _, pos := range positions {
			var cached, uncached pathStateReader
			cached.initFromWorld(tx)
			uncached.init(tx, nil)
			got := cached.pathTypeStatic(pos)
			want := uncached.pathTypeStatic(pos)
			if got != want {
				t.Fatalf("after hazard mutation at %v: cached=%v != uncached=%v", pos, got, want)
			}
		}

		// Verify specific expected hazard priorities (dx, dy, dz scan order).
		if got := pathTypeStatic(tx, cube.Pos{5, 64, 5}); got != PathDamagingInNeighbour {
			t.Fatalf("(5,64,5): got %v want PathDamagingInNeighbour", got)
		}
		if got := pathTypeStatic(tx, cube.Pos{15, 64, 15}); got != PathFireInNeighbour {
			t.Fatalf("(15,64,15): got %v want PathFireInNeighbour", got)
		}

		// Mutate lava back to air -> (15,64,15) must immediately reflect PathWaterBorder.
		tx.SetBlock(cube.Pos{16, 64, 14}, block.Air{}, nil)
		if got := pathTypeStatic(tx, cube.Pos{15, 64, 15}); got != PathWaterBorder {
			t.Fatalf("after clearing lava at (16,64,14): got %v want PathWaterBorder", got)
		}
	})
}

func TestPathfindRepeatDeterminismAndReuse(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		for x := -2; x <= 14; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		// Add fence wall with gap + step up.
		for z := -3; z <= 3; z++ {
			if z == 2 {
				continue
			}
			tx.SetBlock(cube.Pos{5, 64, z}, block.WoodFence{Wood: block.OakWood()}, nil)
		}
		tx.SetBlock(cube.Pos{9, 64, 0}, block.Stone{}, nil)

		zombie := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		zombie.AI.onGround = true
		target := cube.Pos{11, 64, 0}

		first := zombie.AI.nav.CreatePath(zombie, target, 0)
		if first == nil || !first.CanReach() {
			t.Fatalf("expected reachable walk path, got %v", first)
		}
		for i := range 10 {
			zombie.AI.nav.path = nil
			again := zombie.AI.nav.CreatePath(zombie, target, 0)
			if !first.sameAs(again) {
				t.Fatalf("walk path diverged on repeat %d", i)
			}
		}

		// Fly path reuse check.
		parrot := testSpawnMob(t, tx, NewParrot(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 68, 0.5}}))
		flyTarget := cube.Pos{10, 71, 2}
		flyFirst := parrot.AI.nav.CreatePath(parrot, flyTarget, 0)
		if flyFirst == nil || !flyFirst.CanReach() {
			t.Fatalf("expected reachable fly path, got %v", flyFirst)
		}
		for i := range 5 {
			parrot.AI.nav.path = nil
			if !flyFirst.sameAs(parrot.AI.nav.CreatePath(parrot, flyTarget, 0)) {
				t.Fatalf("fly path diverged on repeat %d", i)
			}
		}
	})
}

func TestPathfindSwimAndAmphibiousReuse(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		for x := -2; x <= 14; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 59, z}, block.Stone{}, nil)
				for y := 60; y <= 63; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true}, nil)
				}
			}
		}
		// Island at x=11..13, y=63 for amphibious exit.
		for x := 11; x <= 13; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}

		dolphin := testSpawnMob(t, tx, NewDolphin(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 61.5, 0.5}}))
		dolphin.AI.waterHeight = 1
		swimTarget := cube.Pos{9, 62, 1}
		swimFirst := dolphin.AI.nav.CreatePath(dolphin, swimTarget, 0)
		if swimFirst == nil || !swimFirst.CanReach() {
			t.Fatalf("expected reachable swim path, got %v", swimFirst)
		}
		for i := range 5 {
			dolphin.AI.nav.path = nil
			if !swimFirst.sameAs(dolphin.AI.nav.CreatePath(dolphin, swimTarget, 0)) {
				t.Fatalf("swim path diverged on repeat %d", i)
			}
		}

		frog := testSpawnMob(t, tx, NewFrog(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 62, 0.5}}))
		frog.AI.waterHeight = 1
		amphTarget := cube.Pos{12, 64, 0}
		amphFirst := frog.AI.nav.CreatePath(frog, amphTarget, 0)
		if amphFirst == nil || !amphFirst.CanReach() {
			t.Fatalf("expected reachable amphibious path, got %v", amphFirst)
		}
		for i := range 5 {
			frog.AI.nav.path = nil
			if !amphFirst.sameAs(frog.AI.nav.CreatePath(frog, amphTarget, 0)) {
				t.Fatalf("amphibious path diverged on repeat %d", i)
			}
		}
	})
}

func TestFindGroundPathWaterFireAndReuse(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		for x := -1; x <= 8; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
			}
		}
		// Wall across x=3 with an open gap at z=0.
		for z := -2; z <= 2; z++ {
			if z == 0 {
				continue
			}
			tx.SetBlock(cube.Pos{3, 1, z}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{3, 2, z}, block.Stone{}, nil)
		}
		// Water trench at x=5.
		for z := -2; z <= 2; z++ {
			tx.SetBlock(cube.Pos{5, 1, z}, block.Water{Depth: 8, Still: true}, nil)
		}
		// Fire on the direct line after the water trench.
		tx.SetBlock(cube.Pos{6, 1, 0}, block.Fire{}, nil)

		start, goal := cube.Pos{0, 1, 0}, cube.Pos{7, 1, 0}
		// Blocked when water is disallowed.
		if p := FindGroundPath(tx, start, goal, GroundProfile{MaxVisitedNodes: 256, AllowWater: false}); len(p) != 0 {
			t.Fatalf("expected no path with AllowWater=false, got %v", p)
		}
		// Reachable when water is allowed, routes around fire, and is identical across repeated calls.
		first := FindGroundPath(tx, start, goal, GroundProfile{MaxVisitedNodes: 256, AllowWater: true})
		if len(first) == 0 || first[len(first)-1] != goal {
			t.Fatalf("expected valid path to %v, got %v", goal, first)
		}
		for _, pos := range first {
			if pos == (cube.Pos{6, 1, 0}) {
				t.Fatalf("expected path to avoid fire at (6,1,0), got %v", first)
			}
		}
		for i := range 5 {
			again := FindGroundPath(tx, start, goal, GroundProfile{MaxVisitedNodes: 256, AllowWater: true})
			if len(again) != len(first) {
				t.Fatalf("repeat %d length mismatch: %d vs %d", i, len(again), len(first))
			}
			for j := range first {
				if again[j] != first[j] {
					t.Fatalf("repeat %d pos %d mismatch: %v vs %v", i, j, again[j], first[j])
				}
			}
		}
	})
}

func TestUnreachablePathBackoffAndInvalidation(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	var zombieH *world.EntityHandle
	target := cube.Pos{10, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 14; x++ {
			for z := -6; z <= 6; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		// Enclose target in a 3x3 stone box.
		for x := 9; x <= 11; x++ {
			for z := -1; z <= 1; z++ {
				if x == 10 && z == 0 {
					continue
				}
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 66, z}, block.Stone{}, nil)
			}
		}
		m := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.NoAI = true
		m.AI.onGround = true
		zombieH = m.H()

		p := m.AI.nav.CreatePath(m, target, 0)
		if p == nil || p.CanReach() {
			t.Fatalf("expected unreachable partial path, got %v", p)
		}
		if m.AI.nav.failStreak != 1 {
			t.Fatalf("failStreak = %d, want 1", m.AI.nav.failStreak)
		}
		firstFailTick := m.AI.nav.failTick
		if m.AI.nav.failUntil != firstFailTick+20 {
			t.Fatalf("failUntil = %d, want %d", m.AI.nav.failUntil, firstFailTick+20)
		}
	})

	// Advance 5 ticks (within 20-tick backoff): CreatePath should return cached unreachable path without escalating streak.
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := zombieH.Entity(tx)
		z := e.(*Zombie)
		ai, _ := z.Ent.mobAI()
		m := ai.Mob(z.Ent, z)
		m.AI.onGround = true
		m.AI.nav.path = nil
		prevTick := m.AI.nav.failTick
		p := m.AI.nav.CreatePath(m, target, 0)
		if p == nil || p.CanReach() {
			t.Fatalf("expected backed-off partial path, got %v", p)
		}
		if m.AI.nav.failStreak != 1 || m.AI.nav.failTick != prevTick {
			t.Fatalf("expected backoff hit (streak=1, failTick=%d), got streak=%d failTick=%d", prevTick, m.AI.nav.failStreak, m.AI.nav.failTick)
		}
	})

	// Advance past initial 20-tick cooldown: retry should escalate streak to 2 with 40-tick cooldown.
	for range 16 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		e, _ := zombieH.Entity(tx)
		z := e.(*Zombie)
		ai, _ := z.Ent.mobAI()
		m := ai.Mob(z.Ent, z)
		m.AI.onGround = true
		m.AI.nav.path = nil
		p := m.AI.nav.CreatePath(m, target, 0)
		if p == nil || p.CanReach() {
			t.Fatalf("expected unreachable path on retry, got %v", p)
		}
		if m.AI.nav.failStreak != 2 || m.AI.nav.failUntil != m.GameTime()+40 {
			t.Fatalf("expected escalated streak=2 and +40 cooldown, got streak=%d until=%d now=%d", m.AI.nav.failStreak, m.AI.nav.failUntil, m.GameTime())
		}

		// Break the wall in the same chunk: backoff must invalidate immediately on the next tick.
		tx.SetBlock(cube.Pos{9, 64, 0}, block.Air{}, nil)
		tx.SetBlock(cube.Pos{9, 65, 0}, block.Air{}, nil)
	})

	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		e, _ := zombieH.Entity(tx)
		z := e.(*Zombie)
		ai, _ := z.Ent.mobAI()
		m := ai.Mob(z.Ent, z)
		m.AI.onGround = true
		m.AI.nav.path = nil
		p := m.AI.nav.CreatePath(m, target, 0)
		if p == nil || !p.CanReach() {
			t.Fatalf("expected newly reachable path after breaking wall, got %v", p)
		}
		if m.AI.nav.failStreak != 0 {
			t.Fatalf("expected failStreak cleared after reachable path, got %d", m.AI.nav.failStreak)
		}
	})
}
