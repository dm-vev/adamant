package world_test

import (
	"slices"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/ecs"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type stageAChunkViewer struct {
	world.NopViewer
	chunks      []world.ChunkPos
	blockCounts []int
}

func (v *stageAChunkViewer) ViewChunk(pos world.ChunkPos, _ world.Dimension, _ *world.Column) {
	v.chunks = append(v.chunks, pos)
}

func (v *stageAChunkViewer) ViewBlockUpdates(u *world.BlockUpdates) {
	v.blockCounts = append(v.blockCounts, len(u.Updates))
}

type stageABroadcastViewer struct {
	world.NopViewer
	sharedCalls int
	plainCalls  int
	lastShared  any
}

func (v *stageABroadcastViewer) ViewEntityState(world.Entity) { v.plainCalls++ }
func (v *stageABroadcastViewer) ViewEntityStateShared(_ world.Entity, b *world.Broadcast) {
	v.sharedCalls++
	if b.Load() == nil {
		b.Store("state-token")
	}
	v.lastShared = b.Load()
}
func (v *stageABroadcastViewer) ViewEntityActionShared(_ world.Entity, _ world.EntityAction, b *world.Broadcast) {
	v.sharedCalls++
	if b.Load() == nil {
		b.Store("action-token")
	}
	v.lastShared = b.Load()
}
func (v *stageABroadcastViewer) ViewEntityVelocityShared(_ world.Entity, _ mgl64.Vec3, b *world.Broadcast) {
	v.sharedCalls++
	if b.Load() == nil {
		b.Store("vel-token")
	}
	v.lastShared = b.Load()
}
func (v *stageABroadcastViewer) ViewEntityItemsShared(_ world.Entity, b *world.Broadcast) {
	v.sharedCalls++
	if b.Load() == nil {
		b.Store("items-token")
	}
	v.lastShared = b.Load()
}
func (v *stageABroadcastViewer) ViewEntityArmourShared(_ world.Entity, b *world.Broadcast) {
	v.sharedCalls++
	if b.Load() == nil {
		b.Store("armour-token")
	}
	v.lastShared = b.Load()
}

type stageAPlainViewer struct {
	world.NopViewer
	actions int
	vels    int
	items   int
	armour  int
}

func (v *stageAPlainViewer) ViewEntityAction(world.Entity, world.EntityAction) { v.actions++ }
func (v *stageAPlainViewer) ViewEntityVelocity(world.Entity, mgl64.Vec3)       { v.vels++ }
func (v *stageAPlainViewer) ViewEntityItems(world.Entity)                      { v.items++ }
func (v *stageAPlainViewer) ViewEntityArmour(world.Entity)                     { v.armour++ }

