package entity

import (
	"fmt"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/parallel"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestPrefetchMatchesLiveReads ticks well over prefetchMinimum mobs over
// water, lava, fences, slabs, stairs, ladders and grass, while an entity
// changes blocks among them during the entity ticks. With verifyPrefetch on,
// every use of a prefetched result is checked against reading the world,
// so a result used after a block it depends on changed fails the test.
func TestPrefetchMatchesLiveReads(t *testing.T) {
	verifyPrefetch, verifiedFluids, verifiedCollisions = true, 0, 0
	defer func() { verifyPrefetch = false }()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	const size = 40
	used := 0
	<-w.Exec(func(tx *world.Tx) {
		for x := -size; x < size; x++ {
			for z := -size; z < size; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				var top world.Block
				switch {
				case x < -20 && z < -20:
					tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Depth: 8, Still: true}, nil)
				case x > 20 && z < -20:
					tx.SetBlock(cube.Pos{x, 63, z}, block.Lava{Depth: 8, Still: true}, nil)
				case x%7 == 0 && z%5 != 0:
					top = block.WoodFence{Wood: block.OakWood()}
				case (x+z)%11 == 0:
					top = block.Slab{Block: block.Stone{}}
				case (x*3+z)%13 == 0:
					top = block.Stairs{Block: block.Stone{}}
				case (x-z)%17 == 0:
					top = block.Ladder{Facing: cube.North}
				case x > 0 && z > 10:
					tx.SetBlock(cube.Pos{x, 63, z}, block.Grass{}, nil)
				}
				if top != nil {
					tx.SetBlock(cube.Pos{x, 64, z}, top, nil)
				}
			}
		}
		spawn := []func(world.EntitySpawnOpts) *world.EntityHandle{
			NewCow, NewSheep, NewZombie, NewSkeleton, NewVillager, NewSpider, NewCreeper, NewEnderman, NewPig, NewStrider, NewSquid,
		}
		for x := -size + 2; x < size-2; x += 4 {
			for z := -size + 2; z < size-2; z += 4 {
				f := spawn[used%len(spawn)]
				tx.AddEntity(f(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(x) + .5, 64, float64(z) + .5}}))
				used++
			}
		}
		for cx := -size; cx < size; cx += 16 {
			for cz := -size; cz < size; cz += 16 {
				tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(cx) + 8.5, 70, float64(cz) + 8.5}}.New(blockFlipperType{}, blockFlipperConfig{}))
			}
		}
	})
	if used < prefetchMinimumForTest {
		t.Fatalf("only %d mobs, too few for the parallel phase", used)
	}
	for range 200 {
		w.AdvanceTick()
	}
	// Most reads come from the prefetch; the rest were invalidated or not
	// covered.
	if verifiedFluids < 100*used || verifiedCollisions < 50*used {
		t.Fatalf("only %d fluid and %d collision prefetches were used for %d mobs over 200 ticks", verifiedFluids, verifiedCollisions, used)
	}
	t.Logf("verified %d fluid and %d collision prefetches", verifiedFluids, verifiedCollisions)
}

// prefetchMinimumForTest mirrors the world's prefetchMinimum.
const prefetchMinimumForTest = 64

// blockFlipper toggles blocks around it on every tick, changing what the
// mobs ticking after it in the same tick read.
type blockFlipper struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
}

func (b *blockFlipper) H() *world.EntityHandle { return b.handle }
func (b *blockFlipper) Position() mgl64.Vec3   { return b.data.Pos }
func (b *blockFlipper) Rotation() cube.Rotation {
	return b.data.Rot
}
func (b *blockFlipper) Close() error { return nil }

// Tick swaps the blocks of a ring of positions around the flipper between
// air, water, a fence and a slab.
func (b *blockFlipper) Tick(tx *world.Tx, current int64) {
	base := cube.PosFromVec3(b.data.Pos)
	for i := range 16 {
		pos := cube.Pos{base[0] - 8 + i, 64, base[2] + int(current%8) - 4}
		var bl world.Block
		switch (current + int64(i)) % 4 {
		case 0:
			bl = block.Air{}
		case 1:
			bl = block.Water{Depth: 8, Still: true}
		case 2:
			bl = block.WoodFence{Wood: block.SpruceWood()}
		case 3:
			bl = block.Slab{Block: block.Stone{}, Top: true}
		}
		tx.SetBlock(pos, bl, nil)
	}
}

