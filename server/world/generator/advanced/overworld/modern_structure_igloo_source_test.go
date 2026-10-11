package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type iglooGraphFixture struct {
	Seed     int64                    `json:"seed"`
	Rotation int                      `json:"rotation"`
	Basement bool                     `json:"basement"`
	Depth    int                      `json:"depth"`
	BBox     [6]int32                 `json:"bbox"`
	Pieces   []iglooGraphFixturePiece `json:"pieces"`
	NextLong int64                    `json:"nextLong"`
}

type iglooGraphFixturePiece struct {
	Name             string   `json:"name"`
	Depth            int32    `json:"depth"`
	BBox             [6]int32 `json:"bbox"`
	TemplatePosition [3]int32 `json:"template_position"`
	Pivot            [3]int32 `json:"pivot"`
	Rotation         int      `json:"rotation"`
}

func TestIglooGraphsMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_igloo_graphs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []iglooGraphFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	seenRotation := [4]bool{}
	seenBasement, seenTopOnly, seenMinDepth, seenMaxDepth := false, false, false, false
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fmt.Sprintf("seed_%d", fixture.Seed), func(t *testing.T) {
			random := java26.NewLegacyRandom(fixture.Seed)
			start, valid, err := buildModernIgloo(modernStructureBuildInput{seed: fixture.Seed, name: "igloo", chunkX: 2, chunkZ: -3, random: &random})
			if err != nil {
				t.Fatal(err)
			}
			if !valid || !start.mutablePieces {
				t.Fatalf("igloo start valid=%v mutable=%v", valid, start.mutablePieces)
			}
			wantStartBox := javastructure.Box{Min: [3]int32{fixture.BBox[0], fixture.BBox[1], fixture.BBox[2]}, Max: [3]int32{fixture.BBox[3], fixture.BBox[4], fixture.BBox[5]}}
			if start.box != wantStartBox {
				t.Fatalf("start box=%v, Java=%v", start.box, wantStartBox)
			}
			if len(start.pieces) != len(fixture.Pieces) {
				t.Fatalf("piece count=%d, Java=%d", len(start.pieces), len(fixture.Pieces))
			}
			for i, raw := range start.pieces {
				piece := raw.(modernIglooPiece)
				want := fixture.Pieces[i]
				wantBox := javastructure.Box{Min: [3]int32{want.BBox[0], want.BBox[1], want.BBox[2]}, Max: [3]int32{want.BBox[3], want.BBox[4], want.BBox[5]}}
				if piece.name != strings.TrimPrefix(want.Name, "minecraft:") || piece.origin != want.TemplatePosition || piece.pivot != want.Pivot || int(piece.rotation) != want.Rotation || piece.box != wantBox {
					t.Errorf("piece %d = {%s pos=%v pivot=%v rotation=%d box=%v}, Java {%s pos=%v pivot=%v rotation=%d box=%v}", i, piece.name, piece.origin, piece.pivot, piece.rotation, piece.box, want.Name, want.TemplatePosition, want.Pivot, want.Rotation, wantBox)
				}
				gotDepth := int32(0)
				if piece.name != "igloo/top" {
					gotDepth = 87 - piece.origin[1]
				}
				if gotDepth != want.Depth {
					t.Errorf("piece %d depth=%d, Java=%d", i, gotDepth, want.Depth)
				}
			}
			if got := random.NextLong(); got != fixture.NextLong {
				t.Errorf("generation following nextLong=%d, Java=%d", got, fixture.NextLong)
			}
			seenRotation[fixture.Rotation] = true
			seenBasement = seenBasement || fixture.Basement
			seenTopOnly = seenTopOnly || !fixture.Basement
			seenMinDepth = seenMinDepth || fixture.Depth == 4
			seenMaxDepth = seenMaxDepth || fixture.Depth == 11
		})
	}
	for rotation, seen := range seenRotation {
		if !seen {
			t.Errorf("Java graph fixture misses rotation %d", rotation)
		}
	}
	if !seenBasement || !seenTopOnly || !seenMinDepth || !seenMaxDepth {
		t.Fatalf("incomplete graph fixture coverage: basement=%v topOnly=%v depths=%v/%v", seenBasement, seenTopOnly, seenMinDepth, seenMaxDepth)
	}
}

