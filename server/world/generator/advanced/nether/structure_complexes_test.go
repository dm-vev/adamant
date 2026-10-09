package nether

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() {
	world.DefaultBlockRegistry.Finalize()
}

func TestComplexStartsMatchJava(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_complex_starts.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := compareNetherStartOracle(data); err != nil {
		t.Fatal(err)
	}
}

func TestFortressActorsSurviveRepeatedRendering(t *testing.T) {
	g := NewNether(1)
	// Java start (-14,-54): two MonsterThrones and one chest-bearing corridor.
	expected := []struct {
		pos  cube.Pos
		name string
	}{
		{cube.Pos{-160, 56, -853}, "minecraft:spawner"},
		{cube.Pos{-193, 74, -780}, "minecraft:spawner"},
		{cube.Pos{-207, 65, -796}, "minecraft:chest"},
	}
	first := make([]map[string]any, len(expected))
	for pass := 0; pass < 2; pass++ {
		for n := range expected {
			index := n
			if pass != 0 {
				index = len(expected) - 1 - n
			}
			want := expected[index]
			pos := world.ChunkPos{int32(want.pos[0] >> 4), int32(want.pos[2] >> 4)}
			random := g.featureSource.structureStepRandom(pos, 1, 7)
			var placement javastructure.Placement
			g.placeFortressStructureStep(pos, javastructure.ProcessorEnvironment{
				BaseBlock: func(int32, int32, int32) world.Block { return block.Air{} },
				Random:    random,
			}, func(p javastructure.Placement) { placement = p })
			var data map[string]any
			blockName := ""
			for _, placed := range placement.Blocks {
				if placed.Pos == [3]int32{int32(want.pos[0]), int32(want.pos[1]), int32(want.pos[2])} {
					data = placed.NBT
					blockName = placed.State.Name
				}
			}
			if blockName != want.name {
				t.Fatalf("pass=%d position=%v placement block=%s, want %s", pass, want.pos, blockName, want.name)
			}
			if want.name == "minecraft:chest" {
				if data["LootTable"] != "minecraft:chests/nether_bridge" {
					t.Fatalf("pass=%d missing fortress chest loot: %v", pass, data)
				}
				if seed, ok := bastionRenderInt64(data["LootTableSeed"]); !ok || seed != -8055585923370692160 {
					t.Fatalf("pass=%d fortress chest loot seed=%v, want Java -8055585923370692160", pass, data["LootTableSeed"])
				}
			} else {
				spawn, _ := data["SpawnData"].(map[string]any)
				entity, _ := spawn["entity"].(map[string]any)
				if entity["id"] != "minecraft:blaze" {
					t.Fatalf("pass=%d missing blaze spawner: %v", pass, data)
				}
			}
			if pass == 0 {
				first[index] = data
			} else if !reflect.DeepEqual(data, first[index]) {
				t.Fatalf("re-rendering changed actor at %v: first=%v next=%v", want.pos, first[index], data)
			}
		}
	}
}

func TestFortressPlacementRecordsIntentionalAirAndReadsOverlay(t *testing.T) {
	g := NewNether(1)
	pos := world.ChunkPos{-14, -54}
	render := func(base func(int32, int32, int32) world.Block) javastructure.Placement {
		var placement javastructure.Placement
		g.placeFortressStructureStep(pos, javastructure.ProcessorEnvironment{
			BaseBlock: base,
			Random:    g.featureSource.structureStepRandom(pos, 1, 7),
		}, func(p javastructure.Placement) { placement = p })
		return placement
	}

	allAir := render(func(int32, int32, int32) world.Block { return block.Air{} })
	identicalAirWrite, foundationBelowOverlay := false, false
	for _, placed := range allAir.Blocks {
		if placed.State.Name == "minecraft:air" {
			identicalAirWrite = true
		}
		if placed.State.Name == "minecraft:nether_bricks" && placed.Pos[1] < 16 {
			foundationBelowOverlay = true
		}
	}
	if !identicalAirWrite {
		t.Fatal("fortress placement omitted intentional air writes over air")
	}
	if !foundationBelowOverlay {
		t.Fatal("all-air overlay did not allow fortress foundation fill-down writes")
	}

	blocked := render(func(_, y, _ int32) world.Block {
		if y == 16 {
			return block.Netherrack{}
		}
		return block.Air{}
	})
	for _, placed := range blocked.Blocks {
		if placed.Pos[1] < 16 {
			t.Fatalf("fortress wrote below earlier-feature overlay at %v: %s", placed.Pos, placed.State.Name)
		}
	}
}

