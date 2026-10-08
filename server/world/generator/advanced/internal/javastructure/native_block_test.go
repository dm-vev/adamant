package javastructure

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestBannerPatternsSurviveStateRotation(t *testing.T) {
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	source := Block{State: world.BlockState{Name: "minecraft:blue_wall_banner", Properties: map[string]any{"facing": "north"}}, NBT: map[string]any{
		"id":       "minecraft:banner",
		"patterns": []map[string]any{{"pattern": "minecraft:stripe_top", "color": "red"}, {"pattern": "minecraft:border", "color": "black"}},
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
			placed, err := template.Place(0, [3]int32{}, None, NoMirror, Bounds{Min: [3]int32{-1, -1, -1}, Max: [3]int32{1, 1, 1}}, 1, Processor{Process: func(_ ProcessContext, b Block) (Block, bool, error) { b.State = tc.to; return b, true, nil }})
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

func TestCalibratedSensorRotationPreservesPendingVibration(t *testing.T) {
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	listener := world.VibrationData{
		HasCurrent: true, TravelTime: 3,
		Current: world.VibrationInfo{Event: world.GameEventEquip, Distance: 4.25, Position: mgl64.Vec3{4.25, 64.5, 8.5}},
	}
	source := Block{
		State: world.BlockState{Name: "minecraft:calibrated_sculk_sensor", Properties: map[string]any{
			"facing": "north", "sculk_sensor_phase": "active", "power": int32(11), "waterlogged": true,
		}},
		NBT: map[string]any{
			"id": "minecraft:calibrated_sculk_sensor", "last_vibration_frequency": int32(7), "listener": listener.EncodeNBT(),
		},
	}
	template := Template{Size: [3]int32{1, 1, 1}, Palettes: [][]Block{{source}}}
	placed, err := template.Place(0, [3]int32{8, 64, 8}, Clockwise90, NoMirror, Bounds{Min: [3]int32{0, 0, 0}, Max: [3]int32{15, 100, 15}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	state, err := placed.Blocks[0].ResolveNative(registry)
	if err != nil {
		t.Fatal(err)
	}
	water, ok := world.WaterSourceRuntimeID(registry)
	if !ok || state.Liquid != water {
		t.Fatal("state-derived sensor lost its water layer")
	}
	base, _ := registry.BlockByRuntimeID(state.Block)
	restored := base.(world.NBTer).DecodeNBT(placed.BlockEntityPayloads()[0].Data).(block.CalibratedSculkSensor)
	pos := cube.Pos{8, 64, 8}
	if restored.Facing != cube.East || restored.RedstonePower(pos, nil, cube.FaceNorth) != 11 || restored.ComparatorOutput(nil, pos) != 7 {
		t.Fatalf("rotated active sensor lost its signal outputs: facing=%v power=%d comparator=%d", restored.Facing, restored.JavaPower(), restored.ComparatorOutput(nil, pos))
	}
	if !restored.VibrationData().HasCurrent {
		t.Fatal("state overlay erased the saved travelling vibration")
	}
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				tx.Block(cube.Pos{x * 16, 64, z * 16})
			}
		}
		tx.SetBlock(pos, restored, nil)
	})
	for range 3 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		sensor := tx.Block(pos).(block.CalibratedSculkSensor)
		if sensor.VibrationData().HasCurrent {
			t.Fatal("restored vibration did not finish travelling")
		}
		if sensor.JavaPower() != 11 || sensor.ComparatorOutput(tx, pos) != 7 {
			t.Fatal("arrival during the active phase incorrectly reactivated the sensor")
		}
	})
}

func TestTypedTemplateInventoryRetainsSlots(t *testing.T) {
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	source := Block{
		State: world.BlockState{Name: "minecraft:furnace", Properties: map[string]any{"facing": "north", "lit": "false"}},
		NBT: map[string]any{"id": "minecraft:furnace", "Items": []map[string]any{
			{"Slot": int8(1), "id": "minecraft:coal", "count": int32(2)},
			{"id": "minecraft:iron_ore", "count": int32(4)},
		}},
	}
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
	furnace := base.(world.NBTer).DecodeNBT(placed.BlockEntityPayloads()[0].Data).(block.Furnace)
	for slot, want := range []struct {
		name  string
		count int
	}{{"minecraft:iron_ore", 4}, {"minecraft:coal", 2}} {
		stack, err := furnace.Inventory(nil, cube.Pos{}).Item(slot)
		if err != nil {
			t.Fatal(err)
		}
		if stack.Empty() {
			t.Fatalf("source inventory lost slot %d", slot)
		}
		name, _ := stack.Item().EncodeItem()
		if name != want.name || stack.Count() != want.count {
			t.Fatalf("slot %d = %s x%d, want %s x%d", slot, name, stack.Count(), want.name, want.count)
		}
	}
}

func TestBookshelfSourceSlotPrecedence(t *testing.T) {
	state := world.BlockState{Name: "minecraft:chiseled_bookshelf", Properties: map[string]any{"facing": "north"}}
	payload, err := JavaBlockEntityPayload(state, [3]int32{}, map[string]any{
		"id": "minecraft:chiseled_bookshelf",
		"Items": []map[string]any{
			{"Slot": int8(0), "id": "minecraft:book", "count": int32(1)},
			{"id": "minecraft:writable_book", "count": int32(1)},
			{"Slot": int8(6), "id": "minecraft:book", "count": int32(1)},
			{"Slot": int8(-1), "id": "minecraft:book", "count": int32(1)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	shelf := block.NewChiseledBookshelf().DecodeNBT(payload).(block.ChiseledBookshelf)
	if shelf.Items[0].Empty() {
		t.Fatal("default source slot zero lost its book")
	}
	name, _ := shelf.Items[0].Item().EncodeItem()
	if name != "minecraft:writable_book" {
		t.Fatalf("last item in source slot zero lost precedence: %s", name)
	}
	for slot := 1; slot < len(shelf.Items); slot++ {
		if !shelf.Items[slot].Empty() {
			t.Fatalf("unexpected book in slot %d", slot)
		}
	}
}
