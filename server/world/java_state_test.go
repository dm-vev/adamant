package world

import "testing"

func TestJavaTorchAttachmentDirections(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, family := range []struct{ standing, wall, native string }{
		{"torch", "wall_torch", "torch"},
		{"soul_torch", "soul_wall_torch", "soul_torch"},
		{"redstone_torch", "redstone_wall_torch", "redstone_torch"},
		{"copper_torch", "copper_wall_torch", "copper_torch"},
	} {
		t.Run(family.standing, func(t *testing.T) {
			for _, attachment := range []struct{ facing, native string }{
				{"", "top"}, {"north", "south"}, {"south", "north"}, {"west", "east"}, {"east", "west"},
			} {
				name := family.standing
				var props map[string]any
				if attachment.facing != "" {
					name = family.wall
					props = map[string]any{"facing": attachment.facing}
				}
				source := BlockState{Name: "minecraft:" + name, Properties: props}
				state, err := ResolveState(registry, source)
				if err != nil {
					t.Fatal(err)
				}
				nativeName, nativeProps, _ := registry.RuntimeIDToState(state.Block)
				if nativeName != "minecraft:"+family.native || nativeProps["torch_facing_direction"] != attachment.native {
					t.Fatalf("%+v: native attachment = %s %v, want %s", source, nativeName, nativeProps, attachment.native)
				}
				back, err := JavaState(registry, state)
				if err != nil {
					t.Fatal(err)
				}
				if back.Name != source.Name || back.Properties["facing"] != props["facing"] {
					t.Fatalf("native attachment %s: Java state = %+v, want %+v", attachment.native, back, source)
				}
			}
		})
	}
}

func TestJavaDefaultsPreserveExplicitState(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	properties := map[string]any{"facing": "west"}
	state, err := ResolveState(registry, BlockState{Name: "minecraft:leaf_litter", Properties: properties})
	if err != nil {
		t.Fatal(err)
	}
	name, native, _ := registry.RuntimeIDToState(state.Block)
	if name != "minecraft:leaf_litter" || native["growth"] != int32(0) || native["minecraft:cardinal_direction"] != "west" {
		t.Fatalf("default amount and explicit facing were not retained: %s %v", name, native)
	}
	if len(properties) != 1 || properties["facing"] != "west" {
		t.Fatalf("resolving defaults mutated input: %v", properties)
	}
}

func TestJavaLeafLitterNativeSegmentCounts(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	// BDS 1.26.52.3 build 51798919 drops growth+1 items for all eight
	// native growth values. Java permits only one through four segments.
	for _, facing := range []string{"north", "south", "west", "east"} {
		for growth := range int32(8) {
			rid, ok := registry.StateToRuntimeID("minecraft:leaf_litter", map[string]any{
				"growth": growth, "minecraft:cardinal_direction": facing,
			})
			if !ok {
				t.Fatalf("native leaf litter state missing: growth=%d facing=%s", growth, facing)
			}
			native := NativeState{Block: rid, Liquid: registry.AirRuntimeID()}
			source, err := JavaState(registry, native)
			if growth > 3 {
				if err == nil {
					t.Fatalf("native %d-segment litter accepted as Java state: %+v", growth+1, source)
				}
				continue
			}
			if err != nil {
				t.Fatal(err)
			}
			if source.Name != "minecraft:leaf_litter" || source.Properties["segment_amount"] != growth+1 || source.Properties["facing"] != facing {
				t.Fatalf("native growth=%d facing=%s decoded to %+v", growth, facing, source)
			}
			again, err := ResolveState(registry, source)
			if err != nil || again != native {
				t.Fatalf("leaf litter amount/facing lost: source=%+v native=%+v want=%+v error=%v", source, again, native, err)
			}
		}
	}
}

func TestJavaDoorOpenIndependentOfPower(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, name := range []string{"oak_door", "oak_trapdoor", "oak_fence_gate"} {
		for _, open := range []bool{false, true} {
			for _, powered := range []bool{false, true} {
				state, err := ResolveState(registry, BlockState{Name: "minecraft:" + name, Properties: map[string]any{"open": open, "powered": powered}})
				if err != nil {
					t.Fatal(err)
				}
				_, native, _ := registry.RuntimeIDToState(state.Block)
				if got, _ := boolProperty(native["open_bit"]); got != open {
					t.Fatalf("%s open=%v powered=%v encoded %v", name, open, powered, native)
				}
			}
		}
	}
}

