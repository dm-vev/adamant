package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type seaPostCase struct {
	Family, Template, Mode, Support string
	Rotation                        int
	Warm, Large                     bool
	WorldSeed                       string `json:"world_seed"`
	PostSeed                        string `json:"post_seed"`
	Origin                          [3]int32
	RawBox                          [6]int32 `json:"raw_box"`
	Volume                          [6]int32
	Calls                           []struct {
		Clip, BBox     [6]int32
		TP             [3]int32
		HeightAdjusted bool   `json:"height_adjusted"`
		NextLong       string `json:"next_long"`
		States         [][4]int32
		Actors         []struct {
			Pos       [3]int32
			ID        string
			LootTable string `json:"loot_table"`
			LootSeed  string `json:"loot_seed"`
		}
		Entities []struct {
			ID         string
			Pos        [3]float64
			Persistent bool
			Health     float64
		}
	}
}

type seaPostTerrain struct {
	modernTerrainSource
	family, support string
	states          map[string]world.NativeState
}

func newSeaPostTerrain(t *testing.T, family, support string) seaPostTerrain {
	t.Helper()
	terrain := seaPostTerrain{family: family, support: support, states: make(map[string]world.NativeState)}
	for _, name := range []string{"air", "stone", "water", "ice", "cave_air", "void_air", "bubble_column", "oak_fence"} {
		var properties map[string]any
		if name == "oak_fence" {
			properties = map[string]any{"waterlogged": true}
		}
		state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name, Properties: properties})
		if err != nil {
			t.Fatal(err)
		}
		terrain.states[name] = state
	}
	return terrain
}

func (s seaPostTerrain) BaseHeight(x, z int32, oceanFloor bool) int32 {
	switch s.family {
	case "shipwreck":
		if !oceanFloor {
			return 63
		}
		return 51 + ((3*x+5*z)%7+7)%7
	case "shipwreck_beached":
		return 63 + ((3*x+5*z)%7+7)%7
	default:
		return 65
	}
}
func (s seaPostTerrain) BaseState(x, y, z int32) world.NativeState {
	// A prior feature keeps the support section non-empty. ProtoChunk's
	// empty-section shortcut would otherwise collapse distinct air variants.
	if x == 15 && y == 70 && z == 15 {
		return s.states["stone"]
	}
	h := s.BaseHeight(x, z, true)
	if y >= -10 && y < h {
		if !strings.HasPrefix(s.family, "shipwreck") && x >= 0 && x < 16 && z >= 0 && z < 16 && y >= h-5 && s.support != "flat" {
			name := s.support
			if name == "waterlogged" {
				name = "oak_fence"
			}
			return s.states[name]
		}
		return s.states["stone"]
	}
	if s.family == "shipwreck" && y >= h && y < 63 {
		return s.states["water"]
	}
	return s.states["air"]
}
func (s seaPostTerrain) BaseBlock(x, y, z int32) world.Block {
	b, _ := world.DefaultBlockRegistry.BlockByRuntimeID(s.BaseState(x, y, z).Block)
	return b
}