type iglooPostEntity struct {
	ID       string     `json:"id"`
	Pos      [3]float64 `json:"pos"`
	Rotation [2]float64 `json:"rotation"`
	NBT      string     `json:"nbt"`
}

func iglooVecEqual(got [3]float64, want [3]float64) bool {
	for axis := range 3 {
		if absIgloo(got[axis]-want[axis]) > 1e-6 {
			return false
		}
	}
	return true
}

func absIgloo(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func iglooNumber(value any) (float64, bool) {
	switch n := value.(type) {
	case float32:
		return float64(n), true
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

type iglooFixtureTerrain struct {
	modernTerrainSource
	height                    int32
	air, stone, water, ladder world.NativeState
	support                   [3]int32
	supportState              world.NativeState
	fluid                     [3]int32
	hasFluid                  bool
}

func (s iglooFixtureTerrain) BaseHeight(int32, int32, bool) int32 { return s.height + 1 }
func (s iglooFixtureTerrain) BaseState(x, y, z int32) world.NativeState {
	if s.hasFluid && [3]int32{x, y, z} == s.fluid {
		return s.water
	}
	if [3]int32{x, y, z} == s.support {
		return s.supportState
	}
	if y >= -10 && y <= s.height {
		return s.stone
	}
	return s.air
}
func (s iglooFixtureTerrain) BaseBlock(x, y, z int32) world.Block {
	rid := s.BaseState(x, y, z).Block
	b, _ := world.DefaultBlockRegistry.BlockByRuntimeID(rid)
	return b
}

func TestIglooPostProcessMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_igloo_post.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		FinalRandom string `json:"finalRandom"`
		Piece       string `json:"piece"`
		Seed        string `json:"seed"`
		Rotation    int    `json:"rotation"`
		Height      int32  `json:"height"`
		ClipMode    string `json:"clipMode"`
		Support     string `json:"support"`
		ChestActor  bool   `json:"chestActor"`
		Calls       []struct {
			Clip    [6]int32 `json:"clip"`
			Writes  []string `json:"writes"`
			Markers []string `json:"markers"`
		} `json:"calls"`
		FinalBox         [6]int32          `json:"finalBox"`
		TemplatePosition [3]int32          `json:"templatePosition"`
		Entities         []iglooPostEntity `json:"entities"`
		Actors           map[string]string `json:"actors"`
		TrapdoorPos      [3]int32          `json:"trapdoorPos"`
		TrapdoorState    string            `json:"trapdoorState"`
		SupportState     string            `json:"supportState"`
		InputFluidPos    [3]int32          `json:"inputFluidPos"`
		InputFluidFinal  string            `json:"inputFluidFinal"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	native := func(name string, properties map[string]any) world.NativeState {
		state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name, Properties: properties})
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	for index, fixture := range fixtures {
		t.Run(fmt.Sprintf("%02d/%s/rotation_%d/%s", index, fixture.Piece, fixture.Rotation, fixture.ClipMode), func(t *testing.T) {
			seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			air := native("minecraft:air", nil)
			stone := native("minecraft:stone", nil)
			water := native("minecraft:water", map[string]any{"level": int32(0)})
			ladder := native("minecraft:ladder", map[string]any{"facing": "north", "waterlogged": false})
			support := stone
			switch fixture.Support {
			case "air", "none":
				support = air
			case "cave_air":
				support = native("minecraft:cave_air", nil)
			case "void_air":
				support = native("minecraft:void_air", nil)
			case "ladder":
				support = ladder
			}
			terrain := iglooFixtureTerrain{height: fixture.Height, air: air, stone: stone, water: water, ladder: ladder, support: [3]int32{fixture.TrapdoorPos[0], fixture.TrapdoorPos[1] - 1, fixture.TrapdoorPos[2]}, supportState: support}
			if fixture.InputFluidFinal != "" {
				terrain.fluid, terrain.hasFluid = fixture.InputFluidPos, true
			}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
			for x := int32(0); x < 16; x++ {
				for z := int32(0); z < 16; z++ {
					for y := int32(-10); y <= fixture.Height; y++ {
						world.SetNativeState(c, uint8(x), int16(y), uint8(z), terrain.BaseState(x, y, z))
					}
				}
			}
			world.SetNativeState(c, 8, int16(fixture.Height+1), 8, water)
			if terrain.hasFluid {
				p := terrain.fluid
				world.SetNativeState(c, uint8(p[0]&15), int16(p[1]), uint8(p[2]&15), water)
			}
			result := &world.GenerationResult{}
			features := &ModernFeatures{source: terrain}
			region := newFeatureRegion(features, c, world.ChunkPos{}, result)
			region.SetOrigin(0, 0)
			name, offset, depth := "igloo/top", [3]int32{}, int32(0)
			pivot := [3]int32{3, 5, 5}
			if fixture.Piece == "bottom" {
				name, offset, depth, pivot = "igloo/bottom", [3]int32{0, -3, -2}, 12, [3]int32{3, 6, 7}
			}
			piece, err := makeModernIglooPiece(name, [3]int32{0, 90, 0}, javastructure.Rotation(fixture.Rotation), depth, pivot, offset, seed)
			if err != nil {
				t.Fatal(err)
			}
			random := java26.NewLegacyRandom(seed)
			state := &javastructure.StructurePieceState{}
			env := javastructure.ProcessorEnvironment{
				BaseHeight: terrain.BaseHeight, Random: &random, PieceState: state, Range: c.Range(),
				JavaBlockStateID: func(x, y, z int32) (uint32, error) { return region.JavaBlockStateID(cube.Pos{int(x), int(y), int(z)}) },
				HasChestBlockEntity: func(x, y, z int32) bool {
					return fixture.ChestActor && region.hasChestBlockEntity(cube.Pos{int(x), int(y), int(z)})
				},
				SetBlockEntityLootTable: func(x, y, z int32, table string, lootSeed int64) {
					region.setBlockEntityLootTable(cube.Pos{int(x), int(y), int(z)}, table, lootSeed)
				},
			}
			expected := make(map[[3]int32]string)
			var placedEntities []javastructure.Entity
			for _, call := range fixture.Calls {
				clip := javastructure.Bounds{Min: [3]int32{call.Clip[0], call.Clip[1], call.Clip[2]}, Max: [3]int32{call.Clip[3], call.Clip[4], call.Clip[5]}}
				placement, err := piece.Place(clip, env)
				if err != nil {
					t.Fatal(err)
				}
				features.applyStructurePlacement(region, placement)
				placedEntities = append(placedEntities, placement.Entities...)
				post, err := piece.AfterTemplatePlacement(clip, env)
				if err != nil {
					t.Fatal(err)
				}
				features.applyStructurePlacement(region, post)
				for _, write := range call.Writes {
					var pos [3]int32
					var stateText string
					if _, err := fmt.Sscanf(write, "%d,%d,%d=%s", &pos[0], &pos[1], &pos[2], &stateText); err != nil {
						t.Fatalf("parse source write %q: %v", write, err)
					}
					stateText = strings.TrimSuffix(stateText, ":true")
					expected[pos] = stateText
				}
			}
			if len(placedEntities) != len(fixture.Entities) {
				t.Fatalf("placed entities=%d, Source=%d", len(placedEntities), len(fixture.Entities))
			}
			for i, want := range fixture.Entities {
				got := placedEntities[i]
				id, _ := got.NBT["id"].(string)
				if id != want.ID || !iglooVecEqual(got.Pos, want.Pos) {
					t.Errorf("entity %d=%s at %v, Source=%s at %v", i, id, got.Pos, want.ID, want.Pos)
				}
				rotation, ok := got.NBT["Rotation"].([]any)
				if !ok || len(rotation) < 2 {
					t.Errorf("entity %d has malformed Rotation: %#v", i, got.NBT["Rotation"])
					continue
				}
				yaw, okYaw := iglooNumber(rotation[0])
				pitch, okPitch := iglooNumber(rotation[1])
				if !okYaw || !okPitch || absIgloo(yaw-want.Rotation[0]) > 1e-5 || absIgloo(pitch-want.Rotation[1]) > 1e-5 {
					t.Errorf("entity %d Rotation=%v, Source=%v", i, rotation, want.Rotation)
				}
			}
			handles, err := (javastructure.Placement{Entities: placedEntities}).NewEntityHandles(entity.NewFromJavaNBT)
			if err != nil {
				t.Fatal(err)
			}
			nativeWorld := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
			defer nativeWorld.Close()
			<-nativeWorld.Exec(func(tx *world.Tx) {
				for i, handle := range handles {
					got := tx.AddEntity(handle)
					want := fixture.Entities[i]
					if got.H().Type().EncodeEntity() != want.ID || !iglooVecEqual(got.Position(), want.Pos) ||
						absIgloo(got.Rotation()[0]-want.Rotation[0]) > 1e-5 || absIgloo(got.Rotation()[1]-want.Rotation[1]) > 1e-5 {
						t.Errorf("native entity %d=%s at %v rotation %v, Source=%s at %v rotation %v", i, got.H().Type().EncodeEntity(), got.Position(), got.Rotation(), want.ID, want.Pos, want.Rotation)
					}
					if prisoner, ok := got.(interface {
						Profession() string
						Variant() string
					}); ok {
						if prisoner.Profession() != actorNBTValue(want.NBT, "profession") || prisoner.Variant() != actorNBTValue(want.NBT, "type") {
							t.Errorf("native prisoner profession/type=%s/%s, Source NBT=%s", prisoner.Profession(), prisoner.Variant(), want.NBT)
						}
					}
					if living, ok := got.(interface{ Health() float64 }); ok {
						health, err := strconv.ParseFloat(strings.TrimSuffix(actorNBTValue(want.NBT, "Health"), "f"), 64)
						if err != nil {
							t.Fatal(err)
						}
						if absIgloo(living.Health()-health) > 1e-5 {
							t.Errorf("native prisoner health=%g, Source=%g", living.Health(), health)
						}
					}
				}
			})
			lastCall := fixture.Calls[len(fixture.Calls)-1]
			if len(result.PostProcessing) != len(lastCall.Markers) {
				t.Errorf("native postprocessing=%v, Source=%v", result.PostProcessing, lastCall.Markers)
			}
			for pos := range region.overlay {
				name, _ := region.State(pos)
				if name == "minecraft:structure_block" {
					t.Errorf("marker remained in final world state at %v", pos)
				}
			}
			for pos, want := range expected {
				got := scatteredCanonicalState(region.nativeState(featurePos{pos[0], pos[1], pos[2]}).CanonicalJavaID(c.BlockRegistry()))
				if got != want {
					t.Errorf("final state at %v=%s, Source=%s", pos, got, want)
				}
			}
			if state.BoundingBoxYSet != true || state.BoundingBoxMinY != fixture.FinalBox[1] {
				t.Errorf("piece bounding box state=%+v, Source minY=%d", state, fixture.FinalBox[1])
			}
			if piece.origin != fixture.TemplatePosition {
				t.Errorf("templatePosition=%v, Source=%v", piece.origin, fixture.TemplatePosition)
			}
			if fixture.TrapdoorState != "" {
				pos := featurePos{fixture.TrapdoorPos[0], fixture.TrapdoorPos[1], fixture.TrapdoorPos[2]}
				got := scatteredCanonicalState(region.nativeState(pos).CanonicalJavaID(c.BlockRegistry()))
				if got != fixture.TrapdoorState {
					t.Errorf("trapdoor=%s, Source=%s", got, fixture.TrapdoorState)
				}
				supportPos := featurePos{pos.x, pos.y - 1, pos.z}
				gotSupport := scatteredCanonicalState(region.nativeState(supportPos).CanonicalJavaID(c.BlockRegistry()))
				if gotSupport != fixture.SupportState {
					t.Errorf("trapdoor support=%s, Source=%s", gotSupport, fixture.SupportState)
				}
			}
			if fixture.InputFluidFinal != "" {
				got := scatteredCanonicalState(region.nativeState(featurePos{fixture.InputFluidPos[0], fixture.InputFluidPos[1], fixture.InputFluidPos[2]}).CanonicalJavaID(c.BlockRegistry()))
				if got != fixture.InputFluidFinal {
					t.Errorf("waterlogging input final=%s, Source=%s", got, fixture.InputFluidFinal)
				}
			}
			sourceRandom, err := strconv.ParseInt(fixture.FinalRandom, 10, 64)
			if err != nil {
				t.Fatalf("parse Source final random value %q: %v", fixture.FinalRandom, err)
			}
			if got := random.NextLong(); got != sourceRandom {
				t.Errorf("postProcess RNG continuation=%d, Source=%d", got, sourceRandom)
			}
			for pos, actor := range region.blockActorData {
				// Bedrock beds have actors; Java beds do not.
				if actor["id"] == "Bed" {
					continue
				}
				if _, exists := fixture.Actors[fmt.Sprintf("%d,%d,%d", pos[0], pos[1], pos[2])]; !exists {
					t.Errorf("unexpected Source actor at %v: %v", pos, actor["id"])
				}
			}
			for actorPos, sourceActor := range fixture.Actors {
				var x, y, z int
				if _, err := fmt.Sscanf(actorPos, "%d,%d,%d", &x, &y, &z); err != nil {
					t.Fatalf("parse source actor position %q: %v", actorPos, err)
				}
				got := region.blockActorData[cube.Pos{x, y, z}]
				if got == nil {
					t.Errorf("missing Source actor at %s", actorPos)
					continue
				}
				sourceID := actorNBTValue(sourceActor, "id")
				nativeID := map[string]string{"minecraft:furnace": "Furnace", "minecraft:chest": "Chest", "minecraft:sign": "Sign", "minecraft:brewing_stand": "BrewingStand"}[sourceID]
				if got["id"] != nativeID {
					t.Errorf("actor at %s id=%v, Source=%s (%s)", actorPos, got["id"], sourceID, nativeID)
				}
				if sourceSeed := actorNBTValue(sourceActor, "LootTableSeed"); sourceSeed != "" {
					want, err := strconv.ParseInt(strings.TrimSuffix(sourceSeed, "L"), 10, 64)
					if err != nil {
						t.Fatalf("parse source actor seed %q: %v", sourceSeed, err)
					}
					if got["LootTableSeed"] != want {
						t.Errorf("actor at %s loot seed=%v, Source=%d", actorPos, got["LootTableSeed"], want)
					}
				}
				sourceLoot := actorNBTValue(sourceActor, "LootTable")
				if sourceLoot != "" && got["LootTable"] != sourceLoot {
					t.Errorf("actor at %s loot table=%v, Source=%s", actorPos, got["LootTable"], sourceLoot)
				}
				if sourceLoot == "" {
					if _, exists := got["LootTable"]; exists {
						t.Errorf("actor at %s unexpectedly has loot table %v", actorPos, got["LootTable"])
					}
				}
				gotText := fmt.Sprint(got)
				for _, token := range []string{"<----", "---->"} {
					if strings.Contains(sourceActor, token) && !strings.Contains(gotText, token) {
						t.Errorf("actor at %s lost Source payload %q", actorPos, token)
					}
				}
				if sourcePotion := actorNBTValue(sourceActor, "potion"); sourcePotion != "" {
					items, ok := got["Items"].([]any)
					if !ok || len(items) != 1 {
						t.Fatalf("Source brewing stand inventory=%v, want one item", got["Items"])
					}
					data, ok := items[0].(map[string]any)
					if !ok {
						t.Fatalf("Source brewing stand item=%T", items[0])
					}
					stack := item.ReadNBT(data, nil, world.DefaultBlockRegistry)
					javaItem, err := item.StackToJavaNBT(stack)
					if err != nil {
						t.Fatal(err)
					}
					components, _ := javaItem["components"].(map[string]any)
					potion, _ := components["minecraft:potion_contents"].(map[string]any)
					if potion["potion"] != sourcePotion || javaItem["id"] != "minecraft:splash_potion" || stack.Count() != 1 || data["Slot"] != byte(1) {
						t.Errorf("Source brewing stand slot1 potion=%v slot=%v, Source=%s", javaItem, data["Slot"], sourcePotion)
					}
				}
			}
		})
	}
}

func actorNBTValue(nbt, key string) string {
	start := strings.LastIndex(nbt, key+":")
	if start < 0 {
		return ""
	}
	value := nbt[start+len(key)+1:]
	end := strings.IndexAny(value, ",}")
	if end >= 0 {
		value = value[:end]
	}
	return strings.Trim(value, "\"")
}
