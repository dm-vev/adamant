package entity

import (
	"math"
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/parallel"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
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
	case *zombie:
		ent = v.Ent
	case *skeleton:
		ent = v.Ent
	case *Parrot:
		ent = v.Ent
	case *dolphin:
		ent = v.Ent
	case *frog:
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
		if p := findGroundPath(tx, start, goal, groundProfile{MaxVisitedNodes: 256, AllowWater: false}); len(p) != 0 {
			t.Fatalf("expected no path with AllowWater=false, got %v", p)
		}
		// Reachable when water is allowed, routes around fire, and is identical across repeated calls.
		first := findGroundPath(tx, start, goal, groundProfile{MaxVisitedNodes: 256, AllowWater: true})
		if len(first) == 0 || first[len(first)-1] != goal {
			t.Fatalf("expected valid path to %v, got %v", goal, first)
		}
		for _, pos := range first {
			if pos == (cube.Pos{6, 1, 0}) {
				t.Fatalf("expected path to avoid fire at (6,1,0), got %v", first)
			}
		}
		for i := range 5 {
			again := findGroundPath(tx, start, goal, groundProfile{MaxVisitedNodes: 256, AllowWater: true})
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
		z := e.(*zombie)
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
		z := e.(*zombie)
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
		z := e.(*zombie)
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

func TestUnreachableBackoffAvoidSunTrimPathNoPanic(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	var skelH *world.EntityHandle
	target := cube.Pos{10, 64, 0}
	var origCount int
	<-w.Exec(func(tx *world.Tx) {
		for x := -6; x <= 14; x++ {
			for z := -6; z <= 6; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		// Put a shade roof over the skeleton start so canSeeSky is false at start
		// while intermediate nodes toward target enter open sky so trimPath truncates.
		for x := -4; x <= 2; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 67, z}, block.Stone{}, nil)
			}
		}
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
		if canSeeSky(tx, cube.Pos{0, 64, 0}) {
			t.Fatalf("expected skeleton start under roof to be shaded, skyLight=%d", tx.SkyLight(cube.Pos{0, 64, 0}))
		}
		if !canSeeSky(tx, cube.Pos{6, 64, 0}) {
			t.Fatalf("expected open sky along path, skyLight=%d", tx.SkyLight(cube.Pos{6, 64, 0}))
		}

		m := testSpawnMob(t, tx, NewSkeleton(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.NoAI = true
		m.AI.onGround = true
		m.AI.nav.AvoidSun = true
		skelH = m.H()

		p := m.AI.nav.CreatePath(m, target, 0)
		if p == nil || p.CanReach() {
			t.Fatalf("expected unreachable partial path, got %v", p)
		}
		origCount = p.NodeCount()
		if !m.AI.nav.MoveTo(m, p, 1) {
			t.Fatal("expected MoveTo to accept partial path")
		}
		if p.NodeCount() >= origCount {
			t.Fatalf("expected trimPath to truncate path under open sky: before=%d after=%d", origCount, p.NodeCount())
		}
		if m.AI.nav.failPath == nil || len(m.AI.nav.failPath.nodes) != origCount {
			t.Fatalf("cached failPath mutated by trimPath: got %d nodes, want %d", len(m.AI.nav.failPath.nodes), origCount)
		}
		for i, node := range m.AI.nav.failPath.nodes {
			if node == nil {
				t.Fatalf("failPath.nodes[%d] is nil after first trimPath", i)
			}
		}
	})

	for range 5 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			e, ok := skelH.Entity(tx)
			if !ok {
				t.Fatal("skeleton missing")
			}
			sk := e.(*skeleton)
			ai, _ := sk.Ent.mobAI()
			m := ai.Mob(sk.Ent, sk)
			m.AI.onGround = true
			m.AI.nav.AvoidSun = true
			m.AI.nav.path = nil

			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil || p.CanReach() {
				t.Fatalf("expected cached unreachable partial path during backoff, got %v", p)
			}
			if p.NodeCount() != origCount {
				t.Fatalf("backed-off path node count = %d, want %d", p.NodeCount(), origCount)
			}
			for i, node := range p.nodes {
				if node == nil {
					t.Fatalf("backed-off path nodes[%d] is nil", i)
				}
			}
			if !m.AI.nav.MoveTo(m, p, 1) {
				t.Fatal("expected MoveTo during backoff to succeed without panic")
			}
			for i, node := range m.AI.nav.failPath.nodes {
				if node == nil {
					t.Fatalf("failPath.nodes[%d] became nil during backoff MoveTo", i)
				}
			}
		})
	}
}