func TestStageAECSBagsAndEvents(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer func() { _ = w.Close() }()

	kWorld := ecs.NewKey[string]("test:stagea_world")
	kChunk := ecs.NewKey[int]("test:stagea_chunk")
	kEntity := ecs.NewKey[int]("test:stagea_entity")

	ecs.Set(w.Components(), kWorld, "overworld-tag")
	if got, ok := ecs.Get(w.Components(), kWorld); !ok || got != "overworld-tag" {
		t.Fatalf("World.Components() = (%q, %v), want overworld-tag", got, ok)
	}

	var tickCount, blockEvents, spawns, despawns, chunkLoads, chunkUnloads int
	subTick := event.On(w.Events(), event.PriorityNormal, func(e *world.WorldTickEvent) {
		if e.Tick > 0 {
			tickCount++
		}
	})
	defer subTick.Unsubscribe()

	// Cancel setting bedrock via BlockChangeEvent.
	bedrockRID := world.DefaultBlockRegistry.BlockRuntimeID(block.Bedrock{})
	subBlock := event.On(w.Events(), event.PriorityNormal, func(e *world.BlockChangeEvent) {
		blockEvents++
		if e.NewRID == bedrockRID {
			e.Cancel()
		}
	})
	defer subBlock.Unsubscribe()

	event.On(w.Events(), event.PriorityNormal, func(*world.EntitySpawnEvent) { spawns++ })
	event.On(w.Events(), event.PriorityNormal, func(*world.EntityDespawnEvent) { despawns++ })
	event.On(w.Events(), event.PriorityNormal, func(*world.ChunkLoadEvent) { chunkLoads++ })
	event.On(w.Events(), event.PriorityNormal, func(*world.ChunkUnloadEvent) { chunkUnloads++ })

	<-w.Exec(func(tx *world.Tx) {
		bag := tx.ChunkComponents(world.ChunkPos{1, 1})
		ecs.Set(bag, kChunk, 77)
		if got, ok := ecs.Get(tx.ChunkComponents(world.ChunkPos{1, 1}), kChunk); !ok || got != 77 {
			t.Fatalf("ChunkComponents = (%d, %v), want 77", got, ok)
		}

		// Cancelled BlockChangeEvent prevents SetBlock and SetBlockRID.
		pos := cube.Pos{17, 64, 17}
		tx.SetBlock(pos, block.Bedrock{}, nil)
		if _, isBedrock := tx.Block(pos).(block.Bedrock); isBedrock {
			t.Fatal("cancelled SetBlock still placed bedrock")
		}
		tx.SetBlockRID(pos, bedrockRID, nil)
		if _, isBedrock := tx.Block(pos).(block.Bedrock); isBedrock {
			t.Fatal("cancelled SetBlockRID still placed bedrock")
		}

		// Allowed block change proceeds.
		tx.SetBlock(pos, block.Stone{}, nil)
		if _, isStone := tx.Block(pos).(block.Stone); !isStone {
			t.Fatal("allowed SetBlock did not place stone")
		}

		h := entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{17.5, 65, 17.5}})
		ecs.Set(h.Components(), kEntity, 55)
		ent := tx.AddEntity(h)
		if got, ok := ecs.Get(ent.H().Components(), kEntity); !ok || got != 55 {
			t.Fatalf("EntityHandle.Components = (%d, %v), want 55", got, ok)
		}
		tx.RemoveEntity(ent)
	})

	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		// Unload chunk {1, 1} so ChunkUnloadEvent fires.
		w.CollectGarbage(tx)
	})

	if tickCount != 1 {
		t.Fatalf("WorldTickEvent count = %d, want 1", tickCount)
	}
	if blockEvents != 3 {
		t.Fatalf("BlockChangeEvent count = %d, want 3", blockEvents)
	}
	if spawns != 1 || despawns != 1 {
		t.Fatalf("spawns=%d despawns=%d, want 1 and 1", spawns, despawns)
	}
	if chunkLoads < 1 || chunkUnloads < 1 {
		t.Fatalf("chunkLoads=%d chunkUnloads=%d, want >=1 each", chunkLoads, chunkUnloads)
	}
}

func TestStageATickSystemsAndPanicContainment(t *testing.T) {
	t.Parallel()
	var faults []world.Fault
	var mu sync.Mutex
	w := world.Config{
		Synchronous:     true,
		RandomTickSpeed: -1,
		Fault: func(f world.Fault) {
			mu.Lock()
			faults = append(faults, f)
			mu.Unlock()
		},
	}.New()
	defer func() { _ = w.Close() }()

	var order []string
	var removeSelf func()
	removePre := w.AddSystem(world.PhasePreEntities, func(_ *world.Tx, tick int64) {
		order = append(order, "pre")
	})
	removePanic := w.AddSystem(world.PhasePreEntities, func(_ *world.Tx, _ int64) {
		order = append(order, "pre-panic")
		panic(faultcheck.Expected{Why: "test: system panic"})
	})
	removePreAfter := w.AddSystem(world.PhasePreEntities, func(_ *world.Tx, _ int64) {
		order = append(order, "pre-after-panic")
	})
	removeSelf = w.AddSystem(world.PhasePostEntities, func(_ *world.Tx, _ int64) {
		order = append(order, "post-entities")
		removeSelf()
		removeSelf() // idempotent
	})
	w.AddSystem(world.PhasePostTick, func(_ *world.Tx, _ int64) {
		order = append(order, "post-tick")
	})

	w.AdvanceTick()
	wantTick1 := []string{"pre", "pre-panic", "pre-after-panic", "post-entities", "post-tick"}
	if !slices.Equal(order, wantTick1) {
		t.Fatalf("tick 1 system order = %v, want %v", order, wantTick1)
	}

	mu.Lock()
	faultCount := len(faults)
	mu.Unlock()
	if faultCount != 1 || faults[0].Kind != world.FaultSubsystem {
		t.Fatalf("faults = %+v, want 1 subsystem fault", faults)
	}

	order = nil
	removePanic()
	removePre()
	removePreAfter()
	w.AdvanceTick()
	if !slices.Equal(order, []string{"post-tick"}) {
		t.Fatalf("tick 2 system order = %v, want [post-tick]", order)
	}
}

