package javastructure

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBannerPatternsSurviveStateRotation(t *testing.T) {
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	source := Block{State: world.BlockState{Name: "minecraft:blue_wall_banner", Properties: map[string]any{"facing": "north"}}, NBT: map[string]any{
		"id":       "minecraft:banner",
		"patterns": []any{map[string]any{"pattern": "minecraft:stripe_top", "color": "red"}, map[string]any{"pattern": "minecraft:border", "color": "black"}},
	}}
	template := Template{Size: [3]int32{1, 1, 1}, Palettes: [][]Block{{source}}}
	placed, err := template.Place(0, [3]int32{8, 64, 8}, Clockwise90, NoMirror, Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 100, 15}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	state, err := placed.Blocks[0].ResolveNative(registry)
	if err != nil {
		t.Fatal(err)
	}
	base, _ := registry.BlockByRuntimeID(state.Block)
	restored := base.(world.NBTer).DecodeNBT(placed.BlockEntityPayloads()[0].Data).(block.Banner)
	_, properties := restored.EncodeBlock()
	if restored.Colour.Uint8() != 11 || properties["facing_direction"] != int32(5) {
		t.Fatalf("lost rotated blue wall banner: %#v", restored)
	}
	want := []any{map[string]any{"Pattern": "ts", "Color": int32(1)}, map[string]any{"Pattern": "bo", "Color": int32(0)}}
	if got := restored.EncodeNBT()["Patterns"]; !reflect.DeepEqual(got, want) {
		t.Fatalf("patterns = %#v, want %#v", got, want)
	}
	if _, mutated := source.NBT["Patterns"]; mutated {
		t.Fatal("placement mutated immutable source NBT")
	}
}

func TestProcessorReplacementRefreshesStateActor(t *testing.T) {
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	bed := world.BlockState{Name: "minecraft:red_bed", Properties: map[string]any{"facing": "west", "part": "head", "occupied": "true"}}
	stone := world.BlockState{Name: "minecraft:stone"}
	blue := bed
	blue.Name = "minecraft:blue_bed"
	for _, tc := range []struct {
		name     string
		from, to world.BlockState
		colour   uint8
		actor    bool
	}{
		{"create", stone, bed, 14, true},
		{"recolour", bed, blue, 11, true},
		{"remove", bed, stone, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native, _, err := NativeBlockForJavaState(tc.from)
			if err != nil {
				t.Fatal(err)
			}
			template := Template{Size: [3]int32{1, 1, 1}, Palettes: [][]Block{{{State: tc.from, NativeBlock: native}}}}
			placed, err := template.Place(0, [3]int32{}, None, NoMirror, Bounds{Min: [3]int32{-1, -1, -1}, Max: [3]int32{1, 1, 1}}, 1, func(_ ProcessContext, b Block) (Block, bool, error) { b.State = tc.to; return b, true, nil })
			if err != nil {
				t.Fatal(err)
			}
			state, err := placed.Blocks[0].ResolveNative(registry)
			if err != nil {
				t.Fatal(err)
			}
			base, _ := registry.BlockByRuntimeID(state.Block)
			if !tc.actor {
				if _, ok := base.(block.Stone); !ok || len(placed.BlockEntityPayloads()) != 0 {
					t.Fatalf("replacement retained bed actor: %#v", base)
				}
				return
			}
			restored := base.(world.NBTer).DecodeNBT(placed.BlockEntityPayloads()[0].Data).(block.Bed)
			if restored.Colour.Uint8() != tc.colour || !restored.Head || !restored.Occupied || restored.Facing != cube.West || restored.Sleeper != nil {
				t.Fatalf("bed state lost on replacement: %#v", restored)
			}
		})
	}
}

func TestActorRotationMatchesJavaMirrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    any
		mirror   Mirror
		rotation Rotation
		yaw      float32
	}{
		{"south_left_right", "0", LeftRight, None, 180},
		{"south_front_back", "0", FrontBack, None, 0},
		{"mirror_then_clockwise", "3", LeftRight, Clockwise90, 202.5},
		{"numeric_front_back", int32(3), FrontBack, Clockwise90, 22.5},
		{"clockwise_wrap", "15", NoMirror, Clockwise90, 67.5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			template := Template{Size: [3]int32{1, 1, 1}, Palettes: [][]Block{{{State: world.BlockState{Name: "minecraft:dragon_head", Properties: map[string]any{"rotation": tc.value}}}}}}
			placed, err := template.Place(0, [3]int32{}, tc.rotation, tc.mirror, Bounds{Min: [3]int32{-1, -1, -1}, Max: [3]int32{1, 1, 1}}, 1)
			if err != nil {
				t.Fatal(err)
			}
			if got := placed.BlockEntityPayloads()[0].Data["Rotation"]; got != tc.yaw {
				t.Fatalf("native skull yaw = %v, want %v", got, tc.yaw)
			}
		})
	}
}