func TestSharedPathCache(t *testing.T) {
	t.Parallel()

	t.Run("hit_and_deep_clone", func(t *testing.T) {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
		defer w.Close()

		<-w.Exec(func(tx *world.Tx) {
			for x := -2; x <= 20; x++ {
				for z := -6; z <= 6; z++ {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				}
			}
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{8, 64, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{8, 65, z}, block.Stone{}, nil)
			}
			z1 := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 0.5}}))
			z2 := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 0.5}}))
			for _, z := range []*Mob{z1, z2} {
				z.AI.NoAI = true
				z.AI.onGround = true
			}
			target := cube.Pos{15, 64, 0}

			p1 := z1.AI.nav.CreatePath(z1, target, 0)
			if p1 == nil || !p1.CanReach() {
				t.Fatalf("expected reachable path for z1, got %v", p1)
			}
			wantCount := p1.NodeCount()
			wantFirst := *p1.nodes[0]
			wantDist := p1.distToTarget

			// Mutating caller's path/nodes must not leak into the shared cache.
			p1.nodes[0].x += 99
			p1.truncate(1)

			cache := sharedPathCacheFor(tx)
			if cache == nil {
				t.Fatal("expected sharedPathCache attached to world")
			}
			visited := int(float64(z1.AI.nav.maxVisitedNodes) * z1.AI.nav.maxVisitedNodesMultiplier)
			key := makeSharedPathKey(&wantFirst, target, 0, float32(z1.AI.nav.maxPathLength(z1)), visited, &z1.AI.nav.eval)
			cached, ok := cache.lookup(tx, key)
			if !ok || cached == nil {
				t.Fatalf("expected cache lookup hit for key %+v", key)
			}
			if cached.NodeCount() != wantCount || cached.nodes[0].x != wantFirst.x || cached.distToTarget != wantDist {
				t.Fatalf("cached path corrupted by caller mutation: got count=%d x=%d dist=%v, want count=%d x=%d dist=%v",
					cached.NodeCount(), cached.nodes[0].x, cached.distToTarget, wantCount, wantFirst.x, wantDist)
			}

			z2.AI.nav.path = nil
			p2 := z2.AI.nav.CreatePath(z2, target, 0)
			if p2 == nil || !p2.CanReach() || p2.NodeCount() != wantCount || p2.nodes[0].x != wantFirst.x {
				t.Fatalf("second mob got unexpected path %v (want count=%d x=%d)", p2, wantCount, wantFirst.x)
			}
		})
	})

	t.Run("chunk_version_invalidation_across_chunks", func(t *testing.T) {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
		defer w.Close()

		<-w.Exec(func(tx *world.Tx) {
			// Spans chunk (0,0), chunk (1,0), and chunk (2,0).
			for x := 8; x <= 38; x++ {
				for z := 0; z <= 8; z++ {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				}
			}
			start := mgl64.Vec3{12.5, 64, 4.5}
			target := cube.Pos{33, 64, 4}
			z1 := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: start}))
			z2 := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: start}))
			for _, z := range []*Mob{z1, z2} {
				z.AI.NoAI = true
				z.AI.onGround = true
			}

			p1 := z1.AI.nav.CreatePath(z1, target, 0)
			if p1 == nil || !p1.CanReach() {
				t.Fatalf("expected reachable path across 3 chunks, got %v", p1)
			}
			visited := int(float64(z1.AI.nav.maxVisitedNodes) * z1.AI.nav.maxVisitedNodesMultiplier)
			key := makeSharedPathKey(&pathNodeData{x: 12, y: 64, z: 4}, target, 0, float32(z1.AI.nav.maxPathLength(z1)), visited, &z1.AI.nav.eval)
			cache := sharedPathCacheFor(tx)
			if _, ok := cache.lookup(tx, key); !ok {
				t.Fatal("expected initial cross-chunk path to be stored in cache")
			}

			// Modify only middle chunk (1, 0): place a wall forcing a detour around z=1 or z=7.
			for z := 2; z <= 6; z++ {
				tx.SetBlock(cube.Pos{20, 64, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{20, 65, z}, block.Stone{}, nil)
			}
			if _, ok := cache.lookup(tx, key); ok {
				t.Fatal("expected middle-chunk block change to invalidate cached path")
			}

			p2 := z2.AI.nav.CreatePath(z2, target, 0)
			if p2 == nil || !p2.CanReach() {
				t.Fatalf("expected recomputed detour path, got %v", p2)
			}
			for _, n := range p2.nodes {
				if n.x == 20 && n.z >= 2 && n.z <= 6 {
					t.Fatalf("detour path entered blocked wall at (%d,%d,%d)", n.x, n.y, n.z)
				}
			}
		})
	})

	t.Run("unreachable_partial_path", func(t *testing.T) {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
		defer w.Close()

		<-w.Exec(func(tx *world.Tx) {
			for x := -2; x <= 18; x++ {
				for z := -6; z <= 6; z++ {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				}
			}
			target := cube.Pos{14, 64, 0}
			for x := 13; x <= 15; x++ {
				for z := -1; z <= 1; z++ {
					if x == 14 && z == 0 {
						continue
					}
					tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 66, z}, block.Stone{}, nil)
				}
			}
			z1 := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 0.5}}))
			z2 := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 0.5}}))
			for _, z := range []*Mob{z1, z2} {
				z.AI.NoAI = true
				z.AI.onGround = true
			}

			p1 := z1.AI.nav.CreatePath(z1, target, 0)
			if p1 == nil || p1.CanReach() || p1.distToTarget <= 0 {
				t.Fatalf("expected unreachable partial path with distToTarget > 0, got %v", p1)
			}
			origNodes := p1.NodeCount()
			origDist := p1.distToTarget
			p1.truncate(1)

			p2 := z2.AI.nav.CreatePath(z2, target, 0)
			if p2 == nil || p2.CanReach() || p2.NodeCount() != origNodes || p2.distToTarget != origDist {
				t.Fatalf("cached partial path mismatch: got %v (nodes=%d dist=%v), want nodes=%d dist=%v",
					p2, p2.NodeCount(), p2.distToTarget, origNodes, origDist)
			}

			// Open wall and verify cache invalidates and produces reachable path.
			tx.SetBlock(cube.Pos{13, 64, 0}, block.Air{}, nil)
			tx.SetBlock(cube.Pos{13, 65, 0}, block.Air{}, nil)
			z2.AI.nav.path = nil
			p3 := z2.AI.nav.CreatePath(z2, target, 0)
			if p3 == nil || !p3.CanReach() {
				t.Fatalf("expected newly reachable path after breaking enclosure, got %v", p3)
			}
		})
	})

	t.Run("key_isolation_malus_blocked_maxfall_reach_config", func(t *testing.T) {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
		defer w.Close()

		<-w.Exec(func(tx *world.Tx) {
			for x := -2; x <= 20; x++ {
				for z := -6; z <= 6; z++ {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				}
			}
			// Drop cliff with height 4 at x=8..14 so maxFall=3 fails/detours while maxFall>=4 drops down.
			for x := 8; x <= 16; x++ {
				for z := -2; z <= 2; z++ {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Air{}, nil)
					tx.SetBlock(cube.Pos{x, 59, z}, block.Stone{}, nil)
				}
			}
			m1 := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 0.5}}))
			m2 := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 0.5}}))
			for _, m := range []*Mob{m1, m2} {
				m.AI.NoAI = true
				m.AI.onGround = true
			}
			target := cube.Pos{12, 60, 0}

			// m1 has no target (maxFall=3 -> cannot drop 4 blocks directly).
			pNoDrop := m1.AI.nav.CreatePath(m1, target, 0)

			// m2 has a target and full health so maxFall > 3 -> can drop 4 blocks.
			victim := testSpawnMob(t, tx, NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{12.5, 60, 0.5}}))
			victim.AI.NoAI = true
			m2.SetTarget(victim.Self)
			if m2.maxFallDistance() <= m1.maxFallDistance() {
				t.Fatalf("expected targeted healthy mob maxFallDistance (%d) > untargeted (%d)", m2.maxFallDistance(), m1.maxFallDistance())
			}
			pDrop := m2.AI.nav.CreatePath(m2, target, 0)
			if pDrop == nil || !pDrop.CanReach() {
				t.Fatalf("expected drop path to reach with higher maxFall, got %v", pDrop)
			}
			if pNoDrop != nil && pNoDrop.CanReach() && pNoDrop.NodeCount() == pDrop.NodeCount() {
				t.Fatalf("maxFall isolation failed: both returned identical %d-node reachable path", pDrop.NodeCount())
			}

			// Reach=0 vs Reach=1 must produce separate cache keys.
			flatTarget := cube.Pos{5, 64, 0}
			pR0 := m1.AI.nav.CreatePath(m1, flatTarget, 0)
			pR1 := m1.AI.nav.CreatePath(m1, flatTarget, 1)
			if pR0 == nil || pR1 == nil || pR1.nodes[pR1.NodeCount()-1].Pos() == flatTarget {
				t.Fatalf("expected reach=1 path to stop short of %v, got r0=%v r1=%v", flatTarget, pR0, pR1)
			}

			// Malus override must bypass sharedPathCache (not hit m1's cached path and not write into cache).
			cache := sharedPathCacheFor(tx)
			visited := int(float64(m1.AI.nav.maxVisitedNodes) * m1.AI.nav.maxVisitedNodesMultiplier)
			maxLen := float32(m1.AI.nav.maxPathLength(m1))
			keyMalus := makeSharedPathKey(&pathNodeData{x: 1, y: 64, z: 0}, cube.Pos{6, 64, 0}, 0, maxLen, visited, &m1.AI.nav.eval)
			m1.AI.SetPathfindingMalus(PathOpen, 2.5)
			pm := m1.AI.nav.CreatePath(m1, cube.Pos{6, 64, 0}, 0)
			if pm == nil {
				t.Fatal("expected malus mob path")
			}
			if _, ok := cache.lookup(tx, keyMalus); ok {
				t.Fatal("expected search with non-empty AI.maluses to bypass sharedPathCache store")
			}
			delete(m1.AI.maluses, PathOpen)

			// eval.blocked != nil must bypass sharedPathCache.
			m1.AI.nav.eval.blocked = func(*Mob, cube.Pos) bool { return false }
			pb := m1.AI.nav.CreatePath(m1, cube.Pos{7, 64, 0}, 0)
			if pb == nil {
				t.Fatal("expected blocked-callback mob path")
			}
			keyBlocked := makeSharedPathKey(&pathNodeData{x: 1, y: 64, z: 0}, cube.Pos{7, 64, 0}, 0, maxLen, visited, &m1.AI.nav.eval)
			if _, ok := cache.lookup(tx, keyBlocked); ok {
				t.Fatal("expected search with eval.blocked != nil to bypass sharedPathCache store")
			}
			m1.AI.nav.eval.blocked = nil
		})
	})

	t.Run("concurrent_readview_lookup_and_store", func(t *testing.T) {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
		defer w.Close()

		<-w.Exec(func(tx *world.Tx) {
			for x := -4; x <= 28; x++ {
				for z := -8; z <= 8; z++ {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
					if x%7 == 0 && z%5 != 0 {
						tx.SetBlock(cube.Pos{x, 64, z}, block.WoodFence{Wood: block.OakWood()}, nil)
					}
				}
			}
			mobs := make([]*Mob, 32)
			targets := []cube.Pos{{24, 64, 0}, {24, 64, 4}, {22, 64, -4}, {20, 64, 2}}
			for i := range mobs {
				// Reuse a small set of starts so workers race on misses, stores, and hits.
				sx := float64(i%4) + 0.5
				sz := float64((i/4)%2)*2 + 0.5
				m := testSpawnMob(t, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{sx, 64, sz}}))
				m.AI.NoAI = true
				m.AI.onGround = true
				mobs[i] = m
			}
			want := make([]*mobPath, len(mobs))
			maxLens := make([]float32, len(mobs))
			visiteds := make([]int, len(mobs))
			for i, m := range mobs {
				maxLens[i] = float32(m.AI.nav.maxPathLength(m))
				visiteds[i] = int(float64(m.AI.nav.maxVisitedNodes) * m.AI.nav.maxVisitedNodesMultiplier)
				eval := m.AI.nav.eval
				eval.pw = tx
				eval.maxFall = m.maxFallDistance()
				want[i] = findPathUncached(m, &eval, []cube.Pos{targets[i%len(targets)]}, maxLens[i], 0, visiteds[i])
			}

			got := make([]*mobPath, len(mobs)*2)
			parallel.ForWorkers(len(got), 8, func(_, idx int) {
				i := idx % len(mobs)
				m := mobs[i]
				eval := m.AI.nav.eval
				eval.pw = tx.NewReadView()
				eval.maxFall = m.maxFallDistance()
				got[idx] = findPathOpt(m, &eval, []cube.Pos{targets[i%len(targets)]}, maxLens[i], 0, visiteds[i], true)
			})
			for idx, g := range got {
				i := idx % len(mobs)
				if !pathsIdentical(g, want[i]) {
					t.Fatalf("worker %d path mismatch against uncached: got=%v want=%v", idx, g, want[i])
				}
			}
		})
	})
}

