package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func TestMansionChestMarkersMatchReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_mansion_markers.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Seed             string   `json:"seed"`
		Chunk            [2]int32 `json:"chunk"`
		MarkerRandomSeed string   `json:"markerRandomSeed"`
		Markers          []struct {
			Piece     int                `json:"piece"`
			Name      string             `json:"name"`
			Rotation  structure.Rotation `json:"rotation"`
			Mirror    structure.Mirror   `json:"mirror"`
			Marker    string             `json:"marker"`
			Position  [3]int32           `json:"position"`
			Facing    string             `json:"facing"`
			LootTable string             `json:"lootTable"`
			LootSeed  string             `json:"lootSeed"`
		} `json:"markers"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		markerSeed, err := strconv.ParseInt(fixture.MarkerRandomSeed, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		random := largeFeatureRandom(seed, fixture.Chunk[0], fixture.Chunk[1])
		rotation := structure.Rotation(random.NextInt(4))
		plan := newMansionPlan(&random)
		placer := newMansionPlacer([3]int32{-13, 70, 19}, rotation, seed, &random)
		if err = placer.create(plan); err != nil {
			t.Fatalf("seed %s: %v", fixture.Seed, err)
		}
		type markerResult struct {
			piece         int
			name, marker  string
			rotation      structure.Rotation
			mirror        structure.Mirror
			position      [3]int32
			facing, table string
			seed          int64
			flags         int
			knownShape    bool
		}
		var got []markerResult
		var current markerResult
		markerRandom := vanilla.NewLegacyRandom(markerSeed)
		levelRandom := vanilla.NewLegacyRandom(markerSeed ^ 0x263)
		env := structure.ProcessorEnvironment{
			BaseBlock:           func(int32, int32, int32) world.Block { return block.Air{} },
			Random:              &markerRandom,
			LevelRandom:         &levelRandom,
			GenerationContext:   world.GenerationContext{},
			HasChestBlockEntity: func(int32, int32, int32) bool { return true },
			SetBlockEntityLootTable: func(x, y, z int32, table string, lootSeed int64) {
				if current.position != [3]int32{x, y, z} {
					t.Errorf("loot callback at %v while handling marker at %v", [3]int32{x, y, z}, current.position)
				}
				current.table, current.seed = table, lootSeed
				got = append(got, current)
			},
		}
		clip := structure.Bounds{Min: [3]int32{-1000, -64, -1000}, Max: [3]int32{1000, 320, 1000}}
		for i, piece := range placer.pieces {
			mansion := piece.(modernMansionPiece)
			name := ""
			for n, template := range placer.templates {
				if template == mansion.template {
					name = n
					break
				}
			}
			markersAt := make(map[[3]int32]markerResult)
			for _, block := range mansion.template.Palettes[0] {
				if block.State.Name != "minecraft:structure_block" || block.NBT == nil || block.NBT["mode"] != "DATA" {
					continue
				}
				marker, _ := block.NBT["metadata"].(string)
				if len(marker) < 5 || marker[:5] != "Chest" {
					continue
				}
				pos := structure.TransformPosition(block.Pos, mansion.mirror, mansion.rotation)
				for axis := range 3 {
					pos[axis] += mansion.origin[axis]
				}
				markersAt[pos] = markerResult{piece: i, name: name, marker: marker, rotation: mansion.rotation, mirror: mansion.mirror, position: pos}
			}
			if len(markersAt) == 0 {
				continue
			}
			blocks, err := mansion.Place(clip, structure.ProcessorEnvironment{})
			if err != nil {
				t.Fatalf("seed %s piece %d place: %v", fixture.Seed, i, err)
			}
			for _, block := range blocks.Blocks {
				if block.State.Name == "minecraft:structure_block" {
					t.Fatalf("seed %s piece %d left a structure marker in template blocks", fixture.Seed, i)
				}
			}
			if err = mansion.AfterTemplatePlacement(clip, env, func(out structure.Placement) {
				for _, block := range out.PostShapeBlocks {
					if block.State.Name != "minecraft:chest" {
						continue
					}
					marker, ok := markersAt[block.Pos]
					if !ok {
						t.Errorf("seed %s unexpected chest output at %v", fixture.Seed, block.Pos)
						continue
					}
					if facing, ok := block.State.Properties["facing"].(string); ok {
						marker.facing = facing
					}
					marker.flags, marker.knownShape = out.TemplateUpdateFlags, out.TemplateKnownShape
					current = marker
				}
			}); err != nil {
				t.Fatalf("seed %s piece %d: %v", fixture.Seed, i, err)
			}
		}
		if len(got) != len(fixture.Markers) {
			t.Fatalf("seed %s chest markers=%d, Source=%d", fixture.Seed, len(got), len(fixture.Markers))
		}
		for i, want := range fixture.Markers {
			gotMarker := got[i]
			wantSeed, err := strconv.ParseInt(want.LootSeed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if gotMarker.piece != want.Piece || gotMarker.name != want.Name || gotMarker.rotation != want.Rotation || gotMarker.mirror != want.Mirror || gotMarker.marker != want.Marker || gotMarker.position != want.Position || gotMarker.facing != want.Facing || gotMarker.table != want.LootTable || gotMarker.seed != wantSeed || gotMarker.flags != 2 || !gotMarker.knownShape {
				t.Errorf("seed %s marker %d=%+v, Source piece=%d %s %s at %v facing=%s table=%s seed=%d", fixture.Seed, i, gotMarker, want.Piece, want.Name, want.Marker, want.Position, want.Facing, want.LootTable, wantSeed)
			}
		}
		for _, want := range fixture.Markers {
			mansion := placer.pieces[want.Piece].(modernMansionPiece)
			point := structure.Bounds{Min: want.Position, Max: want.Position}
			for range 2 {
				splitRandom := vanilla.NewLegacyRandom(markerSeed)
				splitLevelRandom := vanilla.NewLegacyRandom(markerSeed ^ 0x263)
				splitEnv := env
				splitEnv.Random, splitEnv.LevelRandom = &splitRandom, &splitLevelRandom
				splitEnv.SetBlockEntityLootTable = func(int32, int32, int32, string, int64) {}
				var splitBlocks []structure.Block
				if err = mansion.AfterTemplatePlacement(point, splitEnv, func(out structure.Placement) {
					for _, block := range out.PostShapeBlocks {
						if block.State.Name == "minecraft:chest" {
							splitBlocks = append(splitBlocks, block)
						}
					}
				}); err != nil {
					t.Fatalf("seed %s split marker at %v: %v", fixture.Seed, want.Position, err)
				}
				if len(splitBlocks) != 1 || splitBlocks[0].Pos != want.Position || splitBlocks[0].State.Properties["facing"] != want.Facing {
					t.Errorf("seed %s split/reload marker at %v got %v, Source facing=%s", fixture.Seed, want.Position, splitBlocks, want.Facing)
				}
			}
		}
	}
}