func TestStageASetBlockRIDAndFillRID(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()

	reg := w.BlockRegistry()
	stoneRID := reg.BlockRuntimeID(block.Stone{})
	glowRID := reg.BlockRuntimeID(block.Glowstone{})
	chestRID := reg.BlockRuntimeID(block.Chest{})
	airRID := reg.AirRuntimeID()

	viewer := &stageAChunkViewer{}
	<-w.Exec(func(tx *world.Tx) {
		l := world.NewLoader(2, w, viewer)
		l.Move(tx, mgl64.Vec3{16, 64, 16})
		l.Load(tx, 9)
	})
	w.AdvanceTick()
	viewer.chunks = nil
	viewer.blockCounts = nil

	// Test SetBlockRID including distinct NBT block entity instances + viewBlockUpdate.
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlockRID(cube.Pos{1, 64, 1}, stoneRID, nil)
		tx.SetBlockRID(cube.Pos{2, 64, 1}, chestRID, nil)
		tx.SetBlockRID(cube.Pos{3, 64, 1}, chestRID, nil)
		c1, ok1 := tx.Block(cube.Pos{2, 64, 1}).(block.Chest)
		c2, ok2 := tx.Block(cube.Pos{3, 64, 1}).(block.Chest)
		if !ok1 || !ok2 {
			t.Fatalf("expected Chest blocks, got %v %v", ok1, ok2)
		}
		if c1.Inventory(tx, cube.Pos{2, 64, 1}) == c2.Inventory(tx, cube.Pos{3, 64, 1}) {
			t.Fatal("SetBlockRID shared NBT inventory instance across two chest positions")
		}
		if got, ok := tx.BlockRID(cube.Pos{1, 64, 1}); !ok || got != stoneRID {
			t.Fatalf("BlockRID = (%d, %v), want %d", got, ok, stoneRID)
		}
	})
	if len(viewer.blockCounts) != 1 || viewer.blockCounts[0] != 3 {
		t.Fatalf("SetBlockRID viewer blockCounts = %v, want [3]", viewer.blockCounts)
	}

	// Test FillRID across chunks & full + partial subchunks, heightmaps, lighting, ViewChunk.
	viewer.chunks = nil
	viewer.blockCounts = nil
	<-w.Exec(func(tx *world.Tx) {
		// Pre-dirty a block update inside [0..17, 0..31, 0..17] that FillRID overwrites so stale pending update is dropped.
		tx.SetBlockRID(cube.Pos{5, 20, 5}, glowRID, nil)

		changed := tx.FillRID(cube.Pos{0, 0, 0}, cube.Pos{17, 31, 17}, stoneRID)
		wantChanged := 18 * 32 * 18
		if changed != wantChanged {
			t.Fatalf("FillRID changed = %d, want %d", changed, wantChanged)
		}
		// Filling same RID again should change 0 blocks.
		if again := tx.FillRID(cube.Pos{0, 0, 0}, cube.Pos{17, 31, 17}, stoneRID); again != 0 {
			t.Fatalf("idempotent FillRID changed = %d, want 0", again)
		}
		if hb := tx.HighestBlock(5, 5); hb != 31 {
			t.Fatalf("HighestBlock(5, 5) = %d, want 31", hb)
		}
		// Fill glowstone at y=32 in chunk {0,0} and verify light updates.
		tx.FillRID(cube.Pos{4, 32, 4}, cube.Pos{6, 32, 6}, glowRID)
		if light := tx.BlockLight(cube.Pos{5, 33, 5}); light < 14 {
			t.Fatalf("BlockLight above FillRID glowstone = %d, want >=14", light)
		}
		// Clear with airRID.
		if cleared := tx.FillRID(cube.Pos{4, 32, 4}, cube.Pos{6, 32, 6}, airRID); cleared != 9 {
			t.Fatalf("FillRID clear = %d, want 9", cleared)
		}
		if light := tx.BlockLight(cube.Pos{5, 33, 5}); light != 0 {
			t.Fatalf("BlockLight after clearing glowstone = %d, want 0", light)
		}
	})
	if len(viewer.chunks) == 0 {
		t.Fatal("FillRID did not trigger ViewChunk on viewers")
	}
	if len(viewer.blockCounts) != 0 {
		t.Fatalf("overwritten pending block update leaked after FillRID: %v", viewer.blockCounts)
	}
}