func TestSolidAtWorldAndLazyNearestVisibleLiving(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		blocks := []world.Block{
			block.Air{},
			block.Stone{},
			block.Grass{},
			block.Slab{Block: block.Stone{}},
			block.Stairs{Block: block.Stone{}},
			block.WoodFence{Wood: block.OakWood()},
			block.Carpet{Colour: item.ColourWhite()},
			block.Glass{},
			block.Chest{},
			block.RedstoneRepeater{},
			block.Water{Depth: 8, Still: true},
			block.Lava{Depth: 8, Still: true},
		}
		for i, b := range blocks {
			pos := cube.Pos{i, 64, 0}
			tx.SetBlock(pos, b, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
			got := solidAtWorld(tx, pos)
			want := false
			if !isAir(tx.Block(pos)) {
				want = legacySolid(tx, tx.Block(pos), pos)
			}
			if got != want {
				t.Fatalf("solidAtWorld(%T at %v) = %v, want %v", b, pos, got, want)
			}
		}
		rng := tx.Range()
		if solidAtWorld(tx, cube.Pos{0, rng.Min() - 1, 0}) || solidAtWorld(tx, cube.Pos{0, rng.Max() + 1, 0}) {
			t.Fatal("expected out-of-bounds Y positions to be non-solid")
		}

		// Spawn villager, nearby living zombie, and non-living item drop.
		tx.SetBlock(cube.Pos{0, 63, 4}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{3, 63, 4}, block.Stone{}, nil)
		vil := tx.AddEntity(NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 4.5}}))
		zom := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 64, 4.5}}))
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, 4.5}}, item.NewStack(item.Diamond{}, 1)))

		if tck, ok := vil.(interface{ Tick(*world.Tx, int64) }); ok {
			tck.Tick(tx, 1)
		}
		if tck, ok := zom.(interface{ Tick(*world.Tx, int64) }); ok {
			tck.Tick(tx, 1)
		}
		vm, ok := mobOf(vil)
		if !ok {
			t.Fatal("villager missing Mob")
		}
		vm.AI.NoAI = true
		zm, ok := mobOf(zom)
		if !ok {
			t.Fatal("zombie missing Mob")
		}
		zm.AI.NoAI = true

		nearestLivingEntitySensor{}.doTick(vm, vm.brain())
		nvlVal, ok := vm.brain().get(memNearestVisibleLivingEntities)
		if !ok {
			t.Fatal("expected memNearestVisibleLivingEntities populated")
		}
		nvl := nvlVal.(*nearestVisibleLiving)
		if nvl.seen != nil {
			t.Fatalf("expected nvl.seen map to stay nil until visible() is called, got %v", nvl.seen)
		}
		if len(nvl.entities) != 1 || nvl.entities[0] != zom.H() {
			t.Fatalf("expected only zombie handle in entities, got %v", nvl.entities)
		}
		_ = nvl.visible(vm, zom)
		if nvl.seen == nil || len(nvl.seen) != 1 {
			t.Fatalf("expected nvl.seen lazily initialized with 1 entry, got %v", nvl.seen)
		}

		// Re-run sensor with no nearby living mobs to ensure reusable scratch doesn't retain stale entries.
		zom.Close()
		nearestLivingEntitySensor{}.doTick(vm, vm.brain())
		nvl2Val, ok := vm.brain().get(memNearestVisibleLivingEntities)
		if !ok {
			t.Fatal("expected memNearestVisibleLivingEntities populated after second sensor tick")
		}
		nvl2 := nvl2Val.(*nearestVisibleLiving)
		if nvl2.seen != nil || len(nvl2.entities) != 0 {
			t.Fatalf("expected empty entities and nil seen after zombie removed, got len=%d seen=%v", len(nvl2.entities), nvl2.seen)
		}
	})
}