func TestJavaCaveVineTipAgeRoundTrip(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, berries := range []bool{false, true} {
		state, err := ResolveState(registry, BlockState{Name: "minecraft:cave_vines", Properties: map[string]any{"age": int32(25), "berries": berries}})
		if err != nil {
			t.Fatal(err)
		}
		_, native, _ := registry.RuntimeIDToState(state.Block)
		if native["growing_plant_age"] != int32(25) {
			t.Fatalf("tip age lost: %v", native)
		}
		back, err := JavaState(registry, state)
		if err != nil {
			t.Fatal(err)
		}
		if back.Name != "minecraft:cave_vines" || back.Properties["age"] != int32(25) || back.Properties["berries"] != berries {
			t.Fatalf("tip state lost on reverse conversion: %+v", back)
		}
	}
}

func TestJavaFreeFluidSecondaryLayer(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, name := range []string{"minecraft:water", "minecraft:lava"} {
		fluid, ok := registry.StateToRuntimeID(name, map[string]any{"liquid_depth": int32(4)})
		if !ok {
			t.Fatalf("native fluid missing: %s", name)
		}
		state, err := JavaState(registry, NativeState{Block: registry.AirRuntimeID(), Liquid: fluid})
		if err != nil {
			t.Fatal(err)
		}
		if state.Name != name || state.Properties["level"] != int32(4) {
			t.Fatalf("secondary free fluid decoded as %+v", state)
		}
	}
}

func TestJavaWallHeadIdentityRoundTrip(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, name := range []string{"skeleton_wall_skull", "wither_skeleton_wall_skull", "zombie_wall_head", "player_wall_head", "creeper_wall_head", "dragon_wall_head", "piglin_wall_head"} {
		for _, facing := range []string{"north", "south", "east", "west"} {
			source := BlockState{Name: "minecraft:" + name, Properties: map[string]any{"facing": facing}}
			native, err := ResolveState(registry, source)
			if err != nil {
				t.Fatal(err)
			}
			back, err := JavaState(registry, native)
			if err != nil {
				t.Fatal(err)
			}
			if back.Name != source.Name || back.Properties["facing"] != facing {
				t.Fatalf("wall head became standing/rotated: %+v -> %+v", source, back)
			}
			if _, hasRotation := back.Properties["rotation"]; hasRotation {
				t.Fatalf("wall head acquired floor rotation: %+v", back)
			}
			again, err := ResolveState(registry, back)
			if err != nil || again != native {
				t.Fatalf("wall head native state changed: %v -> %v, error %v", native, again, err)
			}
		}
	}
}

func TestJavaBubbleColumnDirectionAndWater(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, drag := range []bool{false, true} {
		state, err := ResolveState(registry, BlockState{Name: "minecraft:bubble_column", Properties: map[string]any{"drag": drag}})
		if err != nil {
			t.Fatal(err)
		}
		name, props, _ := registry.RuntimeIDToState(state.Block)
		if name != "minecraft:bubble_column" || nativeBoolValue(props["drag_down"]) != drag {
			t.Fatalf("drag=%v became %s %v", drag, name, props)
		}
		liquid, properties, _ := registry.RuntimeIDToState(state.Liquid)
		if liquid != "minecraft:water" || properties["liquid_depth"] != int32(0) {
			t.Fatalf("bubble column lost source water: %s %v", liquid, properties)
		}
		back, err := JavaState(registry, state)
		if err != nil || back.Name != name || back.Properties["drag"] != drag {
			t.Fatalf("bubble column direction lost: %+v, %v", back, err)
		}
	}
}

func TestJavaWaterloggingUsesSourcePropertyCapability(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, name := range []string{"minecraft:campfire", "minecraft:candle", "minecraft:chest", "minecraft:lantern"} {
		state, err := ResolveState(registry, BlockState{Name: name, Properties: map[string]any{"waterlogged": true}})
		if err != nil {
			t.Fatal(err)
		}
		back, err := JavaState(registry, state)
		if err != nil || back.Name != name || back.Properties["waterlogged"] != true {
			t.Errorf("waterlogged %s roundtrip=%+v error=%v", name, back, err)
		}
	}
	water, err := ResolveState(registry, BlockState{Name: "minecraft:water"})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"minecraft:oak_fence_gate", "minecraft:muddy_mangrove_roots"} {
		state, err := ResolveState(registry, BlockState{Name: name})
		if err != nil {
			t.Fatal(err)
		}
		state.Liquid = water.Block
		if converted, err := JavaState(registry, state); err == nil {
			t.Errorf("native water layer on non-waterloggable Java block %s accepted as %+v", name, converted)
		}
	}
}

