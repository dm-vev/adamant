package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func TestMansionPostProcessMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_mansion_post_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Seed            string             `json:"seed"`
		Chunk           [2]int32           `json:"chunk"`
		PieceIndex      int                `json:"pieceIndex"`
		GraphPieceIndex int                `json:"graphPieceIndex"`
		Template        string             `json:"template"`
		Position        [3]int32           `json:"position"`
		MarkerPosition  [3]int32           `json:"markerPosition"`
		Rotation        structure.Rotation `json:"rotation"`
		Mirror          structure.Mirror   `json:"mirror"`
		ClipMode        string             `json:"clipMode"`
		Calls           []struct {
			Clip         [6]int32 `json:"clip"`
			NextPostLong string   `json:"nextPostLong"`
		} `json:"calls"`
		Actors []struct {
			ID string `json:"id"`
		} `json:"actors"`
		FinalMap []struct {
			Position [3]int32 `json:"position"`
			State    struct {
				Name       string         `json:"name"`
				Properties map[string]any `json:"properties"`
			} `json:"state"`
		} `json:"finalMap"`
		AllEntities []struct {
			ID  string     `json:"id"`
			Pos [3]float64 `json:"pos"`
		} `json:"allEntities"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%s/%d/%s/%s", fixture.Seed, fixture.PieceIndex, fixture.Template, fixture.ClipMode), func(t *testing.T) {
			seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			random := largeFeatureRandom(seed, fixture.Chunk[0], fixture.Chunk[1])
			rotation := structure.Rotation(random.NextInt(4))
			plan := newMansionPlan(&random)
			placer := newMansionPlacer([3]int32{0, 70, 0}, rotation, seed, &random)
			if err = placer.create(plan); err != nil {
				t.Fatal(err)
			}
			if fixture.GraphPieceIndex < 0 || fixture.GraphPieceIndex >= len(placer.pieces) {
				t.Fatalf("graph piece index %d outside %d generated pieces", fixture.GraphPieceIndex, len(placer.pieces))
			}
			piece := placer.pieces[fixture.GraphPieceIndex].(modernMansionPiece)
			name := ""
			for templateName, template := range placer.templates {
				if template == piece.template {
					name = templateName
					break
				}
			}
			if name != fixture.Template || piece.origin != fixture.Position || piece.rotation != fixture.Rotation || piece.mirror != fixture.Mirror {
				t.Fatalf("piece=%s at %v rotation=%d mirror=%d, Source=%s at %v rotation=%d mirror=%d", name, piece.origin, piece.rotation, piece.mirror, fixture.Template, fixture.Position, fixture.Rotation, fixture.Mirror)
			}

			target := world.ChunkPos{fixture.MarkerPosition[0] >> 4, fixture.MarkerPosition[2] >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{-10, 125})
			features := &ModernFeatures{}
			region := newFeatureRegion(features, c, target, &world.GenerationResult{})
			region.SetOrigin(target[0], target[1])
			stone, err := world.ResolveState(c.BlockRegistry(), world.BlockState{Name: "minecraft:stone"})
			if err != nil {
				t.Fatal(err)
			}
			air, err := world.ResolveState(c.BlockRegistry(), world.BlockState{Name: "minecraft:air"})
			if err != nil {
				t.Fatal(err)
			}
			// Template fluid reads cross the target boundary. Use the same
			// stone-to-Y69 Source profile for every queried neighbor column.
			features.source = iglooFixtureTerrain{
				height: 69, stone: stone, air: air,
				support: [3]int32{0, -11, 0},
			}
			for x := uint8(0); x < 16; x++ {
				for z := uint8(0); z < 16; z++ {
					for y := int16(-10); y <= 69; y++ {
						world.SetNativeState(c, x, y, z, stone)
					}
				}
			}
			levelRandom := vanilla.NewXoroshiroRandom(int64(81000 + fixture.PieceIndex))
			baseEnv := structure.ProcessorEnvironment{
				BaseBlock:         func(x, y, z int32) world.Block { return region.Block(featurePos{x, y, z}) },
				LevelRandom:       &levelRandom,
				GenerationContext: world.GenerationContext{},
				Range:             c.Range(),
				BlockStateID: func(x, y, z int32) (uint32, error) {
					return region.BlockStateID(cube.Pos{int(x), int(y), int(z)})
				},
				HasChestBlockEntity: func(x, y, z int32) bool {
					return region.hasChestBlockEntity(cube.Pos{int(x), int(y), int(z)})
				},
				SetBlockEntityLootTable: func(x, y, z int32, table string, lootSeed int64) {
					region.setBlockEntityLootTable(cube.Pos{int(x), int(y), int(z)}, table, lootSeed)
				},
			}
			var entities []structure.Entity
			for callIndex, call := range fixture.Calls {
				postRandom := vanilla.NewLegacyRandom(int64(92000 + fixture.PieceIndex + callIndex))
				env := baseEnv
				env.Random = &postRandom
				clip := structure.Bounds{Min: [3]int32{call.Clip[0], call.Clip[1], call.Clip[2]}, Max: [3]int32{call.Clip[3], call.Clip[4], call.Clip[5]}}
				placement, err := piece.Place(clip, env)
				if err != nil {
					t.Fatal(err)
				}
				// The production structure wrapper supplies these TemplateStructurePiece settings.
				placement.TemplateUpdateFlags, placement.TemplateKnownShape = 2, false
				features.applyStructurePlacement(region, placement)
				entities = append(entities, placement.Entities...)
				err = piece.AfterTemplatePlacement(clip, env, func(post structure.Placement) {
					features.applyStructurePlacement(region, post)
					entities = append(entities, post.PostShapeEntities...)
				})
				if err != nil {
					t.Fatal(err)
				}
				wantNext, err := strconv.ParseInt(call.NextPostLong, 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				if got := postRandom.NextLong(); got != wantNext {
					t.Errorf("call %d postProcess RNG continuation=%d, Source=%d", callIndex, got, wantNext)
				}
			}

			wantStates := make(map[[3]int32]string, len(fixture.FinalMap))
			for _, entry := range fixture.FinalMap {
				wantStates[entry.Position] = mansionSourceStateKey(entry.State.Name, entry.State.Properties)
			}
			gotStates := make(map[[3]int32]string)
			for x := int32(0); x < 16; x++ {
				for z := int32(0); z < 16; z++ {
					for y := int32(-10); y <= 125; y++ {
						pos := featurePos{(target[0] << 4) + x, y, (target[1] << 4) + z}
						state, err := world.CanonicalState(c.BlockRegistry(), region.nativeState(pos))
						if err != nil {
							t.Fatal(err)
						}
						if y <= 69 && state.Name == "minecraft:stone" || y > 69 && state.Name == "minecraft:air" {
							continue
						}
						gotStates[[3]int32{pos.x, pos.y, pos.z}] = mansionSourceStateKey(state.Name, state.Properties)
					}
				}
			}
			if len(gotStates) != len(wantStates) {
				t.Errorf("final block map entries=%d, Source=%d", len(gotStates), len(wantStates))
			}
			for pos, want := range wantStates {
				if got := gotStates[pos]; got != want {
					t.Errorf("final block at %v=%s, Source=%s", pos, got, want)
				}
			}
			for pos, got := range gotStates {
				if _, ok := wantStates[pos]; !ok {
					t.Errorf("unexpected final block at %v=%s", pos, got)
				}
			}
			if len(region.blockActorData) != len(fixture.Actors) {
				t.Errorf("block actors=%d, Source=%d", len(region.blockActorData), len(fixture.Actors))
			}
			if len(entities) != len(fixture.AllEntities) {
				t.Errorf("final entities=%d, Source=%d", len(entities), len(fixture.AllEntities))
			}
			for i, want := range fixture.AllEntities {
				if i >= len(entities) {
					break
				}
				got := entities[i]
				id, _ := got.NBT["id"].(string)
				if id != want.ID || got.Pos[0] != want.Pos[0] || got.Pos[1] != want.Pos[1] || got.Pos[2] != want.Pos[2] || got.NBT["PersistenceRequired"] != byte(1) {
					t.Errorf("entity %d=%s at %v persistence=%v, Source=%s at %v", i, id, got.Pos, got.NBT["PersistenceRequired"], want.ID, want.Pos)
				}
			}
		})
	}
}

func mansionSourceStateKey(name string, properties map[string]any) string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, key := range keys {
		fmt.Fprintf(&b, ":%s=%v", key, properties[key])
	}
	return b.String()
}