func TestFortressFenceStatesFollowPieceOrientation(t *testing.T) {
	g := NewNether(1)
	tests := []struct {
		facing cube.Direction
		masks  [16]uint8
	}{
		{cube.North, [16]uint8{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}},
		{cube.South, [16]uint8{0, 2, 1, 3, 4, 6, 5, 7, 8, 10, 9, 11, 12, 14, 13, 15}},
		{cube.East, [16]uint8{0, 8, 4, 12, 1, 9, 5, 13, 2, 10, 6, 14, 3, 11, 7, 15}},
		{cube.West, [16]uint8{0, 4, 8, 12, 1, 5, 9, 13, 2, 6, 10, 14, 3, 7, 11, 15}},
	}
	for _, tc := range tests {
		piece := &bridgePieceBase{facing: tc.facing}
		for mask, want := range tc.masks {
			if got := piece.netherBrickFenceRID(g, uint8(mask)); got != g.netherBrickFenceRIDs[want] {
				t.Errorf("facing=%s local mask=%04b: native RID=%d, want transformed mask %04b RID=%d", tc.facing, mask, got, want, g.netherBrickFenceRIDs[want])
			}
		}
	}
}

func TestFortressRenderedOverlayMatchesJava263(t *testing.T) {
	// Executed StructureStart.placeInChunk, seed 1/start (-14,-54), decoration
	// step 7/index 1 using WorldgenRandom(XoroshiroRandomSource). Source digests
	// include complete state properties, not just block names.
	for _, tc := range []struct {
		solid       bool
		digest      string
		pos         world.ChunkPos
		post, ticks int
	}{
		{false, "b60ecfb5d6b5759ee50321fcd8c0d4aa285b664d95d0f7a31a0c5428a40606b3", world.ChunkPos{-13, -50}, 24, 0},
		{true, "5cda335dc622e952017dcfdab27bbf87c03875cfb099682383a1c55c0c18c341", world.ChunkPos{-13, -50}, 24, 0},
		// Corner fences must not receive the extra rail writes at i=11.
		{false, "ab847964ec5c7a48ffae1dd669788b248c4bc3fb88e7cdcf540829b31c1810e1", world.ChunkPos{-13, -51}, 64, 1},
	} {
		t.Run(fmt.Sprintf("%d_%d_solid_y16_%t", tc.pos[0], tc.pos[1], tc.solid), func(t *testing.T) {
			g := NewNether(1)
			pos := tc.pos
			c := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
			base := func(_, y, _ int32) world.Block {
				if tc.solid && y == 16 {
					return block.Netherrack{}
				}
				return block.Air{}
			}
			if tc.solid {
				for x := uint8(0); x < 16; x++ {
					for z := uint8(0); z < 16; z++ {
						c.SetBlock(x, 16, z, 0, g.netherrackRID)
					}
				}
			}
			var placement javastructure.Placement
			g.placeFortressStructureStep(pos, javastructure.ProcessorEnvironment{
				BaseBlock: base, Random: g.featureSource.structureStepRandom(pos, 1, 7),
			}, func(p javastructure.Placement) { placement = p })
			for _, entry := range placement.Blocks {
				state, err := world.ResolveState(c.BlockRegistry(), entry.State)
				if err != nil {
					t.Fatal(err)
				}
				world.SetNativeState(c, uint8(entry.Pos[0]&15), int16(entry.Pos[1]), uint8(entry.Pos[2]&15), state)
			}
			if got := netherSourceStateHash(t, c); got != tc.digest {
				t.Errorf("Java fortress state digest=%s, native=%s", tc.digest, got)
			}
			if len(placement.PostProcessing) != tc.post || len(placement.ScheduledTicks) != tc.ticks {
				t.Errorf("source postprocessing=%d ticks=%d; native postprocessing=%d ticks=%v",
					tc.post, tc.ticks, len(placement.PostProcessing), placement.ScheduledTicks)
			}
		})
	}
}
