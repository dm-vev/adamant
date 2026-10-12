package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type shipwreckConstructorFixture struct {
	Beached          bool     `json:"beached"`
	Seed             int64    `json:"seed"`
	Template         string   `json:"template"`
	Rotation         int      `json:"rotation"`
	Size             [3]int32 `json:"size"`
	TemplatePosition [3]int32 `json:"template_position"`
	RawBox           [6]int32 `json:"raw_box"`
	NextLong         string   `json:"next_long"`
}

func TestShipwreckConstructorMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_shipwreck_constructor.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []shipwreckConstructorFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Template+"/"+strconv.FormatInt(fixture.Seed, 10), func(t *testing.T) {
			structure := "shipwreck"
			biome := "ocean"
			if fixture.Beached {
				structure, biome = "shipwreck_beached", "beach"
			}
			config, err := javadata.LoadJSON("structure", structure)
			if err != nil {
				t.Fatal(err)
			}
			random := java26.NewLegacyRandom(fixture.Seed)
			start, valid, err := buildModernShipwreck(modernStructureBuildInput{
				seed: fixture.Seed, name: "minecraft:" + structure, chunkX: -3, chunkZ: 5,
				config: config, random: &random,
				source: surfaceSourceTerrain{fixture: surfaceSourceCase{Structure: structure, Biome: biome, Scenario: "valid", Height: 67, Chunk: [2]int32{-3, 5}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !valid || len(start.pieces) != 1 {
				t.Fatalf("start valid=%v pieces=%d", valid, len(start.pieces))
			}
			piece := start.pieces[0].(*modernShipwreckPiece)
			wantBox := javastructure.Box{Min: [3]int32{fixture.RawBox[0], fixture.RawBox[1], fixture.RawBox[2]}, Max: [3]int32{fixture.RawBox[3], fixture.RawBox[4], fixture.RawBox[5]}}
			if piece.templateName != strings.TrimPrefix(fixture.Template, "minecraft:") || piece.template.Size != fixture.Size || piece.origin != fixture.TemplatePosition || piece.rotation != javastructure.Rotation(fixture.Rotation) || piece.Bounds() != wantBox || start.box != wantBox {
				t.Fatalf("constructed template size=%v TP=%v rotation=%d pieceBox=%v startBox=%v; Java size=%v TP=%v rotation=%d box=%v", piece.template.Size, piece.origin, piece.rotation, piece.Bounds(), start.box, fixture.Size, fixture.TemplatePosition, fixture.Rotation, wantBox)
			}
			if piece.heightAdjusted || piece.oversized {
				t.Fatalf("ordinary Source constructor adjustment flags: heightAdjusted=%v oversized=%v", piece.heightAdjusted, piece.oversized)
			}
			wantNext, err := strconv.ParseInt(fixture.NextLong, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if got := random.NextLong(); got != wantNext {
				t.Fatalf("generation random after constructor=%d, Java=%d", got, wantNext)
			}
		})
	}
}

type shipwreckNativeTerrain struct {
	modernTerrainSource
	air, stone, water world.NativeState
}

func (shipwreckNativeTerrain) BaseHeight(int32, int32, bool) int32 { return 53 }
func (s shipwreckNativeTerrain) BaseState(_, y, _ int32) world.NativeState {
	if y < 53 {
		return s.stone
	}
	if y < 63 {
		return s.water
	}
	return s.air
}
func (s shipwreckNativeTerrain) BaseBlock(x, y, z int32) world.Block {
	block, _ := world.DefaultBlockRegistry.BlockByRuntimeID(s.BaseState(x, y, z).Block)
	return block
}

func TestShipwreckMarkerLootUsesAppliedNativeActor(t *testing.T) {
	resolve := func(name string, properties map[string]any) world.NativeState {
		state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name, Properties: properties})
		if err != nil {
			t.Fatal(err)
		}
		return state
	}
	terrain := shipwreckNativeTerrain{
		air: resolve("minecraft:air", nil), stone: resolve("minecraft:stone", nil),
		water: resolve("minecraft:water", map[string]any{"level": int32(0)}),
	}
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			for y := int16(-64); y < 53; y++ {
				world.SetNativeState(c, x, y, z, terrain.stone)
			}
			for y := int16(53); y < 63; y++ {
				world.SetNativeState(c, x, y, z, terrain.water)
			}
		}
	}
	result := &world.GenerationResult{}
	features := &ModernFeatures{source: terrain}
	region := newFeatureRegion(features, c, world.ChunkPos{-3, 5}, result)
	region.SetOrigin(-3, 5)
	template, err := javastructure.Load("shipwreck/with_mast")
	if err != nil {
		t.Fatal(err)
	}
	piece := modernShipwreckPiece{
		template: template, templateName: "shipwreck/with_mast", origin: [3]int32{-48, 90, 80},
		box:      javastructure.Box{Min: [3]int32{-48, 90, 80}, Max: [3]int32{-40, 110, 107}},
		rotation: javastructure.Rotation(0),
	}
	state := &javastructure.StructurePieceState{}
	random := java26.NewLegacyRandom(123456789)
	env := javastructure.ProcessorEnvironment{
		BaseHeight: terrain.BaseHeight, Range: c.Range(), Random: &random, PieceState: state,
		HasChestBlockEntity: func(x, y, z int32) bool { return region.hasChestBlockEntity(cube.Pos{int(x), int(y), int(z)}) },
		SetBlockEntityLootTable: func(x, y, z int32, table string, seed int64) {
			region.setBlockEntityLootTable(cube.Pos{int(x), int(y), int(z)}, table, seed)
		},
	}
	clips := [...]javastructure.Bounds{
		{Min: [3]int32{-48, 56, 80}, Max: [3]int32{-44, 56, 107}},
		{Min: [3]int32{-48, -64, 80}, Max: [3]int32{-44, 55, 107}},
		{Min: [3]int32{-48, 56, 80}, Max: [3]int32{-44, 56, 107}},
	}
	for _, clip := range clips {
		placement, err := piece.Place(clip, env)
		if err != nil {
			t.Fatal(err)
		}
		features.applyStructurePlacement(region, placement)
		if err := piece.AfterTemplatePlacement(clip, env, func(post javastructure.Placement) { features.applyStructurePlacement(region, post) }); err != nil {
			t.Fatal(err)
		}
	}
	name, _ := region.State(featurePos{-44, 55, 89})
	if name != "minecraft:chest" {
		t.Fatalf("native target chunk chest state=%s", name)
	}
	for _, actor := range result.BlockEntities {
		if actor.Pos == (cube.Pos{-44, 55, 89}) {
			if actor.Data["LootTable"] != "minecraft:chests/shipwreck_supply" || actor.Data["LootTableSeed"] != int64(8429272609719263920) {
				t.Fatalf("native actor=%+v, want Source supply table and seed", actor)
			}
			if got := random.NextLong(); got != 7203573180470058788 {
				t.Fatalf("postProcess random continuation=%d, Source=7203573180470058788", got)
			}
			return
		}
	}
	t.Fatal("native target chunk did not publish the Source marker chest actor")
}
