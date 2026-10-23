package overworld

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

type scatteredLifecycleTerrain struct {
	modernTerrainSource
	context    *modernStructureContext
	stone, air world.NativeState
}

func (s scatteredLifecycleTerrain) NoiseBiomeAt(int32, int32, int32) string {
	return "minecraft:plains"
}
func (s scatteredLifecycleTerrain) BiomeAt(int32, int32, int32) string { return "minecraft:plains" }
func (s scatteredLifecycleTerrain) BaseHeight(x, z int32, _ bool) int32 {
	return 63 + ((3*x+5*z)%7+7)%7
}
func (s scatteredLifecycleTerrain) BaseState(x, y, z int32) world.NativeState {
	if y >= 0 && y < s.BaseHeight(x, z, false) {
		return s.stone
	}
	return s.air
}
func (s scatteredLifecycleTerrain) BaseBlock(x, y, z int32) world.Block {
	b, _ := world.DefaultBlockRegistry.BlockByRuntimeID(s.BaseState(x, y, z).Block)
	return b
}
func (s scatteredLifecycleTerrain) PlaceStructureStep(pos world.ChunkPos, step int, env structure.ProcessorEnvironment, emit func(structure.Placement)) {
	// Production looks up each origin's own structure references.
	if !intersectsChunk(s.context.starts[0].box, pos) {
		return
	}
	s.context.PlaceStep(pos, world.Overworld.Range(), step, env, emit)
}
func (scatteredLifecycleTerrain) UseLegacyRandomSource() bool { return false }
func (scatteredLifecycleTerrain) HasSkyLight() bool           { return true }
func (scatteredLifecycleTerrain) FluidTickDelay(string) int32 { return 5 }

type scatteredLifecycleGenerator struct {
	owner    *Overworld
	features *ModernFeatures
	terrain  scatteredLifecycleTerrain
}

