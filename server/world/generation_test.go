package world_test

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

type payloadGenerator struct {
	handle *world.EntityHandle
}

func (*payloadGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 70, 0} }

func (g *payloadGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	if pos != (world.ChunkPos{2, 0}) {
		return world.GenerationResult{}
	}
	c.SetBlock(4, 64, 4, 0, world.BlockRuntimeID(block.NewEndGateway()))
	water := world.BlockRuntimeID(block.Water{Depth: 8, Still: true})
	c.SetBlock(8, 64, 8, 0, water)
	target := mgl64.Vec3{20, 90, -30}
	g.handle = (world.EntitySpawnOpts{Position: mgl64.Vec3{34.5, 70, 2.5}}).New(entity.EndCrystalType, entity.EndCrystalBehaviourConfig{ShowBase: true, BeamTarget: &target})
	return world.GenerationResult{
		Entities:       []*world.EntityHandle{g.handle},
		BlockEntities:  []chunk.BlockEntity{{Pos: cube.Pos{36, 64, 4}, Data: generatedGatewayNBT()}},
		ScheduledTicks: []chunk.ScheduledTick{{Pos: cube.Pos{40, 64, 8}, Kind: chunk.FluidTick, Type: "minecraft:water"}},
	}
}

func generatedGatewayNBT() map[string]any {
	return map[string]any{
		"Age": int64(73), "TeleportCooldown": int32(11), "ExactTeleport": uint8(1),
		"ExitPortal": map[string]any{"X": int32(-500), "Y": int32(72), "Z": int32(600)},
	}
}
func TestGeneratedTickCollectorDeduplicatesByKindTypeAndPosition(t *testing.T) {
	pos := cube.Pos{8, 64, 8}
	result := world.GenerationResult{}
	first := chunk.ScheduledTick{Pos: pos, Kind: chunk.BlockTick, Type: "minecraft:water", Priority: 0}
	result.ScheduleTick(first)
	result.ScheduleTick(chunk.ScheduledTick{Pos: pos, Kind: chunk.BlockTick, Type: "minecraft:water", Priority: -2})
	result.ScheduleTick(chunk.ScheduledTick{Pos: pos, Kind: chunk.BlockTick, Type: "minecraft:lava"})
	result.ScheduleTick(chunk.ScheduledTick{Pos: pos, Kind: chunk.FluidTick, Type: "minecraft:water"})

	if !result.HasScheduledTick(pos, chunk.BlockTick, "minecraft:water") {
		t.Fatal("generated collector lost the first source block tick")
	}
	if result.HasScheduledTick(pos, chunk.FluidTick, "minecraft:lava") {
		t.Fatal("generated collector reported an unscheduled fluid type")
	}
	want := []chunk.ScheduledTick{
		first,
		{Pos: pos, Kind: chunk.BlockTick, Type: "minecraft:lava"},
		{Pos: pos, Kind: chunk.FluidTick, Type: "minecraft:water"},
	}
	if !reflect.DeepEqual(result.ScheduledTicks, want) {
		t.Fatalf("generated scheduled ticks = %#v, want stable first-occurrence order %#v", result.ScheduledTicks, want)
	}
}