func TestStageAMetaPatchAndBroadcasts(t *testing.T) {
	t.Parallel()
	var patch world.ActorMetaPatch
	if !patch.IsZero() {
		t.Fatal("fresh ActorMetaPatch should be zero")
	}
	patch.SetNameTag("Boss")
	patch.SetAlwaysShowNameTag(true)
	patch.SetScoreTag("100 HP")
	patch.SetScale(1.5)
	patch.SetWidth(1.2)
	patch.SetHeight(2.4)
	patch.SetVariant(3)
	patch.SetMarkVariant(7)
	patch.SetColor(14)
	patch.SetInvisible(false)
	patch.SetOnFire(true)
	patch.SetNoAI(true)
	patch.SetFlag(5)
	patch.ClearFlag(6)
	patch.SetFlag(70)
	patch.ClearFlag(71)
	patch.SetString("role", "guard")
	patch.SetInt("tier", 4)
	patch.SetFloat("rage", 0.75)
	if patch.IsZero() {
		t.Fatal("populated ActorMetaPatch reported IsZero")
	}
	if patch.FlagSet&(1<<5) == 0 || patch.FlagClear&(1<<6) == 0 || patch.Flag2Set&(1<<6) == 0 || patch.Flag2Clear&(1<<7) == 0 {
		t.Fatalf("unexpected flag masks: %+v", patch)
	}

	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer func() { _ = w.Close() }()

	bv1 := &stageABroadcastViewer{}
	bv2 := &stageABroadcastViewer{}
	pv := &stageAPlainViewer{}
	<-w.Exec(func(tx *world.Tx) {
		for _, v := range []world.Viewer{bv1, bv2, pv} {
			l := world.NewLoader(1, w, v)
			l.Move(tx, mgl64.Vec3{8, 64, 8})
			l.Load(tx, 9)
		}
	})
	<-w.Exec(func(tx *world.Tx) {
		h := entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 64, 8.5}})
		ecs.Set(h.Components(), world.MetaPatchKey, patch)
		e := tx.AddEntity(h)
		if got, ok := ecs.Get(e.H().Components(), world.MetaPatchKey); !ok || got.NameTag == nil || *got.NameTag != "Boss" {
			t.Fatalf("MetaPatchKey get mismatch: %+v, %v", got, ok)
		}
		pv.actions, pv.vels, pv.items, pv.armour = 0, 0, 0, 0

		world.BroadcastEntityState(tx, e)
		world.BroadcastEntityAction(tx, e, nil)
		world.BroadcastEntityVelocity(tx, e, mgl64.Vec3{0, 0.2, 0})
		world.BroadcastEntityItems(tx, e)
		world.BroadcastEntityArmour(tx, e)

		// Safe nil/closed checks.
		world.BroadcastEntityAction(nil, e, nil)
		world.BroadcastEntityVelocity(tx, nil, mgl64.Vec3{})
	})
	if bv1.sharedCalls != 5 || bv2.sharedCalls != 5 {
		t.Fatalf("sharedCalls = (%d, %d), want (5, 5)", bv1.sharedCalls, bv2.sharedCalls)
	}
	if bv1.lastShared == nil || bv1.lastShared != bv2.lastShared {
		t.Fatalf("expected broadcast token shared across viewers: %v vs %v", bv1.lastShared, bv2.lastShared)
	}
	if pv.actions != 1 || pv.vels != 1 || pv.items != 1 || pv.armour != 1 {
		t.Fatalf("plain viewer fallback counts = (%d, %d, %d, %d), want all 1", pv.actions, pv.vels, pv.items, pv.armour)
	}
}