func (g *scatteredLifecycleGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, context world.GenerationContext) world.GenerationResult {
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			h := g.terrain.BaseHeight(int32(pos[0])*16+int32(x), int32(pos[1])*16+int32(z), false)
			for y := int16(0); y < int16(h); y++ {
				world.SetNativeState(c, x, y, z, g.terrain.stone)
			}
		}
	}
	return g.features.Decorate(pos, c, context)
}
func (*scatteredLifecycleGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 70, 0} }
func (g *scatteredLifecycleGenerator) LoadGenerationState(data []byte) error {
	return g.owner.LoadGenerationState(data)
}
func (g *scatteredLifecycleGenerator) SaveGenerationState() ([]byte, error) {
	return g.owner.SaveGenerationState()
}
func newScatteredLifecycleGenerator(kind string) (*scatteredLifecycleGenerator, modernStructureStart, error) {
	owner := &Overworld{seed: 4096, modernStructures: new(modernStructures)}
	random := vanilla.NewLegacyRandom(4096)
	in := modernStructureBuildInput{seed: 4096, name: "minecraft:" + kind, source: scatteredLifecycleTerrain{}, random: &random}
	var start modernStructureStart
	var err error
	switch kind {
	case "jungle_temple":
		start, _, err = buildModernJungleTemple(in)
	case "desert_pyramid":
		start, _, err = buildModernDesertPyramid(in)
	case "igloo":
		random = vanilla.NewLegacyRandom(13) // Source fixture: rotation 2, basement depth 4.
		start, _, err = buildModernIgloo(in)
	case "shipwreck", "shipwreck_beached", "ocean_ruin_warm", "ocean_ruin_cold":
		in.config, err = gamedata.LoadJSON("structure", kind)
		if err != nil {
			return nil, start, err
		}
		if kind == "shipwreck" || kind == "shipwreck_beached" {
			start, _, err = buildModernShipwreck(in)
		} else {
			start, _, err = buildModernOceanRuin(in)
		}
	default:
		start, _, err = buildModernSwampHut(in)
	}
	if err != nil {
		return nil, start, err
	}
	start.key = modernStructureStartKey{set: kind}
	start.step = 3
	start.mutablePieces = hasMutableStructurePieceState(start.pieces)
	context := &modernStructureContext{pieceStates: &owner.modernStructures.pieceStates, starts: []modernStructureStart{start}, structureSteps: map[int][]modernStructureStepEntry{3: {{name: start.name, index: 0}}}}
	stone, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:stone"})
	if err != nil {
		return nil, start, err
	}
	air, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:air"})
	if err != nil {
		return nil, start, err
	}
	terrain := scatteredLifecycleTerrain{context: context, stone: stone, air: air}
	features := &ModernFeatures{seed: 4096, source: terrain, biomes: map[string]modernBiomeFeatures{"plains": {}}}
	return &scatteredLifecycleGenerator{owner: owner, features: features, terrain: terrain}, start, nil
}
func TestScatteredStateSurvivesNativeMCDBRestart(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, kind := range []string{"jungle_temple", "swamp_hut"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			generator, start, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				t.Fatal(err)
			}
			db, err := mcdb.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			w := world.Config{Provider: db, Generator: generator, Entities: entity.DefaultRegistry, Synchronous: true}.New()
			t.Cleanup(func() {
				if w != nil {
					_ = w.Close()
				}
			})
			health := map[string]float64{}
			task := w.Do(func(tx *world.Tx) {
				tx.Block(cube.Pos{0, 64, 0})
				for e := range tx.Entities() {
					if living, ok := e.(entity.Living); ok {
						living.Hurt(2, entity.ExplosionDamageSource{})
						health[e.H().Type().EncodeEntity()] = living.Health()
					}
				}
			})
			<-task.Done()
			if err = task.Err(); err != nil {
				t.Fatal(err)
			}
			w.Save()
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			w = nil
			flags := generator.owner.modernStructures.pieceStates.lookup(start.key)
			flags.mu.Lock()
			before := flags.pieces[0]
			flags.mu.Unlock()
			if !before.GroundHeightSet {
				t.Fatal("real generation lost HPos")
			}
			if kind == "jungle_temple" && before.LootPlaced != 15 {
				t.Fatalf("temple did not consume all real loot slots: %+v", before)
			}
			if kind == "swamp_hut" && (before.EntitiesSpawned != 3 || !reflect.DeepEqual(health, map[string]float64{"minecraft:cat": 8, "minecraft:witch": 24})) {
				t.Fatalf("hut did not generate both damaged mobs: state=%+v health=%v", before, health)
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
			sourceSpawnState := map[string]map[string]any{}
			if kind == "swamp_hut" {
				if len(column.Entities) != 2 {
					_ = db.Close()
					t.Fatalf("saved hut mob count=%d, want2", len(column.Entities))
				}
				for _, saved := range column.Entities {
					state, _ := saved.Data["adamant:java"].(map[string]any)
					if state == nil || state["attributes"] == nil || state["LeftHanded"] == nil {
						_ = db.Close()
						t.Fatalf("saved mob lost source finalization state: %v", saved.Data)
					}
					if state["id"] == "minecraft:cat" && (state["variant"] != "minecraft:all_black" || state["sound_variant"] == nil) {
						_ = db.Close()
						t.Fatalf("saved hut cat lost variant/sound: %v", state)
					}
					id := state["id"].(string)
					sourceSpawnState[id] = map[string]any{}
					for _, key := range []string{"attributes", "LeftHanded", "variant", "sound_variant", "PersistenceRequired"} {
						if value, ok := state[key]; ok {
							sourceSpawnState[id][key] = value
						}
					}
				}
			}
			restored, restoredStart, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				_ = db.Close()
				t.Fatal(err)
			}
			w = world.Config{Provider: db, Generator: restored, Entities: entity.DefaultRegistry, Synchronous: true}.New()
			loadedHealth := map[string]float64{}
			task = w.Do(func(tx *world.Tx) {
				tx.Block(cube.Pos{0, 64, 0})
				for e := range tx.Entities() {
					if living, ok := e.(entity.Living); ok {
						loadedHealth[e.H().Type().EncodeEntity()] = living.Health()
					}
				}
			})
			<-task.Done()
			if err = task.Err(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(loadedHealth, health) {
				t.Fatalf("mob health changed on restart: got%v want%v", loadedHealth, health)
			}
			c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			result := restored.GenerateChunk(world.ChunkPos{0, 0}, c, world.GenerationContext{})
			if len(result.Entities) != 0 {
				t.Fatalf("restarted hut duplicated %d mobs", len(result.Entities))
			}
			for _, actor := range result.BlockEntities {
				if actor.Data["LootTable"] != nil || actor.Data["LootTableSeed"] != nil {
					t.Fatalf("restarted temple regenerated loot actor: %v", actor)
				}
			}
			state := restored.owner.modernStructures.pieceStates.lookup(restoredStart.key)
			state.mu.Lock()
			after := state.pieces[0]
			state.mu.Unlock()
			if after != before {
				t.Fatalf("restart changed source state: before=%+v after=%+v", before, after)
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			w = nil
			db, err = mcdb.Open(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			column, err = db.LoadColumn(world.ChunkPos{0, 0}, world.Overworld)
			if err != nil {
				t.Fatal(err)
			}
			for _, saved := range column.Entities {
				state, _ := saved.Data["adamant:java"].(map[string]any)
				id, _ := state["id"].(string)
				got := map[string]any{}
				for _, key := range []string{"attributes", "LeftHanded", "variant", "sound_variant", "PersistenceRequired"} {
					if value, ok := state[key]; ok {
						got[key] = value
					}
				}
				if !reflect.DeepEqual(got, sourceSpawnState[id]) {
					t.Fatalf("%s source spawn state changed on decode/re-save: got%v want%v", id, got, sourceSpawnState[id])
				}
			}
		})
	}
}

