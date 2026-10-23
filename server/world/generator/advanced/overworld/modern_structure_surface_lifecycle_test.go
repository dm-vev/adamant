package overworld

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

func surfaceLifecycleState(t *testing.T, g *scatteredLifecycleGenerator, start modernStructureStart) []structure.StructurePieceState {
	t.Helper()
	state := g.owner.modernStructures.pieceStates.lookup(start.key)
	if state == nil {
		t.Fatal("native generation did not retain surface piece state")
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := state.prepare(start); err != nil {
		t.Fatal(err)
	}
	copy := append([]structure.StructurePieceState(nil), state.pieces...)
	for i := range copy {
		if runtime := copy[i].Pyramid; runtime != nil {
			clone := *runtime
			clone.SandPositions = append([][3]int32(nil), runtime.SandPositions...)
			copy[i].Pyramid = &clone
		}
	}
	return copy
}

func TestMutableSurfaceBoundsAndActorsSurviveNativeMCDBRestart(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, kind := range []string{"desert_pyramid", "igloo"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			g, start, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				t.Fatal(err)
			}
			db, err := mcdb.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			w := world.Config{Provider: db, Generator: g, Entities: entity.DefaultRegistry, Synchronous: true}.New()
			t.Cleanup(func() {
				if w != nil {
					_ = w.Close()
				}
			})
			task := w.Do(func(tx *world.Tx) { tx.Block(cube.Pos{0, 70, 0}) })
			<-task.Done()
			if err = task.Err(); err != nil {
				t.Fatal(err)
			}
			w.Save()
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			w = nil
			before := surfaceLifecycleState(t, g, start)
			for i, flags := range before {
				if !flags.BoundingBoxYSet || mutableStructurePieceBounds(start.pieces[i], &flags).Min[1] == start.pieces[i].Bounds().Min[1] {
					t.Fatalf("piece %d lost the source postProcess bbox move: %+v", i, flags)
				}
			}
			if kind == "desert_pyramid" && (!before[0].GroundHeightSet || before[0].GroundHeight != 63 || before[0].LootPlaced != 15 || before[0].Pyramid == nil || len(before[0].Pyramid.SandPositions) != 83) {
				t.Fatalf("native pyramid did not retain source HPos/chests/runtime: %+v", before[0])
			}
			db, err = mcdb.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			column, err := db.LoadColumn(world.ChunkPos{0, 0}, world.Overworld)
			if err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			tables := map[string]int{}
			for _, actor := range column.BlockEntities {
				if table, ok := actor.Data["LootTable"].(string); ok {
					tables[loot.JavaTableKey(table)]++
				}
			}
			if kind == "desert_pyramid" && tables["minecraft:chests/desert_pyramid"] != 4 {
				_ = db.Close()
				t.Fatalf("saved pyramid chest tables=%v", tables)
			}
			if kind == "igloo" {
				if tables["minecraft:chests/igloo_chest"] != 1 {
					_ = db.Close()
					t.Fatalf("saved igloo marker did not mutate the real chest actor: %v", tables)
				}
			}
			restored, restoredStart, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			w = world.Config{Provider: db, Generator: restored, Entities: entity.DefaultRegistry, Synchronous: true}.New()
			after := surfaceLifecycleState(t, restored, restoredStart)
			for i := range before {
				before[i].Pyramid = nil // Java does not serialize candidates or roofPosition.
				if after[i].Pyramid != nil || !reflect.DeepEqual(after[i], before[i]) {
					t.Fatalf("restart changed piece %d persisted state: before=%+v after=%+v", i, before[i], after[i])
				}
			}
			task = w.Do(func(tx *world.Tx) {
				tx.Block(cube.Pos{0, 70, 0})
				tx.Block(cube.Pos{16, 70, 0})
				if kind == "igloo" {
					ids := map[string]int{}
					for e := range tx.Entities() {
						ids[e.H().Type().EncodeEntity()]++
						if v, ok := e.(*entity.Villager); ok && (v.Profession() != "minecraft:cleric" || v.Variant() != "minecraft:plains") {
							t.Errorf("reloaded prisoner profession/type=%s/%s", v.Profession(), v.Variant())
						}
					}
					if !reflect.DeepEqual(ids, map[string]int{"minecraft:villager": 1, "minecraft:zombie_villager": 1}) {
						t.Errorf("reloaded igloo prisoners=%v", ids)
					}
				}
			})
			<-task.Done()
			if err = task.Err(); err != nil {
				t.Fatal(err)
			}
			if kind == "desert_pyramid" {
				continued := surfaceLifecycleState(t, restored, restoredStart)[0]
				if continued.GroundHeight != before[0].GroundHeight || continued.BoundingBoxMinY != before[0].BoundingBoxMinY || continued.LootPlaced != 15 {
					t.Fatalf("neighbor chunk changed cached pyramid height/chests after restart: %+v", continued)
				}
			}
		})
	}
}

func TestMutableSurfaceNeighborPreviewDoesNotConsumeBBoxOrRuntime(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, kind := range []string{"desert_pyramid", "igloo"} {
		t.Run(kind, func(t *testing.T) {
			g, start, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				t.Fatal(err)
			}
			preview := g.GenerateChunk(world.ChunkPos{-1, 0}, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), world.GenerationContext{})
			for _, handle := range preview.Entities {
				_ = handle.Close()
			}
			if g.owner.modernStructures.pieceStates.lookup(start.key) != nil {
				t.Fatal("neighbor preview created real surface state")
			}
			real := g.GenerateChunk(world.ChunkPos{0, 0}, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), world.GenerationContext{})
			for _, handle := range real.Entities {
				defer handle.Close()
			}
			before := surfaceLifecycleState(t, g, start)
			preview = g.GenerateChunk(world.ChunkPos{-1, 0}, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), world.GenerationContext{})
			for _, handle := range preview.Entities {
				_ = handle.Close()
			}
			after := surfaceLifecycleState(t, g, start)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("neighbor preview mutated source bbox/flags/runtime: before=%+v after=%+v", before, after)
			}
		})
	}
}