func checkGeneratedPayload(t *testing.T, w *world.World) {
	t.Helper()
	task := w.Do(func(tx *world.Tx) {
		gateway, ok := tx.Block(cube.Pos{36, 64, 4}).(block.EndGateway)
		if !ok {
			t.Error("generated gateway missing")
			return
		}
		want := block.NewEndGateway().DecodeNBT(generatedGatewayNBT()).(block.EndGateway).EncodeNBT()
		if got := gateway.EncodeNBT(); !reflect.DeepEqual(got, want) {
			t.Errorf("generated gateway NBT = %v, want %v", got, want)
		}
		count := 0
		for e := range tx.Entities() {
			count++
			crystal, ok := e.(*entity.EndCrystal)
			if !ok {
				t.Errorf("unexpected generated entity %T", e)
				continue
			}
			beam, has := crystal.BeamTarget()
			if !crystal.ShowBase() || !has || beam != (mgl64.Vec3{20, 90, -30}) {
				t.Errorf("crystal properties lost: base=%t beam=%v present=%t", crystal.ShowBase(), beam, has)
			}
		}
		if count != 1 {
			t.Errorf("generated entities = %d, want exactly one", count)
		}
	})
	<-task.Done()
	if err := task.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestGeneratedPayloadPersistsWithoutDuplicateActivation(t *testing.T) {
	for _, synchronous := range []bool{true, false} {
		name := "worker"
		if synchronous {
			name = "synchronous"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			db, err := (mcdb.Config{}).Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			g := &payloadGenerator{}
			w := world.Config{Provider: db, Generator: g, Entities: entity.DefaultRegistry, Synchronous: synchronous, GeneratorWorkers: 1, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			checkGeneratedPayload(t, w)
			checkGeneratedPayload(t, w)
			w.Save()
			checkGeneratedPayload(t, w)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			if !g.handle.Closed() {
				t.Error("generated entity was not closed")
			}

			db, err = (mcdb.Config{}).Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			// No generator: everything observed below must come from disk.
			w = world.Config{Provider: db, Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
			checkGeneratedPayload(t, w)
			w.AdvanceTick()
			<-w.Exec(func(tx *world.Tx) {
				if _, flowing := tx.Liquid(cube.Pos{40, 63, 8}); flowing {
					t.Error("saved generated fluid tick ran before its 3x3 neighbourhood was ready")
				}
				for dx := range 3 {
					for dz := range 3 {
						tx.Block(cube.Pos{(dx + 1) * 16, 64, (dz - 1) * 16})
					}
				}
			})
			for i := range 3 {
				w.AdvanceTick()
				task := w.Do(func(tx *world.Tx) {
					_, flowing := tx.Block(cube.Pos{40, 63, 8}).(block.Water)
					if !flowing {
						t.Errorf("water below source after tick %d = %t, want true", i+1, flowing)
					}
				})
				<-task.Done()
				if err := task.Err(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestReadOnlyWorldClosesGeneratedEntities(t *testing.T) {
	g := &payloadGenerator{}
	w := world.Config{Generator: g, Entities: entity.DefaultRegistry, Synchronous: true, ReadOnly: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	checkGeneratedPayload(t, w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !g.handle.Closed() {
		t.Fatal("read-only world leaked generated entity")
	}
}

type protoFluidTickGenerator struct{}

func (protoFluidTickGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{8, 64, 8} }

func (protoFluidTickGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	if pos != (world.ChunkPos{}) {
		return world.GenerationResult{}
	}
	c.SetBlock(9, 64, 8, 0, world.BlockRuntimeID(block.Stone{}))
	return world.GenerationResult{ScheduledTicks: sourceProtoFluidTicks()}
}

// Executed Java 26.3 ProtoChunkTicks retains all five fluid registry types at
// these heights, in insertion order, with zero delay and NORMAL priority.
func sourceProtoFluidTicks() []chunk.ScheduledTick {
	ticks := make([]chunk.ScheduledTick, 0, 20)
	for _, fluid := range [...]string{"minecraft:empty", "minecraft:water", "minecraft:flowing_water", "minecraft:lava", "minecraft:flowing_lava"} {
		for _, y := range [...]int{-65, -64, 319, 320} {
			ticks = append(ticks, chunk.ScheduledTick{Pos: cube.Pos{8, y, 8}, Kind: chunk.FluidTick, Type: fluid})
		}
	}
	return ticks
}

func TestGeneratedProtoFluidTicksRetainEmptyAndOutsideHeightAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Generator: protoFluidTickGenerator{}, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() {
		if w != nil {
			_ = w.Close()
		}
	})
	load := func(neighbours bool) {
		task := w.Do(func(tx *world.Tx) {
			if neighbours {
				for dx := range 3 {
					for dz := range 3 {
						tx.Block(cube.Pos{(dx - 1) * 16, 64, (dz - 1) * 16})
					}
				}
			}
			if _, ok := tx.Block(cube.Pos{9, 64, 8}).(block.Stone); !ok {
				t.Errorf("inert or stale fluid tick changed the stone marker")
			}
		})
		<-task.Done()
		if err := task.Err(); err != nil {
			t.Fatal(err)
		}
	}
	save := func() {
		w.Save()
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		w = nil
		db, err = mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
	}
	checkSaved := func(want []chunk.ScheduledTick) {
		col, err := db.LoadColumn(world.ChunkPos{}, world.Overworld)
		if err != nil {
			t.Fatal(err)
		}
		if len(col.ScheduledTicks) != len(want) {
			t.Fatalf("saved Source proto ticks=%v, want %v", col.ScheduledTicks, want)
		}
		for i, expected := range want {
			got := col.ScheduledTicks[i]
			if got.ScheduledTick != expected || got.Delay != 0 {
				t.Errorf("saved tick %d=%+v, Source=%+v with zero delay", i, got, expected)
			}
			if expected.Type == "minecraft:empty" && got.NativeBlock != world.DefaultBlockRegistry.AirRuntimeID() {
				t.Errorf("empty fluid legacy block=%d, want native air", got.NativeBlock)
			}
		}
	}
	load(false)
	save()
	checkSaved(sourceProtoFluidTicks())
	w = world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	load(false)
	save()
	checkSaved(sourceProtoFluidTicks())
	w = world.Config{Provider: db, Synchronous: true, RandomTickSpeed: -1}.New()
	load(true)
	w.AdvanceTick()
	load(false)
	save()
	checkSaved(nil)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}
