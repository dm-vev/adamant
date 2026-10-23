package structure

import (
	"encoding/json"
	"maps"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
)

func TestJigsawReplacementMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_jigsaw_replacement.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name, NBTKind, LootTable string
		Chest, Keep, KeepNBT     bool
		Text                     any
		State                    json.RawMessage
		Position                 [3]int32
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			name := "minecraft:jigsaw"
			if tc.Chest {
				name = "minecraft:chest"
			}
			var nbt map[string]any
			if tc.NBTKind != "missing" {
				nbt = map[string]any{"marker": int32(7)}
				switch tc.NBTKind {
				case "string":
					nbt["final_state"] = tc.Text.(string)
				case "int":
					nbt["final_state"] = int32(tc.Text.(float64))
				}
				if tc.Chest {
					nbt["LootTable"] = "minecraft:chests/simple_dungeon"
				}
			}
			piece := JigsawPiece{
				Template: &Template{Size: [3]int32{4, 4, 4}, Palettes: [][]Block{{{
					Pos:   [3]int32{1, 2, 3},
					State: world.BlockState{Name: name, Properties: gamedata.BlockPropertiesWithDefaults(name, nil)},
					NBT:   nbt,
				}}}},
				Origin: [3]int32{16, 80, 16}, Rotation: Clockwise90,
			}
			placed, err := piece.PlaceWithEnvironment(0, Bounds{Min: [3]int32{-100, -64, -100}, Max: [3]int32{100, 320, 100}}, 0, ProcessorEnvironment{})
			if err != nil {
				t.Fatal(err)
			}
			if !tc.Keep {
				if len(placed.Blocks) != 0 {
					t.Fatalf("source drops invalid/void record; got %+v", placed.Blocks)
				}
				return
			}
			if len(placed.Blocks) != 1 {
				t.Fatalf("source retains one record; got %+v", placed.Blocks)
			}
			got, want := placed.Blocks[0], oracleRuleState(t, tc.State)
			if got.Pos != tc.Position || got.State.Name != want.Name || !maps.Equal(got.State.Properties, want.Properties) {
				t.Fatalf("got position=%v state=%+v; source position=%v state=%+v", got.Pos, got.State, tc.Position, want)
			}
			if tc.KeepNBT {
				if !maps.Equal(got.NBT, nbt) || got.NBT["LootTable"] != tc.LootTable {
					t.Fatalf("unchanged non-jigsaw lost source NBT: %+v", got.NBT)
				}
			} else if got.NBT != nil {
				t.Fatalf("replacement retains discarded source NBT: %+v", got.NBT)
			}
		})
	}
}