func TestJavaSharedNativeBlockNamesKeepMaterialIdentity(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, pair := range []struct{ java, native string }{
		{"stone_stairs", "normal_stone_stairs"},
		{"cobblestone_stairs", "stone_stairs"},
		{"end_stone_brick_stairs", "end_brick_stairs"},
		{"prismarine_brick_stairs", "prismarine_bricks_stairs"},
		{"nether_bricks", "nether_brick"},
		{"red_nether_bricks", "red_nether_brick"},
		{"slime_block", "slime"},
		{"frogspawn", "frog_spawn"},
		{"grass_block", "grass_block"},
		{"poppy", "poppy"},
	} {
		native, err := ResolveState(registry, BlockState{Name: "minecraft:" + pair.java})
		if err != nil {
			t.Fatalf("%s: %v", pair.java, err)
		}
		name, _, _ := registry.RuntimeIDToState(native.Block)
		if name != "minecraft:"+pair.native {
			t.Fatalf("%s became native %s, want %s", pair.java, name, pair.native)
		}
		back, err := JavaState(registry, native)
		if err != nil || back.Name != "minecraft:"+pair.java {
			t.Fatalf("%s material identity lost: %+v, error %v", pair.java, back, err)
		}
	}
}

func TestJavaSeaPickleCountsAndHydration(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, count := range []int32{1, 4} {
		for _, wet := range []bool{false, true} {
			state := BlockState{Name: "minecraft:sea_pickle", Properties: map[string]any{"pickles": count, "waterlogged": wet}}
			native, err := ResolveState(registry, state)
			if err != nil {
				t.Fatal(err)
			}
			_, properties, _ := registry.RuntimeIDToState(native.Block)
			dead, ok := boolProperty(properties["dead_bit"])
			if properties["cluster_count"] != count-1 || !ok || dead == wet {
				t.Fatalf("pickles=%d wet=%v became native %v", count, wet, properties)
			}
			back, err := JavaState(registry, native)
			if err != nil || back.Properties["pickles"] != count || back.Properties["waterlogged"] != wet {
				t.Fatalf("pickle count/hydration lost: %+v, error %v", back, err)
			}
		}
	}
}

func TestJavaCopperDoubleSlabIdentity(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, pair := range []struct{ java, native string }{
		{"cut_copper_slab", "double_cut_copper_slab"},
		{"waxed_weathered_cut_copper_slab", "waxed_weathered_double_cut_copper_slab"},
	} {
		state := BlockState{Name: "minecraft:" + pair.java, Properties: map[string]any{"type": "double", "waterlogged": true}}
		native, err := ResolveState(registry, state)
		if err != nil {
			t.Fatal(err)
		}
		name, _, _ := registry.RuntimeIDToState(native.Block)
		if name != "minecraft:"+pair.native || native.Liquid == registry.AirRuntimeID() {
			t.Fatalf("double slab %s lost material or water: %s %+v", pair.java, name, native)
		}
		back, err := JavaState(registry, native)
		if err != nil || back.Name != state.Name || back.Properties["type"] != "double" || back.Properties["waterlogged"] != true {
			t.Fatalf("double slab became %+v, error %v", back, err)
		}
	}
}

func TestJavaPortalAndEggStateProperties(t *testing.T) {
	registry := NewBlockRegistry()
	registry.Finalize()
	for _, row := range []struct {
		name, key string
		value     any
	}{
		{"nether_portal", "axis", "x"},
		{"nether_portal", "axis", "z"},
		{"end_portal_frame", "eye", false},
		{"end_portal_frame", "eye", true},
		{"sniffer_egg", "hatch", int32(0)},
		{"sniffer_egg", "hatch", int32(2)},
	} {
		state := BlockState{Name: "minecraft:" + row.name, Properties: map[string]any{row.key: row.value}}
		native, err := ResolveState(registry, state)
		if err != nil {
			t.Fatal(err)
		}
		back, err := JavaState(registry, native)
		if err != nil || back.Name != state.Name || back.Properties[row.key] != row.value {
			t.Fatalf("%s property lost: %+v, error %v", row.name, back, err)
		}
	}
}
