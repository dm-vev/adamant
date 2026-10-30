package world_test

import (
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestQueuedEntityBirthTickRunsOnceAndIgnoresRemoval(t *testing.T) {
	var blocks atomic.Int32
	states := []*postBlockProbeState{{blocks: &blocks}, {blocks: &blocks}, {blocks: &blocks}, {blocks: &blocks}}
	entities := make([]*postBlockProbeEntity, len(states))
	spawn := func(tx *world.Tx, i int) {
		e := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 64, 8.5}}.New(postBlockProbeType{}, postBlockProbeConfig{states[i]}))
		entities[i] = e.(*postBlockProbeEntity)
	}
	b := entitySpawnTickBlock{frozenTickBlock: frozenTickBlock{ticks: &blocks}, spawn: func(tx *world.Tx) {
		blocks.Add(1)
		for i := 0; i < 3; i++ {
			spawn(tx, i)
			tx.QueueEntityTick(entities[i])
		}
		tx.QueueEntityTick(entities[0])
		tx.RemoveEntity(entities[2])
		// This actor has already run in the ordinary pre-block phase.
		tx.QueueEntityTick(entities[3])
	}}
	registry := world.NewBlockRegistry()
	name, properties := b.EncodeBlock()
	registry.RegisterBlockState(world.BlockState{Name: name, Properties: properties})
	registry.RegisterBlock(b)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Blocks: registry, Entities: (world.EntityRegistryConfig{}).New([]world.EntityType{postBlockProbeType{}})}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		p := cube.Pos{8, 63, 8}
		tx.SetBlock(p, b, nil)
		tx.ScheduleBlockUpdate(p, b, time.Second/20)
		spawn(tx, 3)
	})
	for tick := 1; tick <= 2; tick++ {
		w.AdvanceTick()
		for i, state := range states {
			want := tick
			if i == 2 {
				want = 0
			}
			if state.normal != want || state.post != want || entities[i].data.Age != time.Duration(want)*time.Second/20 {
				t.Fatalf("tick %d entity %d normal=%d post=%d age=%s; want %d ticks", tick, i, state.normal, state.post, entities[i].data.Age, want)
			}
			if want > 0 && !state.sawBlock {
				t.Fatalf("tick %d entity %d sampled blocks too early", tick, i)
			}
		}
	}
}

type entitySpawnTickBlock struct {
	frozenTickBlock
	spawn func(*world.Tx)
}

func (b entitySpawnTickBlock) ScheduledTick(_ cube.Pos, tx *world.Tx, _ *rand.Rand) {
	b.spawn(tx)
}

func TestEntityPostBlockPhaseHandlesRemovalAndFrozenTicks(t *testing.T) {
	var blocks atomic.Int32
	b := frozenTickBlock{ticks: &blocks}
	registry := world.NewBlockRegistry()
	name, properties := b.EncodeBlock()
	registry.RegisterBlockState(world.BlockState{Name: name, Properties: properties})
	registry.RegisterBlock(b)
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Blocks: registry, Entities: (world.EntityRegistryConfig{}).New([]world.EntityType{postBlockProbeType{}})}.New()
	defer w.Close()
	states := []*postBlockProbeState{{blocks: &blocks, remove: true}, {blocks: &blocks}}
	<-w.Exec(func(tx *world.Tx) {
		p := cube.Pos{8, 63, 8}
		tx.SetBlock(p, b, nil)
		tx.ScheduleBlockUpdate(p, b, time.Second/20)
		for _, state := range states {
			tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 64, 8.5}}.New(postBlockProbeType{}, postBlockProbeConfig{state}))
		}
	})
	w.AdvanceTick()
	for i, state := range states {
		if state.normal != 1 || state.post != 1 || !state.sawBlock {
			t.Fatalf("entity %d normal=%d post=%d saw block=%t", i, state.normal, state.post, state.sawBlock)
		}
	}
	<-w.Exec(func(tx *world.Tx) { tx.World().Freeze() })
	w.AdvanceTick()
	if states[1].normal != 1 || states[1].post != 1 {
		t.Fatal("post-block callback ran while frozen")
	}
	<-w.Exec(func(tx *world.Tx) {
		if err := tx.World().StepTicks(1); err != nil {
			t.Fatal(err)
		}
	})
	w.AdvanceTick()
	if states[0].normal != 1 || states[0].post != 1 || states[1].normal != 2 || states[1].post != 2 {
		t.Fatal("post-block callbacks skipped, duplicated or revisited a removed entity")
	}
}

type postBlockProbeState struct {
	blocks           *atomic.Int32
	normal, post     int
	remove, sawBlock bool
}
type postBlockProbeConfig struct{ state *postBlockProbeState }

func (c postBlockProbeConfig) Apply(d *world.EntityData) { d.Data = c.state }

type postBlockProbeType struct{ frozenMoveType }

func (postBlockProbeType) EncodeEntity() string { return "test:post_block_probe" }
func (postBlockProbeType) Open(_ *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &postBlockProbeEntity{frozenMoveEntity: frozenMoveEntity{handle: h, data: d}, state: d.Data.(*postBlockProbeState)}
}

type postBlockProbeEntity struct {
	frozenMoveEntity
	state *postBlockProbeState
}

func (e *postBlockProbeEntity) Tick(*world.Tx, int64) {
	e.state.normal++
	e.data.Age += time.Second / 20
}
func (e *postBlockProbeEntity) TickAfterBlockUpdates(tx *world.Tx, _ int64) {
	e.state.post++
	e.state.sawBlock = e.state.blocks.Load() == 1
	if e.state.remove {
		tx.RemoveEntity(e)
	}
}
