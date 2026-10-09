package nether

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type bastionRenderBlockEntity struct {
	pos                  [3]int32
	id                   string
	lootTable            string
	lootTableSeed        int64
	spawnEntityType      string
	spawnPotentialWeight int64
}

func TestBastionRenderedOutputMatchesJava263(t *testing.T) {
	// Digests cover every block in the target chunk, in y,x,z order, using the
	// complete Java state name and property values (including default values).
	// They were captured from Java 26.3 StructureStart.placeInChunk.
	cases := []struct {
		seed          int64
		target        world.ChunkPos
		stateSHA256   string
		blockEntities []bastionRenderBlockEntity
	}{
		{
			seed:        0,
			target:      world.ChunkPos{338, 36},
			stateSHA256: "c4b477db8cbafb4a50f7af2abbb38ebedaba565c5076f8af3a9e753fba7f5002",
			blockEntities: []bastionRenderBlockEntity{
				{pos: [3]int32{5422, 36, 588}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_treasure", lootTableSeed: 5691968962318014801},
				{pos: [3]int32{5422, 39, 582}, id: "minecraft:mob_spawner", spawnEntityType: "minecraft:magma_cube", spawnPotentialWeight: 1},
			},
		},
		{
			seed:        1,
			target:      world.ChunkPos{-36, -46},
			stateSHA256: "b6a000d2f8e8ca16ad578147230d40603ecd734fcd5ecdcfd0ecaf98578ff756",
			blockEntities: []bastionRenderBlockEntity{
				{pos: [3]int32{-566, 36, -731}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: -533874545964007916},
				{pos: [3]int32{-566, 36, -732}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: 4542339041452496857},
				{pos: [3]int32{-569, 73, -722}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: -952884135698806872},
				{pos: [3]int32{-569, 73, -726}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: 5705545606652168659},
				{pos: [3]int32{-569, 73, -727}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: 5069327095364894571},
			},
		},
		{
			seed:        -1,
			target:      world.ChunkPos{-44, 1029},
			stateSHA256: "f9768a6e435b95ea910f62fc955eec03fc45e100485df354812f056e555dbfd1",
			blockEntities: []bastionRenderBlockEntity{
				{pos: [3]int32{-691, 35, 16468}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: -891451509326186090},
				{pos: [3]int32{-693, 72, 16473}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: -7557450268772767799},
				{pos: [3]int32{-696, 72, 16473}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: -8695542598493634793},
				{pos: [3]int32{-697, 72, 16473}, id: "minecraft:chest", lootTable: "minecraft:chests/bastion_other", lootTableSeed: -323584871996924866},
			},
		},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("seed_%d", tc.seed), func(t *testing.T) {
			g := NewNether(tc.seed)
			env := javastructure.ProcessorEnvironment{
				BaseBlock:   g.featureSource.BaseBlock,
				JavaBiomeAt: g.featureSource.JavaBiomeAt,
				BaseHeight:  g.featureSource.BaseHeight,
				Random:      g.featureSource.structureStepRandom(tc.target, 18, 4),
			}
			var placements []javastructure.Placement
			g.featureSource.placeBastionStructureStep(tc.target, env, func(p javastructure.Placement) {
				placements = append(placements, p)
			})

			chunk := g.featureSource.terrainChunk(tc.target).Clone()
			blockEntities := make(map[[3]int32]map[string]any)
			for _, placement := range placements {
				for _, block := range placement.Blocks {
					state, err := world.ResolveState(chunk.BlockRegistry(), block.State)
					if err != nil {
						t.Fatalf("resolve Java state %s at %v: %v", block.State.Name, block.Pos, err)
					}
					world.SetNativeState(chunk, uint8(block.Pos[0]&15), int16(block.Pos[1]), uint8(block.Pos[2]&15), state)
					if _, ok := block.NBT["id"]; ok {
						blockEntities[block.Pos] = block.NBT
					}
				}
			}

			assertBastionRenderBlockEntities(t, blockEntities, tc.blockEntities)

			if got := netherSourceStateHash(t, chunk); got != tc.stateSHA256 {
				t.Errorf("complete target chunk state digest mismatch: Java 26.3 %s, native %s", tc.stateSHA256, got)
			}
		})
	}
}

func assertBastionRenderBlockEntities(t *testing.T, got map[[3]int32]map[string]any, want []bastionRenderBlockEntity) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("placed %d block entities, want %d", len(got), len(want))
	}
	for _, expected := range want {
		actual, ok := got[expected.pos]
		if !ok {
			t.Errorf("missing block entity at %v", expected.pos)
			continue
		}
		if actual["id"] != expected.id {
			t.Errorf("block entity at %v has id %v, want %s", expected.pos, actual["id"], expected.id)
		}
		if expected.lootTable != "" && actual["LootTable"] != expected.lootTable {
			t.Errorf("chest at %v has loot table %v, want %s", expected.pos, actual["LootTable"], expected.lootTable)
		}
		if expected.lootTable != "" {
			seed, ok := bastionRenderInt64(actual["LootTableSeed"])
			if !ok || seed != expected.lootTableSeed {
				t.Errorf("chest at %v has loot seed %v, want %d", expected.pos, actual["LootTableSeed"], expected.lootTableSeed)
			}
		}
		if expected.spawnEntityType != "" {
			spawnData, ok := actual["SpawnData"].(map[string]any)
			entity, entityOK := spawnData["entity"].(map[string]any)
			if !ok || !entityOK || entity["id"] != expected.spawnEntityType {
				t.Errorf("spawner at %v has spawn data %v, want entity %s", expected.pos, actual["SpawnData"], expected.spawnEntityType)
			}
			potential, ok := bastionRenderSingleCompound(actual["SpawnPotentials"])
			if !ok {
				t.Errorf("spawner at %v does not have exactly one source spawn potential", expected.pos)
				continue
			}
			data, ok := potential["data"].(map[string]any)
			entity, entityOK = data["entity"].(map[string]any)
			if !ok || !entityOK || entity["id"] != expected.spawnEntityType {
				t.Errorf("spawner at %v has potential data %v, want entity %s", expected.pos, potential["data"], expected.spawnEntityType)
			}
			weight, ok := bastionRenderInt64(potential["weight"])
			if !ok || weight != expected.spawnPotentialWeight {
				t.Errorf("spawner at %v has potential weight %v, want %d", expected.pos, potential["weight"], expected.spawnPotentialWeight)
			}
		}
	}
}

func bastionRenderInt64(value any) (int64, bool) {
	switch value := value.(type) {
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	default:
		return 0, false
	}
}
func bastionRenderSingleCompound(value any) (map[string]any, bool) {
	switch list := value.(type) {
	case []any:
		if len(list) != 1 {
			return nil, false
		}
		compound, ok := list[0].(map[string]any)
		return compound, ok
	case []map[string]any:
		if len(list) != 1 {
			return nil, false
		}
		return list[0], true
	default:
		return nil, false
	}
}
