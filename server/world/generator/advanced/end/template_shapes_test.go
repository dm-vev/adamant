package end

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func TestEndCityTemplateUpdatesBoundaryShapes(t *testing.T) {
	g, region := newEndTemplateShapeFixture()
	registry := world.DefaultBlockRegistry
	stone, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:stone"})
	if err != nil {
		t.Fatal(err)
	}
	if !region.setLayers(16, 70, 8, stone) {
		t.Fatal("could not seed existing block across the chunk edge")
	}

	template := &structure.Template{
		Size: [3]int32{1, 1, 1},
		Palettes: [][]structure.Block{{{
			Pos:   [3]int32{0, 0, 0},
			State: world.BlockState{Name: "minecraft:iron_bars"},
		}}},
	}
	piece := &endCityPiece{
		pos:      endCityPos{x: 15, y: 70, z: 8},
		template: &endCityTemplate{Template: template},
	}
	random := vanilla.NewLegacyRandom(0)
	var result world.GenerationResult
	g.placeEndCityPiece(0, 0, region, piece, &random, true, &result)

	bars, err := world.CanonicalState(registry, region.nativeState(15, 70, 8))
	if err != nil {
		t.Fatal(err)
	}
	if connected, ok := bars.Properties["east"].(bool); !ok || !connected {
		t.Fatalf("template boundary bars east connection = %#v, want true after immediate placement shape pass", bars.Properties["east"])
	}
}

func newEndTemplateShapeFixture() (*End, *endFeatureRegion) {
	world.DefaultBlockRegistry.Finalize()
	g := NewEnd(0)
	registry := world.DefaultBlockRegistry
	c := chunk.New(registry, cube.Range{0, 127})
	region := &endFeatureRegion{
		minChunkX: -2, minChunkZ: -2, targetX: 0, targetZ: 0,
		minY: 0, maxY: 127, airRID: g.airRID,
		shapeRandoms:   make(map[[2]int]*vanilla.Random),
		scheduledTicks: make(map[chunk.ScheduledTick]struct{}),
		blockActors:    make(map[cube.Pos]endBlockActor),
		logicalStates:  make(map[cube.Pos]world.NativeState),
	}
	for x := region.minChunkX; x < region.minChunkX+endFeatureRadius; x++ {
		for z := region.minChunkZ; z < region.minChunkZ+endFeatureRadius; z++ {
			region.chunks[region.index(x, z)] = chunk.New(registry, c.Range())
		}
	}
	region.chunks[region.index(0, 0)] = c
	return g, region
}

func TestEndCityChestMarkerPreservesShapedActor(t *testing.T) {
	g, region := newEndTemplateShapeFixture()
	template := &structure.Template{
		Size: [3]int32{1, 2, 1},
		Palettes: [][]structure.Block{{
			{
				Pos:   [3]int32{0, 0, 0},
				State: world.BlockState{Name: "minecraft:chest", Properties: map[string]any{"facing": "east", "type": "left"}},
				NBT:   map[string]any{"id": "minecraft:chest", "LootTable": "minecraft:chests/simple_dungeon"},
			},
			{
				Pos:   [3]int32{0, 1, 0},
				State: world.BlockState{Name: "minecraft:structure_block"},
				NBT:   map[string]any{"id": "minecraft:structure_block", "mode": "DATA", "metadata": "Chest"},
			},
		}},
	}
	piece := &endCityPiece{pos: endCityPos{x: 8, y: 70, z: 8}, template: &endCityTemplate{Template: template}}
	random := vanilla.NewLegacyRandom(0)
	var result world.GenerationResult
	g.placeEndCityPiece(0, 0, region, piece, &random, true, &result)
	state, err := world.CanonicalState(world.DefaultBlockRegistry, region.nativeState(8, 70, 8))
	if err != nil {
		t.Fatal(err)
	}
	if state.Properties["type"] != "single" || state.Properties["facing"] != "east" {
		t.Fatalf("orphan chest shape/facing = %#v", state.Properties)
	}
	if len(result.BlockEntities) != 1 {
		t.Fatalf("published actors = %d, want one chest without its DATA marker", len(result.BlockEntities))
	}
	data := result.BlockEntities[0].Data
	if data["LootTable"] != "minecraft:chests/end_city_treasure" {
		t.Fatalf("post-shape loot marker was lost: %#v", data)
	}
	if _, stale := data["pairx"]; stale {
		t.Fatalf("unpaired chest retained native partner: %#v", data)
	}
	if template.Palettes[0][0].NBT["LootTable"] != "minecraft:chests/simple_dungeon" {
		t.Fatal("placement mutated reusable source template NBT")
	}
}

func TestEndTemplateShapesUseSourceWriteRadius(t *testing.T) {
	_, region := newEndTemplateShapeFixture()
	stone, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:stone"})
	if err != nil {
		t.Fatal(err)
	}
	var result world.GenerationResult
	access := endShapeAccess{
		endFeatureOrigin: endFeatureOrigin{region: region, chunkX: 1, chunkZ: 0},
		result:           &result,
	}
	id := stone.CanonicalID(world.DefaultBlockRegistry)
	if !access.SetBlockState(cube.Pos{32, 70, 8}, id, 18) {
		t.Fatal("source-adjacent write incorrectly clipped to target radius")
	}
	if access.SetBlockState(cube.Pos{-16, 70, 8}, id, 18) {
		t.Fatal("target-adjacent write escaped source radius")
	}
	if got := region.block(-16, 70, 8); got != region.airRID {
		t.Fatalf("rejected source write changed block to %d", got)
	}
}