type blockFlipperConfig struct{}

func (blockFlipperConfig) Apply(*world.EntityData) {}

type blockFlipperType struct{}

func (blockFlipperType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &blockFlipper{tx: tx, handle: handle, data: data}
}
func (blockFlipperType) EncodeEntity() string { return "dragonfly:block_flipper" }
func (blockFlipperType) BBox(world.Entity) cube.BBox {
	return cube.Box(-.1, 0, -.1, .1, .1, .1)
}
func (blockFlipperType) DecodeNBT(map[string]any, *world.EntityData) {}
func (blockFlipperType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

// TestReadViewMatchesTransactionForEveryBlock places every registered block
// state in a dense grid, so models see all sorts of neighbours, and checks
// that reading through a world's read view gives the collision boxes,
// liquids and liquid hints a transaction gives. The read view reads on all
// cores at once, as in the parallel phase.
func TestReadViewMatchesTransactionForEveryBlock(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	blocks := world.DefaultBlockRegistry.Blocks()
	side := 1
	for side*side < len(blocks) {
		side++
	}
	opts := &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true}
	var positions []cube.Pos
	<-w.Exec(func(tx *world.Tx) {
		for i, b := range blocks {
			if _, unknown := b.(interface{ Unknown() }); unknown {
				continue
			}
			pos := cube.Pos{i % side, 64 + (i/side)%2, i / side}
			tx.SetBlock(pos, b, opts)
			positions = append(positions, pos)
		}
		type result struct {
			boxes  []cube.BBox
			liquid world.Liquid
			ok     bool
		}
		want := make([]result, len(positions))
		skip := make([]bool, len(positions))
		for i, pos := range positions {
			func() {
				defer func() {
					// Registry templates of some blocks, such as shulker
					// boxes, have no model without the state their
					// constructors make.
					if recover() != nil {
						skip[i] = true
					}
				}()
				l, ok := tx.Liquid(pos)
				want[i] = result{boxes: tx.Block(pos).Model().BBox(pos, tx), liquid: l, ok: ok}
			}()
		}
		mismatches := make([]string, len(positions))
		var compared, missed atomic.Int64
		parallel.ForWorkers(len(positions), 8, func(_, i int) {
			if skip[i] {
				return
			}
			v := tx.NewReadView()
			pos := positions[i]
			boxes := v.Block(pos).Model().BBox(pos, v)
			l, ok := v.Liquid(pos)
			hint := v.MayContainLiquid(pos, pos)
			if v.Missed() {
				// Blocks whose data the transaction creates on reading are
				// left to the owner.
				missed.Add(1)
				return
			}
			compared.Add(1)
			switch {
			case !slices.Equal(boxes, want[i].boxes):
				mismatches[i] = fmt.Sprintf("%v at %v: boxes %v, want %v", v.Block(pos), pos, boxes, want[i].boxes)
			case ok != want[i].ok || ok && l != want[i].liquid:
				mismatches[i] = fmt.Sprintf("%v at %v: liquid %v %v, want %v %v", v.Block(pos), pos, l, ok, want[i].liquid, want[i].ok)
			case want[i].ok && !hint:
				mismatches[i] = fmt.Sprintf("%v at %v: holds %v but MayContainLiquid is false", v.Block(pos), pos, want[i].liquid)
			}
		})
		for _, m := range mismatches {
			if m != "" {
				t.Error(m)
			}
		}
		t.Logf("%d block states placed, %d compared, %d left to the owner", len(positions), compared.Load(), missed.Load())
		if compared.Load() < int64(len(positions))*9/10 {
			t.Errorf("only %d of %d block states compared", compared.Load(), len(positions))
		}
	})
	if len(positions) < 1000 {
		t.Fatalf("only %d block states placed", len(positions))
	}
}

