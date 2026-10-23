package overworld

import (
	"reflect"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

func seaLifecycleChunks(start modernStructureStart) []world.ChunkPos {
	var chunks []world.ChunkPos
	for z := start.box.Min[2] >> 4; z <= start.box.Max[2]>>4; z++ {
		for x := start.box.Min[0] >> 4; x <= start.box.Max[0]>>4; x++ {
			chunks = append(chunks, world.ChunkPos{x, z})
		}
	}
	return chunks
}

func seaLifecycleActors(t *testing.T, db *mcdb.DB, chunks []world.ChunkPos) map[cube.Pos]map[string]any {
	t.Helper()
	actors := make(map[cube.Pos]map[string]any)
	for _, pos := range chunks {
		column, err := db.LoadColumn(pos, world.Overworld)
		if err != nil {
			t.Fatal(err)
		}
		for _, actor := range column.BlockEntities {
			if table, ok := actor.Data["LootTable"].(string); ok {
				actors[actor.Pos] = map[string]any{"LootTable": table, "LootTableSeed": actor.Data["LootTableSeed"]}
			}
		}
	}
	return actors
}

func TestMutableSeaHeightAndActorsSurviveNativeMCDBRestart(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, kind := range []string{"shipwreck", "shipwreck_beached", "ocean_ruin_warm", "ocean_ruin_cold"} {
		t.Run(kind, func(t *testing.T) {
			g, start, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
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
			chunks := seaLifecycleChunks(start)
			entitiesBefore := make(map[cube.Pos]map[string]any)
			load := w.Do(func(tx *world.Tx) {
				for _, pos := range chunks {
					tx.Block(cube.Pos{int(pos[0]) * 16, 70, int(pos[1]) * 16})
				}
				for e := range tx.Entities() {
					if e.H().Type().EncodeEntity() != "minecraft:drowned" {
						t.Errorf("unexpected sea structure entity %s", e.H().Type().EncodeEntity())
						continue
					}
					drowned, ok := e.(*entity.Drowned)
					if !ok || drowned.Health() != 20 {
						t.Errorf("source drowned health/type=%T", e)
						continue
					}
					drowned.Hurt(2, entity.ExplosionDamageSource{})
					main, off := drowned.HeldItems()
					mainNBT, err := item.StackToStructureNBT(main)
					if err != nil {
						t.Fatal(err)
					}
					offNBT, err := item.StackToStructureNBT(off)
					if err != nil {
						t.Fatal(err)
					}
					entitiesBefore[cube.PosFromVec3(e.Position())] = map[string]any{"Health": drowned.Health(), "IsBaby": drowned.Baby(), "MainHand": mainNBT, "OffHand": offNBT}
				}
			})
			<-load.Done()
			if err = load.Err(); err != nil {
				t.Fatal(err)
			}
			before := surfaceLifecycleState(t, g, start)
			for i, flags := range before {
				if !flags.TemplatePositionYSet || !flags.BoundingBoxYSet || flags.TemplatePositionY != flags.BoundingBoxMinY || flags.TemplatePositionY == 90 {
					t.Fatalf("piece %d did not retain Source TP/bbox move: %+v", i, flags)
				}
				if strings.HasPrefix(kind, "shipwreck") && !flags.HeightAdjusted {
					t.Fatalf("shipwreck piece %d did not cache its first height: %+v", i, flags)
				}
			}
			w.Save()
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			w = nil
			db, err = mcdb.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			actorsBefore := seaLifecycleActors(t, db, chunks)
			for pos, actor := range actorsBefore {
				table := loot.JavaTableKey(actor["LootTable"].(string))
				if strings.HasPrefix(kind, "shipwreck") && !strings.HasPrefix(table, "minecraft:chests/shipwreck_") {
					t.Fatalf("shipwreck actor at%v got %s", pos, table)
				}
			}
			restored, restoredStart, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			w = world.Config{Provider: db, Generator: restored, Entities: entity.DefaultRegistry, Synchronous: true}.New()
			after := surfaceLifecycleState(t, restored, restoredStart)
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("Source TP/height_adjusted/bbox changed on restart: before=%+v after=%+v", before, after)
			}
			entitiesAfter := make(map[cube.Pos]map[string]any)
			load = w.Do(func(tx *world.Tx) {
				for _, pos := range chunks {
					tx.Block(cube.Pos{int(pos[0]) * 16, 70, int(pos[1]) * 16})
				}
				for e := range tx.Entities() {
					drowned, ok := e.(*entity.Drowned)
					if !ok {
						t.Errorf("restored sea structure entity=%T", e)
						continue
					}
					main, off := drowned.HeldItems()
					mainNBT, err := item.StackToStructureNBT(main)
					if err != nil {
						t.Fatal(err)
					}
					offNBT, err := item.StackToStructureNBT(off)
					if err != nil {
						t.Fatal(err)
					}
					entitiesAfter[cube.PosFromVec3(e.Position())] = map[string]any{"Health": drowned.Health(), "IsBaby": drowned.Baby(), "MainHand": mainNBT, "OffHand": offNBT}
				}
			})
			<-load.Done()
			if err = load.Err(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(entitiesAfter, entitiesBefore) {
				t.Fatalf("native drowned reran finalizeSpawn or lost state: before=%v after=%v", entitiesBefore, entitiesAfter)
			}
			w.Save()
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			w = nil
			db, err = mcdb.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			actorsAfter := seaLifecycleActors(t, db, chunks)
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(actorsAfter, actorsBefore) {
				t.Fatalf("native chest loot changed after reload/re-save: before=%v after=%v", actorsBefore, actorsAfter)
			}
		})
	}
}

func TestSeaNeighborPreviewDoesNotConsumeHeightAdjustment(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, kind := range []string{"shipwreck", "shipwreck_beached", "ocean_ruin_warm", "ocean_ruin_cold"} {
		t.Run(kind, func(t *testing.T) {
			g, start, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				t.Fatal(err)
			}
			preview := world.ChunkPos{(start.box.Max[0] >> 4) + 1, start.box.Min[2] >> 4}
			g.GenerateChunk(preview, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), world.GenerationContext{})
			state := g.owner.modernStructures.pieceStates.lookup(start.key)
			if state != nil {
				state.mu.Lock()
				defer state.mu.Unlock()
				for i, flags := range state.pieces {
					if flags.TemplatePositionYSet || flags.BoundingBoxYSet || flags.HeightAdjusted {
						t.Fatalf("preview consumed piece%d Source vertical state: %+v", i, flags)
					}
				}
			}
		})
	}
}