func BenchmarkDenseCrowdPathfinding(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	w.SetDifficulty(world.DifficultyHard)
	w.SetTime(18000)

	<-w.Exec(func(tx *world.Tx) {
		for x := -16; x < 16; x++ {
			for z := -16; z < 16; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 70, z}, block.Stone{}, nil)
				if x == 4 && z >= -10 && z <= 10 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.WoodFence{Wood: block.OakWood()}, nil)
				}
			}
		}
		// Keep a villager target beyond the fence wall so zombies share paths continuously.
		for _, pos := range []mgl64.Vec3{{11.5, 64, 0.5}, {11.5, 64, 6.5}} {
			v := tx.AddEntity(NewVillager(world.EntitySpawnOpts{Position: pos}))
			if vm, ok := mobOf(v); ok {
				vm.AI.NoAI = true
			}
			// Enclose villager so crowd never kills it and keeps computing routes.
			bp := cube.PosFromVec3(pos)
			for dx := -1; dx <= 1; dx++ {
				for dz := -1; dz <= 1; dz++ {
					if dx == 0 && dz == 0 {
						continue
					}
					tx.SetBlock(bp.Add(cube.Pos{dx, 0, dz}), block.Stone{}, nil)
					tx.SetBlock(bp.Add(cube.Pos{dx, 1, dz}), block.Stone{}, nil)
				}
			}
		}
		for i := range 400 {
			x := float64(-12 + (i % 12))
			z := float64(-12 + ((i / 12) % 24))
			tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{x + 0.5, 64, z + 0.5}}))
		}
	})

	// Warm up AI startup before measuring ticks.
	for range 5 {
		w.AdvanceTick()
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		w.AdvanceTick()
	}
}