// TestPrefetchEveryMobType ticks a few of every mob spawnMob makes over
// stone, water and grass with verifyPrefetch on, so the parallel phase reads
// the boxes and surroundings of each species.
func TestPrefetchEveryMobType(t *testing.T) {
	verifyPrefetch, verifiedFluids, verifiedCollisions = true, 0, 0
	defer func() { verifyPrefetch = false }()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	var ids []string
	for _, et := range DefaultRegistry.Types() {
		ids = append(ids, et.EncodeEntity())
	}
	slices.Sort(ids)
	spawned := 0
	<-w.Exec(func(tx *world.Tx) {
		for x := -48; x < 48; x++ {
			for z := -48; z < 48; z++ {
				var b world.Block = block.Stone{}
				switch {
				case x < -16:
					b = block.Water{Depth: 8, Still: true}
				case x > 16:
					b = block.Grass{}
				}
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 64, z}, b, nil)
			}
		}
		for i, id := range ids {
			for j := range 3 {
				pos := mgl64.Vec3{float64(i%24*4-46) + .5, 66, float64(i/24*12+j*4-46) + .5}
				if h := spawnMob(id, world.EntitySpawnOpts{Position: pos}); h != nil {
					tx.AddEntity(h)
					spawned++
				}
			}
		}
	})
	if spawned < prefetchMinimumForTest {
		t.Fatalf("only %d mobs spawned", spawned)
	}
	for range 60 {
		w.AdvanceTick()
	}
	t.Logf("%d mobs of %d types, verified %d fluid and %d collision prefetches", spawned, len(ids), verifiedFluids, verifiedCollisions)
	if verifiedFluids == 0 || verifiedCollisions == 0 {
		t.Fatal("no prefetched result was used")
	}
}

// TestPathTypeCacheFollowsBlockChanges fills the PathTypeCache of a world,
// changes blocks among the cached positions, and checks every position
// still reads what computing its type afresh gives.
func TestPathTypeCacheFollowsBlockChanges(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	palette := []world.Block{block.Air{}, block.Stone{}, block.Water{Depth: 8, Still: true}, block.Lava{Depth: 8, Still: true},
		block.WoodFence{Wood: block.OakWood()}, block.Cactus{}, block.Slab{Block: block.Stone{}}, block.Grass{}}
	<-w.Exec(func(tx *world.Tx) {
		var positions []cube.Pos
		for x := -20; x < 20; x++ {
			for z := -20; z < 20; z++ {
				for y := 62; y < 66; y++ {
					pos := cube.Pos{x, y, z}
					tx.SetBlock(pos, palette[(x*7+z*13+y*3)&7], nil)
					positions = append(positions, pos)
				}
			}
		}
		check := func(round int) {
			for _, pos := range positions {
				got := pathTypeFromState(tx, pos)
				want, _ := computePathTypeFromState(tx, pos)
				if got != want {
					t.Fatalf("round %d: %v at %v reads %v from the cache, want %v", round, tx.Block(pos), pos, got, want)
				}
			}
		}
		check(0)
		for round := 1; round <= 5; round++ {
			for i, pos := range positions {
				if (i+round)%3 == 0 {
					tx.SetBlock(pos, palette[(i+round*5)&7], nil)
				}
			}
			check(round)
		}
	})
}

// TestPrefetchedPathsMatchFindingThem ticks zombies hunting villagers and
// iron golems around obstacles with verifyPrefetch on, so every prefetched
// path is checked against finding the path in the tick.
func TestPrefetchedPathsMatchFindingThem(t *testing.T) {
	verifyPrefetch, verifiedPaths = true, 0
	defer func() { verifyPrefetch = false }()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	w.SetDifficulty(world.DifficultyHard)
	<-w.Exec(func(tx *world.Tx) {
		for x := -40; x < 40; x++ {
			for z := -40; z < 40; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Grass{}, nil)
				if x%6 == 0 && z%4 != 0 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.WoodFence{Wood: block.OakWood()}, nil)
				} else if (x+z)%9 == 0 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
				}
			}
		}
		for x := -36; x < 36; x += 6 {
			for z := -36; z < 36; z += 6 {
				tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(x) + 1.5, 64, float64(z) + .5}}))
				tx.AddEntity(NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(x) + 3.5, 64, float64(z) + 2.5}}))
			}
		}
		for i := 0; i < 12; i++ {
			tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i*6 - 36), 70, 3}}.New(blockFlipperType{}, blockFlipperConfig{}))
		}
	})
	for range 300 {
		w.AdvanceTick()
	}
	t.Logf("verified %d prefetched paths", verifiedPaths)
	if verifiedPaths < 50 {
		t.Fatalf("only %d prefetched paths were used", verifiedPaths)
	}
}

