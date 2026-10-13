package world_test

import (
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestFrozenWorldStopsBlockTicksUntilStepped(t *testing.T) {
	var ticks atomic.Int32
	b := frozenTickBlock{ticks: &ticks}
	registry := world.NewBlockRegistry()
	name, properties := b.EncodeBlock()
	registry.RegisterBlockState(world.BlockState{Name: name, Properties: properties})
	registry.RegisterBlock(b)
	w := world.Config{Synchronous: true, Blocks: registry, RandomTickSpeed: -1}.New()
	defer w.Close()
	pos := cube.Pos{8, 64, 8}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, b, nil)
		tx.ScheduleBlockUpdate(pos, b, time.Second/20)
		tx.World().Freeze()
	})
	w.AdvanceTick()
	if got := ticks.Load(); got != 0 {
		t.Fatalf("scheduled block ticks while frozen = %d, want 0", got)
	}
	<-w.Exec(func(tx *world.Tx) {
		if err := tx.World().StepTicks(1); err != nil {
			t.Fatal(err)
		}
	})
	w.AdvanceTick()
	if got := ticks.Load(); got != 1 {
		t.Fatalf("scheduled block ticks after one step = %d, want 1", got)
	}
	w.AdvanceTick()
	if got := ticks.Load(); got != 1 {
		t.Fatalf("scheduled block ticks after step budget expired = %d, want 1", got)
	}
}

type frozenTickBlock struct {
	block.Stone
	ticks *atomic.Int32
}

func (frozenTickBlock) EncodeBlock() (string, map[string]any)                 { return "test:frozen_tick", nil }
func (frozenTickBlock) Hash() (uint64, uint64)                                { return 1 << 59, 0 }
func (b frozenTickBlock) ScheduledTick(_ cube.Pos, _ *world.Tx, _ *rand.Rand) { b.ticks.Add(1) }

func TestFrozenWorldStopsEntityMovementUntilStepped(t *testing.T) {
	registry := (world.EntityRegistryConfig{}).New([]world.EntityType{frozenMoveType{}})
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: registry}.New()
	defer w.Close()
	handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(frozenMoveType{}, frozenMoveConfig{})
	var initial mgl64.Vec3
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(handle)
		tx.World().Freeze()
		entity, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("entity was not added")
		}
		initial = entity.Position()
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		entity, _ := handle.Entity(tx)
		if got := entity.Position(); got != initial {
			t.Fatalf("frozen entity position = %v, want %v", got, initial)
		}
	})
	<-w.Exec(func(tx *world.Tx) {
		if err := tx.World().StepTicks(1); err != nil {
			t.Fatal(err)
		}
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		entity, _ := handle.Entity(tx)
		if got := entity.Position(); got[0] != initial[0]+1 {
			t.Fatalf("entity position after one step = %v, want X=%v", got, initial[0]+1)
		}
	})
}

type frozenMoveType struct{}

func (frozenMoveType) EncodeEntity() string                        { return "test:frozen_mover" }
func (frozenMoveType) BBox(world.Entity) cube.BBox                 { return cube.Box(0, 0, 0, 1, 1, 1) }
func (frozenMoveType) DecodeNBT(map[string]any, *world.EntityData) {}
func (frozenMoveType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
func (frozenMoveType) Open(_ *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &frozenMoveEntity{handle: handle, data: data}
}

type frozenMoveConfig struct{}

func (frozenMoveConfig) Apply(*world.EntityData) {}

type frozenMoveEntity struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (e *frozenMoveEntity) Close() error              { return nil }
func (e *frozenMoveEntity) H() *world.EntityHandle    { return e.handle }
func (e *frozenMoveEntity) Position() mgl64.Vec3      { return e.data.Pos }
func (e *frozenMoveEntity) Rotation() cube.Rotation   { return e.data.Rot }
func (e *frozenMoveEntity) Tick(_ *world.Tx, _ int64) { e.data.Pos[0]++ }

func TestFrozenWorldStepsOnlyRequestedTicks(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	initial := w.CurrentTick()
	<-w.Exec(func(tx *world.Tx) { tx.World().Freeze() })
	w.AdvanceTick()
	if got := w.CurrentTick(); got != initial {
		t.Fatalf("tick while frozen = %d, want %d", got, initial)
	}
	<-w.Exec(func(tx *world.Tx) {
		if err := tx.World().StepTicks(2); err != nil {
			t.Fatal(err)
		}
	})
	w.AdvanceTick()
	w.AdvanceTick()
	w.AdvanceTick()
	if got := w.CurrentTick(); got != initial+2 {
		t.Fatalf("tick after two-step budget = %d, want %d", got, initial+2)
	}
	if !w.Frozen() {
		t.Fatal("world did not remain frozen after its step budget")
	}
}

func TestForcedChunkStaysLoadedAndPersists(t *testing.T) {
	data := &world.WorldDataMemory{}
	config := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: &world.NopProvider{WorldData: data}}
	w := config.New()
	pos := world.ChunkPos{3, -2}
	<-w.Exec(func(tx *world.Tx) {
		if err := tx.ForceChunk(pos, true); err != nil {
			t.Fatal(err)
		}
		if collected, _, _ := tx.World().CollectGarbage(tx); collected != 0 {
			t.Fatalf("forced chunk garbage-collected: %d", collected)
		}
	})
	if got := w.LoadedChunkCount(); got != 1 {
		t.Fatalf("loaded chunk count = %d, want 1", got)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	config.Provider = &world.NopProvider{WorldData: data}
	reopened := config.New()
	defer reopened.Close()
	if !reopened.IsChunkForced(pos) {
		t.Fatal("forced chunk ticket was not restored from world data")
	}
}