func TestSeaPostProcessingMatchesReference(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_sea_post.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Palette map[uint32]struct {
			Name       string
			Properties map[string]any
		}
		Cases []seaPostCase
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	sourceIDs := make(map[uint32]uint32, len(fixture.Palette))
	for sourceID, state := range fixture.Palette {
		id, err := gamedata.BlockStateID(state.Name, state.Properties)
		if err != nil {
			t.Fatal(err)
		}
		sourceIDs[sourceID] = id
	}
	for _, source := range fixture.Cases {
		t.Run(fmt.Sprintf("%s/rotation_%d/%s", source.Family, source.Rotation, source.Mode), func(t *testing.T) {
			worldSeed, err := strconv.ParseInt(source.WorldSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			postSeed, err := strconv.ParseInt(source.PostSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			template, err := structure.Load(source.Template)
			if err != nil {
				t.Fatal(err)
			}
			terrain := newSeaPostTerrain(t, source.Family, source.Support)
			terrain.modernTerrainSource = modernTerrainSource{generator: NewOverworld(worldSeed)}
			target := world.ChunkPos{source.Origin[0] >> 4, source.Origin[2] >> 4}
			for _, call := range source.Calls {
				if len(call.Entities) != 0 {
					target = world.ChunkPos{int32(call.Entities[0].Pos[0]) >> 4, int32(call.Entities[0].Pos[2]) >> 4}
					break
				}
			}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
			for x := int32(0); x < 16; x++ {
				for z := int32(0); z < 16; z++ {
					for y := int32(-64); y <= 319; y++ {
						world.SetNativeState(c, uint8(x), int16(y), uint8(z), terrain.BaseState(target[0]*16+x, y, target[1]*16+z))
					}
				}
			}
			features := &ModernFeatures{seed: worldSeed, source: terrain}
			result := new(world.GenerationResult)
			region := newFeatureRegion(features, c, target, result)
			region.SetOrigin(source.Origin[0]>>4, source.Origin[2]>>4)
			flags := structure.StructurePieceState{}
			env := structure.ProcessorEnvironment{
				BaseHeight: terrain.BaseHeight, Range: c.Range(), WorldSeed: worldSeed, SeaLevel: 63, PieceState: &flags,
				LevelRandom: region.RegionRandom(), BiomeAt: terrain.BiomeAt,
				BlockStateID:        func(x, y, z int32) (uint32, error) { return region.BlockStateID(cube.Pos{int(x), int(y), int(z)}) },
				HasChestBlockEntity: func(x, y, z int32) bool { return region.hasChestBlockEntity(cube.Pos{int(x), int(y), int(z)}) },
				SetBlockEntityLootTable: func(x, y, z int32, table string, seed int64) {
					region.setBlockEntityLootTable(cube.Pos{int(x), int(y), int(z)}, table, seed)
				},
			}
			var piece modernStructurePiece
			if strings.HasPrefix(source.Family, "shipwreck") {
				piece = &modernShipwreckPiece{template: template, templateName: source.Template, origin: source.Origin, rotation: structure.Rotation(source.Rotation), beached: source.Family == "shipwreck_beached", seed: worldSeed, box: structure.Box{Min: [3]int32{source.RawBox[0], source.RawBox[1], source.RawBox[2]}, Max: [3]int32{source.RawBox[3], source.RawBox[4], source.RawBox[5]}}}
			} else {
				ruin := modernOceanRuinPiece{template: template, templateName: source.Template, origin: source.Origin, rotation: structure.Rotation(source.Rotation), integrity: .8, warm: source.Warm, large: source.Large, seed: worldSeed}
				ruin.processors, err = oceanRuinProcessors(ruin)
				if err != nil {
					t.Fatal(err)
				}
				piece = ruin
			}
			emit := func(placed structure.Placement) {
				placed.StructurePieceWrites = true
				features.applyStructurePlacement(region, placed)
			}
			for i, call := range source.Calls {
				random := vanilla.NewLegacyRandom(postSeed + int64(i))
				env.Random = &random
				clip := structure.Bounds{Min: [3]int32{call.Clip[0], call.Clip[1], call.Clip[2]}, Max: [3]int32{call.Clip[3], call.Clip[4], call.Clip[5]}}
				placed, err := piece.Place(clip, env)
				if err != nil {
					t.Fatal(err)
				}
				placed.TemplateUpdateFlags = 2
				emit(placed)
				firstEntity := len(result.Entities)
				if err = piece.(interface {
					AfterTemplatePlacement(structure.Bounds, structure.ProcessorEnvironment, func(structure.Placement)) error
				}).AfterTemplatePlacement(clip, env, emit); err != nil {
					t.Fatal(err)
				}
				if !flags.TemplatePositionYSet || flags.TemplatePositionY != call.TP[1] || !flags.BoundingBoxYSet || flags.BoundingBoxMinY != call.BBox[1] || flags.HeightAdjusted != call.HeightAdjusted {
					t.Errorf("call %d state=%+v, Source TP=%v bbox=%v heightAdjusted=%t", i, flags, call.TP, call.BBox, call.HeightAdjusted)
				}
				wantNext, err := strconv.ParseInt(call.NextLong, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				if got := random.NextLong(); got != wantNext {
					t.Errorf("call %d postProcess random continuation=%d, Source=%d", i, got, wantNext)
				}
				wantStates := make(map[[3]int32]uint32, len(call.States))
				for _, state := range call.States {
					wantStates[[3]int32{state[0], state[1], state[2]}] = sourceIDs[uint32(state[3])]
				}
				gotStates := make(map[[3]int32]uint32)
				for y := source.Volume[1]; y <= source.Volume[4]; y++ {
					for x := source.Volume[0]; x <= source.Volume[3]; x++ {
						for z := source.Volume[2]; z <= source.Volume[5]; z++ {
							pos := [3]int32{x, y, z}
							got, err := region.BlockStateID(cube.Pos{int(x), int(y), int(z)})
							if err != nil {
								t.Fatal(err)
							}
							if got != terrain.BaseState(x, y, z).CanonicalID(world.DefaultBlockRegistry) {
								gotStates[pos] = got
							}
						}
					}
				}
				if !reflect.DeepEqual(gotStates, wantStates) {
					differences := 0
					for pos, want := range wantStates {
						if gotStates[pos] != want {
							if differences < 8 {
								t.Errorf("call %d block %v=%d, Source=%d", i, pos, gotStates[pos], want)
							}
							differences++
						}
					}
					for pos, got := range gotStates {
						if _, exists := wantStates[pos]; !exists {
							if differences < 8 {
								t.Errorf("call %d extra native block %v=%d", i, pos, got)
							}
							differences++
						}
					}
					t.Errorf("call %d final state differences=%d", i, differences)
				}
				wantActors := make(map[cube.Pos]map[string]any, len(call.Actors))
				for _, actor := range call.Actors {
					seed, err := strconv.ParseInt(actor.LootSeed, 10, 64)
					if err != nil {
						t.Fatal(err)
					}
					id := map[string]string{"minecraft:chest": "Chest", "minecraft:brushable_block": "BrushableBlock"}[actor.ID]
					if id == "" {
						t.Fatalf("unhandled Source actor type %s", actor.ID)
					}
					wantActors[cube.Pos{int(actor.Pos[0]), int(actor.Pos[1]), int(actor.Pos[2])}] = map[string]any{"id": id, "LootTable": actor.LootTable, "LootTableSeed": seed}
				}
				gotActors := make(map[cube.Pos]map[string]any, len(region.blockActorData))
				for pos, actor := range region.blockActorData {
					table, _ := actor["LootTable"].(string)
					seed, _ := actor["LootTableSeed"].(int64)
					gotActors[pos] = map[string]any{"id": actor["id"], "LootTable": table, "LootTableSeed": seed}
				}
				if !reflect.DeepEqual(gotActors, wantActors) {
					t.Errorf("call %d actors=%v, Source=%v", i, gotActors, wantActors)
				}
				if len(result.Entities)-firstEntity != len(call.Entities) {
					t.Errorf("call %d native entities=%d, Source=%d", i, len(result.Entities)-firstEntity, len(call.Entities))
				}
				if len(call.Entities) != 0 {
					w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
					job := w.Do(func(tx *world.Tx) {
						for j, handle := range result.Entities[firstEntity:] {
							e := tx.AddEntity(handle)
							if j >= len(call.Entities) {
								continue
							}
							want := call.Entities[j]
							drowned, ok := e.(*entity.Drowned)
							if !ok {
								t.Errorf("call %d actual entity=%T", i, e)
								continue
							}
							if e.H().Type().EncodeEntity() != want.ID || [3]float64(e.Position()) != want.Pos || drowned.Health() != want.Health {
								t.Errorf("call %d actual drowned pos=%v health=%v, Source pos=%v health=%v", i, e.Position(), drowned.Health(), want.Pos, want.Health)
							}
							if e.H().SpawnReason() != "structure" {
								t.Errorf("call %d drowned skipped Source finalizeSpawn: reason=%q", i, e.H().SpawnReason())
							}
						}
					})
					<-job.Done()
					if err = job.Err(); err != nil {
						t.Fatal(err)
					}
					if err = w.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
	}
}