// TestMultiGoalAndBrainWalkPrefetch verifies parallel prefetching for
// rangedBowAttackGoal, rangedAttackGoal (reach=1), and brain memWalkTarget
// (reach=0, including blockPosTracker and *entityTracker without mutating
// et.last), and exercises them in a full parallel tick under verifyPrefetch.
func TestMultiGoalAndBrainWalkPrefetch(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	w.SetDifficulty(world.DifficultyHard)
	w.SetTime(18000)

	var (
		skelH, golemH, vilBlockH, vilEntH *world.EntityHandle
		et                                *entityTracker
		sentinel                          = mgl64.Vec3{777.5, 64, 777.5}
		wantPos                           = cube.Pos{18, 64, 0}
	)
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 28; x++ {
			for z := -12; z <= 12; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 70, z}, block.Stone{}, nil)
			}
		}
		// Place a pillar wall so ranged mobs need to pathfind around it.
		for z := -4; z <= 4; z++ {
			tx.SetBlock(cube.Pos{10, 64, z}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{10, 65, z}, block.Stone{}, nil)
		}
		targetZ := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{20.5, 64, 0.5}}))
		targetH := targetZ.H()
		if tm, ok := mobOf(targetZ); ok {
			tm.AI.NoAI = true
		}

		skel := tx.AddEntity(NewSkeleton(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, -2.5}}))
		skel.(*skeleton).SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
		golem := tx.AddEntity(NewSnowGolem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 2.5}}))
		vilBlock := tx.AddEntity(NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, -6.5}}))
		vilEnt := tx.AddEntity(NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 6.5}}))
		skelH, golemH, vilBlockH, vilEntH = skel.H(), golem.H(), vilBlock.H(), vilEnt.H()

		for _, e := range []world.Entity{skel, golem, vilBlock, vilEnt} {
			if ticker, ok := e.(interface{ Tick(*world.Tx, int64) }); ok {
				ticker.Tick(tx, 1)
			}
		}
		sm, _ := mobOf(skel)
		sm.AI.tickCount = 0
		sm.AI.onGround = true
		sm.SetTarget(targetZ)
		if bow, _ := sm.AI.findRangedBowAttackGoal(); bow == nil {
			t.Fatal("expected skeleton to have rangedBowAttackGoal")
		}

		gm, _ := mobOf(golem)
		gm.AI.tickCount = 0
		gm.AI.onGround = true
		gm.SetTarget(targetZ)
		if rng, _ := gm.AI.findRangedAttackGoal(); rng == nil {
			t.Fatal("expected snow golem to have rangedAttackGoal")
		}

		vbm, _ := mobOf(vilBlock)
		vbm.AI.tickCount = 0
		vbm.AI.onGround = true
		vbm.brain().set(memWalkTarget, walkTarget{target: blockPosTracker{pos: wantPos}, speed: 1, closeEnough: 0})

		vem, _ := mobOf(vilEnt)
		vem.AI.tickCount = 0
		vem.AI.onGround = true
		et = newEntityTracker(targetZ, false)
		et.last = sentinel
		vem.brain().set(memWalkTarget, walkTarget{target: et, speed: 1, closeEnough: 0})
		_ = targetH

		prefetched := []world.Entity{skel, golem, vilBlock, vilEnt}
		parallel.ForWorkers(len(prefetched), 4, func(_, i int) {
			e := prefetched[i]
			_, ent := e.(interface{ mobAI() (*MobAI, *Ent) }).mobAI()
			ent.Prefetch(e, tx.NewReadView())
		})

		checkMobPrefetch := func(name string, h *world.EntityHandle, wantTarget cube.Pos, wantReach int) {
			e, ok := h.Entity(tx)
			if !ok {
				t.Fatalf("%s missing", name)
			}
			m, ok := mobOf(e)
			if !ok {
				t.Fatalf("%s has no Mob", name)
			}
			p := &m.AI.pre.path
			prePath := p.path
			if !p.ok || prePath == nil {
				t.Fatalf("%s expected prefetched path, got ok=%v path=%v", name, p.ok, prePath)
			}
			if p.reach != wantReach || p.target != wantTarget {
				t.Fatalf("%s got target=%v reach=%d, want target=%v reach=%d", name, p.target, p.reach, wantTarget, wantReach)
			}
			m.AI.nav.path = nil
			live := m.AI.nav.CreatePath(m, wantTarget, wantReach)
			if p.path != nil {
				t.Fatalf("%s expected takePath to consume prefetched path, still got %v", name, p.path)
			}
			if !pathsIdentical(live, prePath) {
				t.Fatalf("%s prefetched path mismatch: prefetched=%v live=%v", name, prePath, live)
			}
		}
		checkMobPrefetch("skeleton/rangedBowAttackGoal", skelH, cube.PosFromVec3(targetZ.Position()), 1)
		checkMobPrefetch("snowGolem/rangedAttackGoal", golemH, cube.PosFromVec3(targetZ.Position()), 1)
		checkMobPrefetch("villager/blockPosTracker", vilBlockH, wantPos, 0)
		checkMobPrefetch("villager/entityTracker", vilEntH, cube.PosFromVec3(targetZ.Position()), 0)
		if et.last != sentinel {
			t.Fatalf("entityTracker.last mutated during Prefetch: got %v, want %v", et.last, sentinel)
		}
	})

	// Part B: live world tick with > prefetchMinimum mobs and verifyPrefetch=true.
	verifyPrefetch, verifiedPaths = true, 0
	defer func() { verifyPrefetch = false }()
	w2 := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w2.Close()
	w2.SetDifficulty(world.DifficultyHard)
	w2.SetTime(18000)

	<-w2.Exec(func(tx *world.Tx) {
		for x := -36; x <= 36; x++ {
			for z := -36; z <= 36; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 70, z}, block.Stone{}, nil)
				if x%8 == 0 && z%4 != 0 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.WoodFence{Wood: block.OakWood()}, nil)
				}
			}
		}
		farZombie := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{30.5, 64, 30.5}}))
		if zm, ok := mobOf(farZombie); ok {
			zm.AI.NoAI = true
		}
		for i := range 24 {
			off := float64((i%6)*3 - 20)
			zOff := float64((i/6)*3 - 20)
			sk := tx.AddEntity(NewSkeleton(world.EntitySpawnOpts{Position: mgl64.Vec3{off + .5, 64, zOff + .5}}))
			sk.(*skeleton).SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
			if m, ok := mobOf(sk); ok {
				m.SetTarget(farZombie)
			}
			sg := tx.AddEntity(NewSnowGolem(world.EntitySpawnOpts{Position: mgl64.Vec3{off + 1.5, 64, zOff + .5}}))
			if m, ok := mobOf(sg); ok {
				m.SetTarget(farZombie)
			}
			v := tx.AddEntity(NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{off + .5, 64, zOff + 1.5}}))
			if m, ok := mobOf(v); ok {
				target := cube.Pos{int(off) + 18, 64, int(zOff) + 18}
				if i%2 == 0 {
					m.brain().set(memWalkTarget, walkTarget{target: blockPosTracker{pos: target}, speed: 1, closeEnough: 0})
				} else {
					m.brain().set(memWalkTarget, walkTarget{target: newEntityTracker(farZombie, false), speed: 1, closeEnough: 0})
				}
			}
		}
	})
	for range 60 {
		w2.AdvanceTick()
	}
	t.Logf("verified %d multi-goal & brain prefetched paths", verifiedPaths)
	if verifiedPaths < 30 {
		t.Fatalf("only %d multi-goal & brain prefetched paths were verified", verifiedPaths)
	}
}