func TestScatteredNeighborPreviewDoesNotConsumeHPosLootOrMobs(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	for _, kind := range []string{"jungle_temple", "swamp_hut"} {
		t.Run(kind, func(t *testing.T) {
			generator, start, err := newScatteredLifecycleGenerator(kind)
			if err != nil {
				t.Fatal(err)
			}
			generator.GenerateChunk(world.ChunkPos{-1, 0}, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), world.GenerationContext{})
			flags := generator.owner.modernStructures.pieceStates.lookup(start.key)
			if flags != nil {
				flags.mu.Lock()
				before := flags.pieces[0]
				flags.mu.Unlock()
				if before != (structure.StructurePieceState{}) {
					t.Fatalf("neighbor preview consumed real state: %+v", before)
				}
			}
			result := generator.GenerateChunk(world.ChunkPos{0, 0}, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()), world.GenerationContext{})
			flags = generator.owner.modernStructures.pieceStates.lookup(start.key)
			flags.mu.Lock()
			after := flags.pieces[0]
			flags.mu.Unlock()
			if !after.GroundHeightSet || after.GroundHeight != 66 {
				t.Fatalf("real target did not sample live HPos after preview: %+v", after)
			}
			if kind == "swamp_hut" {
				defer func() {
					for _, e := range result.Entities {
						_ = e.Close()
					}
				}()
				if after.EntitiesSpawned != 3 || len(result.Entities) != 2 {
					t.Fatalf("preview consumed hut mobs: %+v entities=%d", after, len(result.Entities))
				}
			} else {
				slots := map[string]int{}
				for _, actor := range result.BlockEntities {
					if table, ok := actor.Data["LootTable"].(string); ok {
						slots[table]++
					}
				}
				if after.LootPlaced != 15 || !reflect.DeepEqual(slots, map[string]int{"minecraft:chests/jungle_temple": 2, "minecraft:chests/jungle_temple_dispenser": 2}) {
					t.Fatalf("preview consumed temple loot: %+v slots=%v", after, slots)
				}
			}
		})
	}
}
