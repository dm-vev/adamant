package world

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// CanonicalStateBlock applies source palette properties to a native actor while
// retaining actor-only metadata on the receiver.
type CanonicalStateBlock interface {
	WithCanonicalState(name string, properties map[string]any) (Block, error)
}

var waterSourceProperties = map[string]any{"liquid_depth": int32(0)}
var facingPropertyCandidates = [...]string{"minecraft:cardinal_direction", "facing_direction", "direction", "weirdo_direction", "minecraft:facing_direction", "minecraft:block_face", "torch_facing_direction"}
var rotationPropertyCandidates = [...]string{"ground_sign_direction", "rotation"}
var poweredPropertyCandidates = [...]string{"powered_bit", "rail_data_bit", "output_lit_bit", "button_pressed_bit", "toggle_bit", "open_bit"}
var agePropertyCandidates = [...]string{"age", "growth", "age_bit", "kelp_age", "twisting_vines_age", "weeping_vines_age", "propagule_stage", "growing_plant_age"}
var levelPropertyCandidates = [...]string{"liquid_depth", "depth", "fill_level"}

var javaToNativeBlockNames = map[string]string{
	"minecraft:grass_block":                  "minecraft:grass",
	"minecraft:dead_bush":                    "minecraft:deadbush",
	"minecraft:cobweb":                       "minecraft:web",
	"minecraft:lily_pad":                     "minecraft:waterlily",
	"minecraft:snow":                         "minecraft:snow_layer",
	"minecraft:moving_piston":                "minecraft:moving_block",
	"minecraft:cave_air":                     "minecraft:air",
	"minecraft:void_air":                     "minecraft:air",
	"minecraft:magma_block":                  "minecraft:magma",
	"minecraft:spawner":                      "minecraft:mob_spawner",
	"minecraft:end_stone_bricks":             "minecraft:end_bricks",
	"minecraft:piston_head":                  "minecraft:piston_arm_collision",
	"minecraft:tripwire":                     "minecraft:trip_wire",
	"minecraft:powered_rail":                 "minecraft:golden_rail",
	"minecraft:large_fern":                   "minecraft:double_plant",
	"minecraft:tall_grass":                   "minecraft:double_plant",
	"minecraft:lilac":                        "minecraft:double_plant",
	"minecraft:peony":                        "minecraft:double_plant",
	"minecraft:rose_bush":                    "minecraft:double_plant",
	"minecraft:sunflower":                    "minecraft:double_plant",
	"minecraft:torchflower":                  "minecraft:torchflower",
	"minecraft:melon":                        "minecraft:melon_block",
	"minecraft:dandelion":                    "minecraft:yellow_flower",
	"minecraft:poppy":                        "minecraft:red_flower",
	"minecraft:blue_orchid":                  "minecraft:red_flower",
	"minecraft:allium":                       "minecraft:red_flower",
	"minecraft:azure_bluet":                  "minecraft:red_flower",
	"minecraft:red_tulip":                    "minecraft:red_flower",
	"minecraft:orange_tulip":                 "minecraft:red_flower",
	"minecraft:white_tulip":                  "minecraft:red_flower",
	"minecraft:pink_tulip":                   "minecraft:red_flower",
	"minecraft:oxeye_daisy":                  "minecraft:red_flower",
	"minecraft:cornflower":                   "minecraft:red_flower",
	"minecraft:lily_of_the_valley":           "minecraft:red_flower",
	"minecraft:sugar_cane":                   "minecraft:reeds",
	"minecraft:jack_o_lantern":               "minecraft:lit_pumpkin",
	"minecraft:note_block":                   "minecraft:noteblock",
	"minecraft:wall_torch":                   "minecraft:torch",
	"minecraft:soul_wall_torch":              "minecraft:soul_torch",
	"minecraft:redstone_wall_torch":          "minecraft:redstone_torch",
	"minecraft:copper_wall_torch":            "minecraft:copper_torch",
	"minecraft:dirt_path":                    "minecraft:grass_path",
	"minecraft:terracotta":                   "minecraft:hardened_clay",
	"minecraft:snow_block":                   "minecraft:snow",
	"minecraft:attached_melon_stem":          "minecraft:melon_stem",
	"minecraft:attached_pumpkin_stem":        "minecraft:pumpkin_stem",
	"minecraft:bricks":                       "minecraft:brick_block",
	"minecraft:cobblestone_stairs":           "minecraft:stone_stairs",
	"minecraft:light_gray_glazed_terracotta": "minecraft:silver_glazed_terracotta",
	"minecraft:stone_stairs":                 "minecraft:normal_stone_stairs",
	"minecraft:end_stone_brick_stairs":       "minecraft:end_brick_stairs",
	"minecraft:prismarine_brick_stairs":      "minecraft:prismarine_bricks_stairs",
	"minecraft:nether_bricks":                "minecraft:nether_brick",
	"minecraft:red_nether_bricks":            "minecraft:red_nether_brick",
	"minecraft:nether_portal":                "minecraft:portal",
	"minecraft:frogspawn":                    "minecraft:frog_spawn",
	"minecraft:slime_block":                  "minecraft:slime",
	"minecraft:oak_button":                   "minecraft:wooden_button",
	"minecraft:oak_door":                     "minecraft:wooden_door",
	"minecraft:oak_fence_gate":               "minecraft:fence_gate",
	"minecraft:oak_pressure_plate":           "minecraft:wooden_pressure_plate",
	"minecraft:oak_trapdoor":                 "minecraft:trapdoor",
	"minecraft:stonecutter":                  "minecraft:stonecutter_block",
	"minecraft:nether_quartz_ore":            "minecraft:quartz_ore",
	"minecraft:flowering_azalea_leaves":      "minecraft:azalea_leaves_flowered",
	"minecraft:beetroots":                    "minecraft:beetroot",
	"minecraft:skeleton_wall_skull":          "minecraft:skeleton_skull",
	"minecraft:wither_skeleton_wall_skull":   "minecraft:wither_skeleton_skull",
	"minecraft:zombie_wall_head":             "minecraft:zombie_head",
	"minecraft:player_wall_head":             "minecraft:player_head",
	"minecraft:creeper_wall_head":            "minecraft:creeper_head",
	"minecraft:dragon_wall_head":             "minecraft:dragon_head",
	"minecraft:piglin_wall_head":             "minecraft:piglin_head",
	"minecraft:stone_slab":                   "minecraft:normal_stone_slab",
	"minecraft:twisting_vines_plant":         "minecraft:twisting_vines",
	"minecraft:weeping_vines_plant":          "minecraft:weeping_vines",
	"minecraft:waxed_copper_block":           "minecraft:waxed_copper",
	"minecraft:rooted_dirt":                  "minecraft:dirt_with_roots",
	"minecraft:big_dripleaf_stem":            "minecraft:big_dripleaf",
}

var nativeToJavaBlockNames = map[string]string{
	"minecraft:flowing_water":                "minecraft:water",
	"minecraft:flowing_lava":                 "minecraft:lava",
	"minecraft:melon_block":                  "minecraft:melon",
	"minecraft:magma":                        "minecraft:magma_block",
	"minecraft:mob_spawner":                  "minecraft:spawner",
	"minecraft:end_bricks":                   "minecraft:end_stone_bricks",
	"minecraft:grass":                        "minecraft:grass_block",
	"minecraft:sticky_piston_arm_collision":  "minecraft:piston_head",
	"minecraft:deadbush":                     "minecraft:dead_bush",
	"minecraft:web":                          "minecraft:cobweb",
	"minecraft:waterlily":                    "minecraft:lily_pad",
	"minecraft:snow_layer":                   "minecraft:snow",
	"minecraft:moving_block":                 "minecraft:moving_piston",
	"minecraft:piston_arm_collision":         "minecraft:piston_head",
	"minecraft:trip_wire":                    "minecraft:tripwire",
	"minecraft:yellow_flower":                "minecraft:dandelion",
	"minecraft:small_dripleaf_block":         "minecraft:small_dripleaf",
	"minecraft:reeds":                        "minecraft:sugar_cane",
	"minecraft:lit_pumpkin":                  "minecraft:jack_o_lantern",
	"minecraft:noteblock":                    "minecraft:note_block",
	"minecraft:cave_vines_body_with_berries": "minecraft:cave_vines_plant",
	"minecraft:cave_vines_head_with_berries": "minecraft:cave_vines",
	"minecraft:grass_path":                   "minecraft:dirt_path",
	"minecraft:daylight_detector_inverted":   "minecraft:daylight_detector",
	"minecraft:hardened_clay":                "minecraft:terracotta",
	"minecraft:snow":                         "minecraft:snow_block",
	"minecraft:brick_block":                  "minecraft:bricks",
	"minecraft:stone_stairs":                 "minecraft:cobblestone_stairs",
	"minecraft:silver_glazed_terracotta":     "minecraft:light_gray_glazed_terracotta",
	"minecraft:normal_stone_stairs":          "minecraft:stone_stairs",
	"minecraft:end_brick_stairs":             "minecraft:end_stone_brick_stairs",
	"minecraft:prismarine_bricks_stairs":     "minecraft:prismarine_brick_stairs",
	"minecraft:nether_brick":                 "minecraft:nether_bricks",
	"minecraft:red_nether_brick":             "minecraft:red_nether_bricks",
	"minecraft:portal":                       "minecraft:nether_portal",
	"minecraft:frog_spawn":                   "minecraft:frogspawn",
	"minecraft:slime":                        "minecraft:slime_block",
	"minecraft:wooden_button":                "minecraft:oak_button",
	"minecraft:wooden_door":                  "minecraft:oak_door",
	"minecraft:fence_gate":                   "minecraft:oak_fence_gate",
	"minecraft:wooden_pressure_plate":        "minecraft:oak_pressure_plate",
	"minecraft:trapdoor":                     "minecraft:oak_trapdoor",
	"minecraft:stonecutter_block":            "minecraft:stonecutter",
	"minecraft:quartz_ore":                   "minecraft:nether_quartz_ore",
	"minecraft:azalea_leaves_flowered":       "minecraft:flowering_azalea_leaves",
	"minecraft:beetroot":                     "minecraft:beetroots",
	"minecraft:normal_stone_slab":            "minecraft:stone_slab",
	"minecraft:twisting_vines":               "minecraft:twisting_vines_plant",
	"minecraft:weeping_vines":                "minecraft:weeping_vines_plant",
	"minecraft:waxed_copper":                 "minecraft:waxed_copper_block",
	"minecraft:dirt_with_roots":              "minecraft:rooted_dirt",
}
var redFlowerNativeTypes = map[string]string{
	"minecraft:poppy":              "poppy",
	"minecraft:blue_orchid":        "orchid",
	"minecraft:allium":             "allium",
	"minecraft:azure_bluet":        "houstonia",
	"minecraft:red_tulip":          "red_tulip",
	"minecraft:orange_tulip":       "orange_tulip",
	"minecraft:white_tulip":        "white_tulip",
	"minecraft:pink_tulip":         "pink_tulip",
	"minecraft:oxeye_daisy":        "oxeye",
	"minecraft:cornflower":         "cornflower",
	"minecraft:lily_of_the_valley": "lily_of_the_valley",
}

var nativeRedFlowerJavaTypes = map[string]string{
	"poppy": "minecraft:poppy", "orchid": "minecraft:blue_orchid", "allium": "minecraft:allium",
	"houstonia": "minecraft:azure_bluet", "red_tulip": "minecraft:red_tulip", "orange_tulip": "minecraft:orange_tulip",
	"white_tulip": "minecraft:white_tulip", "pink_tulip": "minecraft:pink_tulip", "oxeye": "minecraft:oxeye_daisy",
	"cornflower": "minecraft:cornflower", "lily_of_the_valley": "minecraft:lily_of_the_valley",
}

var doublePlantNativeTypes = map[string]string{
	"minecraft:sunflower": "sunflower", "minecraft:lilac": "syringa", "minecraft:rose_bush": "rose", "minecraft:peony": "paeonia",
	"minecraft:tall_grass": "grass", "minecraft:large_fern": "fern",
}

var nativeDoublePlantJavaTypes = map[string]string{
	"sunflower": "minecraft:sunflower", "syringa": "minecraft:lilac", "rose": "minecraft:rose_bush", "paeonia": "minecraft:peony",
	"grass": "minecraft:tall_grass", "fern": "minecraft:large_fern",
}

// ActorBlockName identifies the source families whose palette properties
// require a native block actor. Only dye-coloured beds use the generic bed palette.
func ActorBlockName(name string) string {
	if name == "minecraft:shulker_box" {
		return "minecraft:undyed_shulker_box"
	}
	var colour, native string
	switch {
	case strings.HasSuffix(name, "_wall_banner"):
		colour, native = strings.TrimSuffix(name, "_wall_banner"), "minecraft:wall_banner"
	case strings.HasSuffix(name, "_banner"):
		colour, native = strings.TrimSuffix(name, "_banner"), "minecraft:standing_banner"
	case strings.HasSuffix(name, "_bed"):
		colour, native = strings.TrimSuffix(name, "_bed"), "minecraft:bed"
	case strings.HasSuffix(name, "_shulker_box"):
		colour, native = strings.TrimSuffix(name, "_shulker_box"), name
	default:
		return ""
	}
	switch colour {
	case "minecraft:white", "minecraft:orange", "minecraft:magenta", "minecraft:light_blue",
		"minecraft:yellow", "minecraft:lime", "minecraft:pink", "minecraft:gray",
		"minecraft:light_gray", "minecraft:cyan", "minecraft:purple", "minecraft:blue",
		"minecraft:brown", "minecraft:green", "minecraft:red", "minecraft:black":
		return native
	}
	return ""
}

func javaActorPaletteState(name string, properties map[string]any) (string, map[string]any) {
	native := ActorBlockName(name)
	if native == "" {
		return name, properties
	}
	if !strings.HasSuffix(native, "_shulker_box") {
		return native, properties
	}
	// Shulker facing is stored in the block actor, not the palette.
	out := make(map[string]any, len(properties))
	for key, value := range properties {
		if key != "facing" {
			out[key] = value
		}
	}
	return native, out
}

func resolveJavaState(registry chunk.BlockRegistry, state BlockState) (NativeState, error) {
	if registry == nil {
		return NativeState{}, fmt.Errorf("java state codec: nil block registry")
	}
	javaName := NormaliseBlockName(state.Name)
	if javaName == "" {
		return NativeState{}, fmt.Errorf("java state codec: empty block name")

	}
	properties := gamedata.BlockPropertiesWithDefaults(javaName, state.Properties)
	paletteName, paletteProperties := javaActorPaletteState(javaName, properties)
	name, props, err := nativeBlockState(registry, paletteName, paletteProperties)
	if err != nil {
		return NativeState{}, err
	}
	block, ok := registry.StateToRuntimeID(name, props)
	if !ok {
		return NativeState{}, fmt.Errorf("java state codec: no native state for %s%v (mapped to %s%v)", javaName, state.Properties, name, props)
	}
	liquid := registry.AirRuntimeID()
	if needsWaterLayer(javaName, properties) {
		liquid, ok = registry.StateToRuntimeID("minecraft:water", waterSourceProperties)
		if !ok {
			return NativeState{}, fmt.Errorf("java state codec: native water source state is unavailable")
		}
	}
	return NativeState{Block: block, Liquid: liquid}, nil
}

func nativeBlockState(registry chunk.BlockRegistry, javaName string, javaProps map[string]any) (string, map[string]any, error) {
	// Available aliases also disambiguate identifiers shared by both palettes:
	// native stone_stairs are Java cobblestone_stairs, not Java stone_stairs.
	name := javaName
	if alias := javaToNativeBlockNames[javaName]; alias != "" && hasNativeBlock(registry, alias) {
		name = alias
	}
	if strings.HasPrefix(javaName, "minecraft:cave_vines") {
		berries, _ := boolProperty(javaProps["berries"])
		switch javaName {
		case "minecraft:cave_vines":
			if berries {
				name = "minecraft:cave_vines_head_with_berries"
			} else {
				name = "minecraft:cave_vines"
			}
		case "minecraft:cave_vines_plant":
			if berries {
				name = "minecraft:cave_vines_body_with_berries"
			} else {
				name = "minecraft:cave_vines"
			}
		}
	}
	if javaName == "minecraft:water_cauldron" || javaName == "minecraft:lava_cauldron" || javaName == "minecraft:powder_snow_cauldron" {
		name = "minecraft:cauldron"
	}
	if javaName == "minecraft:daylight_detector" {
		if inverted, _ := boolProperty(javaProps["inverted"]); inverted {
			name = "minecraft:daylight_detector_inverted"
		}
	}
	if strings.HasPrefix(javaName, "minecraft:potted_") && !hasNativeBlock(registry, javaName) {
		name = "minecraft:flower_pot"
	}
	if strings.HasSuffix(javaName, "_wall_hanging_sign") {
		name = strings.TrimSuffix(javaName, "_wall_hanging_sign") + "_hanging_sign"
	}
	if javaName == "minecraft:oak_sign" && !hasNativeBlock(registry, javaName) {
		if javaProps["rotation"] != nil {
			name = "minecraft:standing_sign"
		} else {
			name = "minecraft:wall_sign"
		}
	} else if strings.HasSuffix(javaName, "_sign") && !strings.HasSuffix(javaName, "_wall_sign") && !strings.HasSuffix(javaName, "_hanging_sign") && !hasNativeBlock(registry, javaName) {
		wood := strings.TrimSuffix(javaName, "_sign")
		if wood == "minecraft:dark_oak" {
			wood = "minecraft:darkoak"
		}
		if javaProps["rotation"] != nil {
			name = wood + "_standing_sign"
		} else {
			name = wood + "_wall_sign"
		}
	} else if strings.HasSuffix(javaName, "_wall_sign") && !hasNativeBlock(registry, javaName) {
		name = "minecraft:wall_sign"
	}
	if javaName == "minecraft:repeater" || javaName == "minecraft:comparator" {
		powered, _ := boolProperty(javaProps["powered"])
		prefix := "unpowered_"
		if powered {
			prefix = "powered_"
		}
		name = "minecraft:" + prefix + strings.TrimPrefix(javaName, "minecraft:")
	}
	if javaName == "minecraft:furnace" || javaName == "minecraft:smoker" || javaName == "minecraft:blast_furnace" {
		lit, _ := boolProperty(javaProps["lit"])
		if lit {
			name = "minecraft:lit_" + strings.TrimPrefix(javaName, "minecraft:")
		}
	}
	if javaName == "minecraft:redstone_torch" || javaName == "minecraft:redstone_wall_torch" {
		if lit, present := boolProperty(javaProps["lit"]); present && !lit {
			name = "minecraft:unlit_redstone_torch"
		} else if name != "minecraft:unlit_redstone_torch" {
			name = "minecraft:redstone_torch"
		}
	}
	if javaName == "minecraft:redstone_lamp" || javaName == "minecraft:redstone_ore" || javaName == "minecraft:deepslate_redstone_ore" {
		if lit, _ := boolProperty(javaProps["lit"]); lit {
			name = "minecraft:lit_" + strings.TrimPrefix(javaName, "minecraft:")
		}
	}
	if javaName == "minecraft:big_dripleaf_stem" && !hasNativeBlock(registry, javaName) {
		name = "minecraft:big_dripleaf"
	}
	if javaName == "minecraft:small_dripleaf" && !hasNativeBlock(registry, javaName) {
		name = "minecraft:small_dripleaf_block"
	}
	if javaName == "minecraft:light" && !hasNativeBlock(registry, javaName) {
		level, ok := intProperty(javaProps["level"])
		if !ok {
			level = 0
		}
		if level < 0 || level > 15 {
			return "", nil, fmt.Errorf("java state codec: invalid light level %d", level)
		}
		name = "minecraft:light_block_" + strconv.Itoa(level)
	}
	if javaName == "minecraft:double_plant" {
		return "", nil, fmt.Errorf("java state codec: unsupported synthetic Java block name %q", javaName)
	}
	if slabType, ok := stringProperty(javaProps["type"]); ok && slabType == "double" && strings.HasSuffix(javaName, "_slab") {
		if strings.HasSuffix(name, "cut_copper_slab") {
			name = strings.TrimSuffix(name, "cut_copper_slab") + "double_cut_copper_slab"
		} else {
			name = strings.TrimSuffix(name, "_slab") + "_double_slab"
		}
		if !hasNativeBlock(registry, name) {
			return "", nil, fmt.Errorf("java state codec: no native double-slab block for %q", javaName)
		}
	}
	if javaName == "minecraft:tall_seagrass" && !hasNativeBlock(registry, javaName) {
		name = "minecraft:seagrass"
	}
	if javaName == "minecraft:kelp_plant" && !hasNativeBlock(registry, javaName) {
		name = "minecraft:kelp"
	}
	if javaName == "minecraft:piston_head" {
		headType, ok := stringProperty(javaProps["type"])
		if !ok || (headType != "normal" && headType != "sticky") {
			return "", nil, fmt.Errorf("java state codec: invalid piston head type %v", javaProps["type"])
		}
		if headType == "sticky" {
			name = "minecraft:sticky_piston_arm_collision"
		}
	}
	defaultRID, ok := registry.StateToRuntimeID(name, nil)
	if !ok {
		return "", nil, fmt.Errorf("java state codec: Java block %q has no native block alias (%q)", javaName, name)
	}
	_, schema, ok := registry.RuntimeIDToState(defaultRID)
	if !ok {
		return "", nil, fmt.Errorf("java state codec: registry cannot decode native default state %q", name)
	}
	out := make(map[string]any, len(javaProps)+len(schema))
	for key, value := range javaProps {
		target, mapped, consume, err := mapJavaProperty(javaName, name, key, value, javaProps, schema)
		if err != nil {
			return "", nil, err
		}
		if consume {
			continue
		}
		if target == "" {
			return "", nil, fmt.Errorf("java state codec: unsupported property %q=%v on %s", key, value, javaName)
		}
		if javaName == "minecraft:jigsaw" && key == "orientation" && target == "facing_direction" {
			orientation, _ := stringProperty(value)
			_, rotation, ok := jigsawOrientationParts(orientation)
			if !ok {
				return "", nil, fmt.Errorf("java state codec: unsupported jigsaw orientation %v", value)
			}
			out["rotation"] = valueForSchema(rotation, schema["rotation"])
		}
		out[target] = mapped
	}
	switch javaName {
	case "minecraft:torch", "minecraft:soul_torch", "minecraft:redstone_torch", "minecraft:copper_torch":
		out["torch_facing_direction"] = "top"
	}
	if strings.HasSuffix(javaName, "_hanging_sign") {
		wall := strings.HasSuffix(javaName, "_wall_hanging_sign")
		out["hanging"] = nativeBool(!wall, schema["hanging"])
		if wall {
			out["attached_bit"] = nativeBool(false, schema["attached_bit"])
			out["ground_sign_direction"] = int32(0)
		} else {
			rotation, ok := intProperty(javaProps["rotation"])
			if !ok || rotation < 0 || rotation > 15 {
				return "", nil, fmt.Errorf("java state codec: invalid hanging sign rotation %v", javaProps["rotation"])
			}
			attached, _ := boolProperty(javaProps["attached"])
			out["facing_direction"] = int32(0)
			out["ground_sign_direction"] = int32(rotation)
			if !attached && rotation%4 == 0 {
				out["facing_direction"] = [...]int32{3, 4, 2, 5}[rotation/4]
				out["ground_sign_direction"] = int32(0)
			}
		}
	}
	if javaName == "minecraft:sea_pickle" {
		waterlogged, _ := boolProperty(javaProps["waterlogged"])
		out["dead_bit"] = nativeBool(!waterlogged, schema["dead_bit"])
	}
	if name == "minecraft:cauldron" {
		switch javaName {
		case "minecraft:water_cauldron":
			out["cauldron_liquid"] = "water"
		case "minecraft:powder_snow_cauldron":
			out["cauldron_liquid"] = "powder_snow"
		case "minecraft:lava_cauldron":
			out["cauldron_liquid"] = "lava"
			out["fill_level"] = int32(5)
		}
	}
	if javaName == "minecraft:comparator" {
		if _, exists := schema["output_lit_bit"]; exists {
			powered, ok := boolProperty(javaProps["powered"])
			if !ok {
				return "", nil, fmt.Errorf("java state codec: comparator has invalid powered property %v", javaProps["powered"])
			}
			out["output_lit_bit"] = nativeBool(powered, schema["output_lit_bit"])
		}
	}
	if slabType, ok := stringProperty(javaProps["type"]); ok && slabType == "double" && strings.HasSuffix(javaName, "_slab") {
		// A double slab is represented by its distinct native block name.
		delete(out, "type")
	}
	if javaName == "minecraft:water" || javaName == "minecraft:lava" {
		if level, ok := intProperty(javaProps["level"]); ok {
			out["liquid_depth"] = int32(level)
		}
	}
	if name == "minecraft:big_dripleaf" {
		if head, ok := schema["big_dripleaf_head"]; ok {
			out["big_dripleaf_head"] = nativeBool(javaName == "minecraft:big_dripleaf", head)
		}
	}
	if name == "minecraft:red_flower" {
		// Old-style Bedrock flower aliases encode species in this palette property.
		flowerType, ok := redFlowerNativeTypes[javaName]
		if !ok {
			return "", nil, fmt.Errorf("java state codec: unsupported flower alias %s", javaName)
		}
		if _, exists := schema["red_flower_type"]; exists {
			out["red_flower_type"] = flowerType
		} else if _, exists := schema["flower_type"]; exists {
			out["flower_type"] = flowerType
		} else if _, exists := schema["type"]; exists {
			out["type"] = flowerType
		} else {
			return "", nil, fmt.Errorf("java state codec: red flower native palette has no species property")
		}
	}
	if name == "minecraft:double_plant" {
		flowerType, ok := doublePlantNativeTypes[javaName]
		if !ok {
			return "", nil, fmt.Errorf("java state codec: unsupported double-plant alias %s", javaName)
		}
		if _, exists := schema["double_plant_type"]; exists {
			out["double_plant_type"] = flowerType
		} else if _, exists := schema["tall_grass_type"]; exists {
			out["tall_grass_type"] = flowerType
		} else if _, exists := schema["type"]; exists {
			out["type"] = flowerType
		} else {
			return "", nil, fmt.Errorf("java state codec: double plant native palette has no species property")
		}
	}
	if javaName == "minecraft:tall_seagrass" && name == "minecraft:seagrass" {
		half, ok := stringProperty(javaProps["half"])
		if !ok || (half != "upper" && half != "lower") {
			return "", nil, fmt.Errorf("java state codec: tall seagrass needs upper/lower half, got %v", javaProps["half"])
		}
		grassType := "double_bot"
		if half == "upper" {
			grassType = "double_top"
		}
		out["sea_grass_type"] = grassType
	}
	if javaName == "minecraft:kelp_plant" && name == "minecraft:kelp" {
		out["kelp_age"] = int32(0)
	}
	if javaName == "minecraft:attached_melon_stem" || javaName == "minecraft:attached_pumpkin_stem" {
		out["growth"] = valueForSchema(int32(7), schema["growth"])
	}
	if javaName == "minecraft:melon_stem" || javaName == "minecraft:pumpkin_stem" {
		if _, exists := out["facing_direction"]; !exists {
			facing, err := convertJavaFacing("facing_direction", "up", javaName)
			if err != nil {
				return "", nil, err
			}
			out["facing_direction"] = facing
		}
	}
	if strings.HasPrefix(name, "minecraft:cave_vines") {
		if _, exists := out["growing_plant_age"]; !exists {
			out["growing_plant_age"] = int32(0)
		}
	}
	return name, out, nil
}

func hasNativeBlock(registry chunk.BlockRegistry, name string) bool {
	_, ok := registry.StateToRuntimeID(name, nil)
	return ok
}

func mapJavaProperty(javaName, nativeName, key string, value any, javaProps, schema map[string]any) (target string, mapped any, consume bool, err error) {
	if javaName == "minecraft:nether_portal" && key == "axis" {
		return "portal_axis", valueForSchema(value, schema["portal_axis"]), false, nil
	}
	if javaName == "minecraft:sea_pickle" && key == "pickles" {
		count, ok := intProperty(value)
		if !ok || count < 1 || count > 4 {
			return "", nil, false, fmt.Errorf("java state codec: invalid sea pickle count %v", value)
		}
		return "cluster_count", int32(count - 1), false, nil
	}
	if javaName == "minecraft:jigsaw" && key == "orientation" {
		orientation, ok := stringProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid jigsaw orientation %v", value)
		}
		front, _, ok := jigsawOrientationParts(orientation)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: unsupported jigsaw orientation %q", orientation)
		}
		if _, ok := schema["orientation"]; ok {
			return "orientation", valueForSchema(value, schema["orientation"]), false, nil
		}
		if _, ok := schema["facing_direction"]; !ok {
			return "", nil, false, fmt.Errorf("java state codec: native jigsaw has no facing_direction property")
		}
		if _, ok := schema["rotation"]; !ok {
			return "", nil, false, fmt.Errorf("java state codec: native jigsaw has no rotation property")
		}
		mapped, err := convertJavaFacing("facing_direction", front, javaName)
		return "facing_direction", mapped, false, err
	}
	if (javaName == "minecraft:pointed_dripstone" || javaName == "minecraft:sulfur_spike") && key == "vertical_direction" {
		direction, ok := stringProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid dripstone vertical_direction %v", value)
		}
		if direction != "up" && direction != "down" {
			return "", nil, false, fmt.Errorf("java state codec: unsupported dripstone vertical_direction %q", direction)
		}
		if schemaValue, ok := schema["hanging"]; ok {
			return "hanging", nativeBool(direction == "down", schemaValue), false, nil
		}
		if schemaValue, ok := schema["vertical_direction"]; ok {
			return "vertical_direction", valueForSchema(value, schemaValue), false, nil
		}
		return "", nil, false, fmt.Errorf("java state codec: native %s has neither hanging nor vertical_direction", javaName)
	}
	if (javaName == "minecraft:pointed_dripstone" || javaName == "minecraft:sulfur_spike") && key == "thickness" {
		thickness, ok := stringProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid dripstone thickness %v", value)
		}
		if _, ok := schema["dripstone_thickness"]; ok {
			if thickness == "tip_merge" {
				thickness = "merge"
			}
			return "dripstone_thickness", thickness, false, nil
		}
		if _, ok := schema["thickness"]; ok {
			return "thickness", valueForSchema(value, schema["thickness"]), false, nil
		}
		return "", nil, false, fmt.Errorf("java state codec: native %s has no dripstone thickness property", javaName)
	}
	if key == "flower_amount" && (javaName == "minecraft:pink_petals" || javaName == "minecraft:wildflowers") {
		amount, ok := intProperty(value)
		if !ok || amount < 1 || amount > 4 {
			return "", nil, false, fmt.Errorf("java state codec: invalid flower amount %v on %s", value, javaName)
		}
		if _, ok := schema["growth"]; ok {
			return "growth", int32(amount - 1), false, nil
		}
	}
	if key == "moisture" && javaName == "minecraft:farmland" {
		amount, ok := intProperty(value)
		if !ok || amount < 0 || amount > 7 {
			return "", nil, false, fmt.Errorf("java state codec: invalid farmland moisture %v", value)
		}
		if _, ok := schema["moisturized_amount"]; ok {
			return "moisturized_amount", int32(amount), false, nil
		}
	}
	if javaName == "minecraft:leaf_litter" && key == "segment_amount" {
		amount, ok := intProperty(value)
		if !ok || amount < 1 || amount > 4 {
			return "", nil, false, fmt.Errorf("java state codec: invalid leaf litter segment amount %v", value)
		}
		return "growth", int32(amount - 1), false, nil
	}
	if isSnowyGroundBlock(javaName) && key == "snowy" {
		return "", nil, true, nil
	}
	if javaName == "minecraft:daylight_detector" && key == "inverted" {
		return "", nil, true, nil
	}
	if javaName == "minecraft:target" && key == "power" {
		return "", nil, true, nil
	}
	if javaName == "minecraft:piston_head" && key == "type" {
		headType, ok := stringProperty(value)
		if !ok || (headType != "normal" && headType != "sticky") {
			return "", nil, false, fmt.Errorf("java state codec: invalid piston head type %v", value)
		}
		return "", nil, true, nil
	}
	// The native moving-block palette has no properties. Java facing/type
	// remain in the retained canonical state, independently of its actor.
	if javaName == "minecraft:moving_piston" {
		switch key {
		case "facing":
			_, err := convertJavaFacing("facing_direction", value, javaName)
			return "", nil, true, err
		case "type":
			pistonType, ok := stringProperty(value)
			if !ok || (pistonType != "normal" && pistonType != "sticky") {
				return "", nil, false, fmt.Errorf("java state codec: invalid moving piston type %v", value)
			}
			return "", nil, true, nil
		}
	}
	// Repeater/skull activation is neighbor-derived in Java, while piston extension
	// is represented by a separate head block rather than a native base-state bit.
	if key == "locked" && isRepeater(javaName) {
		if _, ok := schema["locked"]; !ok {
			return "", nil, true, nil
		}
	}
	if key == "extended" && (javaName == "minecraft:piston" || javaName == "minecraft:sticky_piston") {
		if _, ok := schema["extended_bit"]; !ok {
			return "", nil, true, nil
		}
	}
	if key == "short" && javaName == "minecraft:piston_head" {
		if _, ok := schema["short"]; !ok {
			return "", nil, true, nil
		}
	}
	if key == "powered" && isSkull(javaName) {
		if _, ok := schema["powered"]; !ok {
			return "", nil, true, nil
		}
	}
	if key == "powered" && (strings.HasSuffix(javaName, "_door") || strings.HasSuffix(javaName, "_trapdoor") || strings.HasSuffix(javaName, "_fence_gate")) {
		// Native open_bit carries openness, not Java's independent power latch.
		if _, ok := boolProperty(value); !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid powered property %v", value)
		}
		return "", nil, true, nil
	}
	if javaName == "minecraft:chorus_plant" && (key == "up" || key == "down" || key == "north" || key == "south" || key == "east" || key == "west") {
		// Native chorus connections are derived from neighbouring blocks.
		if _, ok := boolProperty(value); !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid chorus connection %s=%v", key, value)
		}
		return "", nil, true, nil
	}
	if strings.HasSuffix(javaName, "copper_golem_statue") && key == "copper_golem_pose" {
		// NativeBlockForState carries pose in the block actor, not the palette.
		switch value {
		case "standing", "sitting", "running", "star":
			return "", nil, true, nil
		}
		return "", nil, false, fmt.Errorf("java state codec: invalid copper golem pose %v", value)
	}
	if javaName == "minecraft:mangrove_propagule" && key == "stage" {
		// The tree-growth latch is server-side; native propagule_stage carries age.
		stage, ok := intProperty(value)
		if !ok || stage < 0 || stage > 1 {
			return "", nil, false, fmt.Errorf("java state codec: invalid propagule stage %v", value)
		}
		return "", nil, true, nil
	}
	if key == "candles" && (javaName == "minecraft:candle" || strings.HasSuffix(javaName, "_candle")) {
		count, ok := intProperty(value)
		if !ok || count < 1 || count > 4 {
			return "", nil, false, fmt.Errorf("java state codec: invalid candle count %v", value)
		}
		if _, ok := schema["candles"]; ok {
			return "candles", int32(count - 1), false, nil
		}
	}
	if key == "power" && (javaName == "minecraft:sculk_sensor" || javaName == "minecraft:calibrated_sculk_sensor") && schema["redstone_signal"] == nil {
		power, ok := intProperty(value)
		if !ok || power < 0 || power > 15 {
			return "", nil, false, fmt.Errorf("java state codec: invalid sculk sensor power %v", value)
		}
		// Power lives in the source actor sidecar, not the native palette.
		return "", nil, true, nil
	}
	if javaName == "minecraft:structure_block" && key == "mode" {
		if _, ok := schema["structure_block_type"]; ok {
			return "structure_block_type", valueForSchema(value, schema["structure_block_type"]), false, nil
		}
	}
	if javaName == "minecraft:hopper" && key == "enabled" {
		enabled, ok := boolProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid hopper enabled property %v", value)
		}
		return "toggle_bit", nativeBool(!enabled, schema["toggle_bit"]), false, nil
	}
	if isMushroomBlock(javaName) && (key == "north" || key == "south" || key == "east" || key == "west") {
		variant, err := mushroomVariant(javaProps)
		return "huge_mushroom_bits", int32(variant), false, err
	}
	if isMushroomBlock(javaName) && (key == "up" || key == "down") {
		// Bedrock's legacy mushroom variant only stores the four side textures.
		return "", nil, true, nil
	}
	if javaName == "minecraft:chiseled_bookshelf" && strings.HasPrefix(key, "slot_") {
		index, ok := occupiedSlotIndex(key)
		if !ok || index >= 6 {
			return "", nil, false, fmt.Errorf("java state codec: invalid chiseled bookshelf slot property %q", key)
		}
		mask, err := occupiedSlotMask(javaProps, 6)
		return "books_stored", int32(mask), false, err
	}
	// Bedrock's 0..3 shelf type represents Java's side-chain part, not inventory occupancy.
	if isShelf(javaName) && key == "side_chain" {
		if _, ok := schema["powered_shelf_type"]; ok {
			part, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid shelf side chain %v", value)
			}
			switch part {
			case "unconnected":
				return "powered_shelf_type", int32(0), false, nil
			case "right":
				return "powered_shelf_type", int32(1), false, nil
			case "center":
				return "powered_shelf_type", int32(2), false, nil
			case "left":
				return "powered_shelf_type", int32(3), false, nil
			default:
				return "", nil, false, fmt.Errorf("java state codec: unsupported shelf side chain %q", part)
			}
		}
	}
	if javaName == "minecraft:brewing_stand" && strings.HasPrefix(key, "has_bottle_") {
		index, err := strconv.Atoi(strings.TrimPrefix(key, "has_bottle_"))
		if err != nil || index < 0 || index > 2 {
			return "", nil, false, fmt.Errorf("java state codec: invalid brewing stand slot property %q", key)
		}
		nativeKey := [...]string{"brewing_stand_slot_a_bit", "brewing_stand_slot_b_bit", "brewing_stand_slot_c_bit"}[index]
		return nativeKey, valueForSchema(value, schema[nativeKey]), false, nil
	}
	if javaName == "minecraft:comparator" && key == "mode" {
		mode, ok := stringProperty(value)
		if !ok || (mode != "compare" && mode != "subtract") {
			return "", nil, false, fmt.Errorf("java state codec: invalid comparator mode %v", value)
		}
		return "output_subtract_bit", nativeBool(mode == "subtract", schema["output_subtract_bit"]), false, nil
	}
	if isRepeater(javaName) && key == "delay" {
		delay, ok := intProperty(value)
		if !ok || delay < 1 || delay > 4 {
			return "", nil, false, fmt.Errorf("java state codec: invalid repeater delay %v", value)
		}
		return "repeater_delay", int32(delay - 1), false, nil
	}
	if key == "berries" && strings.HasPrefix(javaName, "minecraft:cave_vines") {
		return "", nil, true, nil
	}
	if isCampfire(javaName) && key == "lit" {
		lit, ok := boolProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid campfire lit property %v", value)
		}
		return "extinguished", nativeBool(!lit, schema["extinguished"]), false, nil
	}
	if isCampfire(javaName) && key == "signal_fire" {
		// Java derives this state from the hay block below; Bedrock recalculates it.
		return "", nil, true, nil
	}
	if isFire(javaName) && (key == "north" || key == "south" || key == "east" || key == "west" || key == "up") {
		// Fire geometry is neighbor-derived in Java and Bedrock.
		return "", nil, true, nil
	}
	if javaName == "minecraft:vine" && (key == "north" || key == "south" || key == "east" || key == "west") {
		bits, err := vineDirectionBits(javaProps)
		return "vine_direction_bits", int32(bits), false, err
	}
	if javaName == "minecraft:vine" && key == "up" {
		// Java's top attachment has no Bedrock vine_direction_bits equivalent.
		return "", nil, true, nil
	}
	if isFire(javaName) && key == "age" {
		if _, ok := schema["age"]; ok {
			return "age", valueForSchema(value, schema["age"]), false, nil
		}
	}
	if key == "lit" && (strings.Contains(javaName, "furnace") || javaName == "minecraft:smoker" || isRedstoneLitBlock(javaName)) {
		return "", nil, true, nil
	}
	if key == "power" && schema["redstone_signal"] != nil {
		n, ok := intProperty(value)
		if !ok || n < 0 || n > 15 {
			return "", nil, false, fmt.Errorf("java state codec: invalid redstone power %v", value)
		}
		return "redstone_signal", int32(n), false, nil
	}
	if key == "lit" && isCampfire(javaName) {
		lit, ok := boolProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid campfire lit property %v", value)
		}
		return "extinguished", nativeBool(!lit, schema["extinguished"]), false, nil
	}
	if key == "layers" {
		if _, ok := schema["height"]; ok {
			n, ok := intProperty(value)
			if !ok || n < 1 {
				return "", nil, false, fmt.Errorf("java state codec: invalid snow layer count %v", value)
			}
			return "height", int32(n - 1), false, nil
		}
	}
	if key == "powered" && (javaName == "minecraft:repeater" || javaName == "minecraft:comparator") {
		return "", nil, true, nil
	}
	if key == "waterlogged" {
		waterlogged, ok := boolProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: non-boolean waterlogged property on %s", javaName)
		}
		if waterlogged && !hasJavaWaterloggedProperty(javaName) {
			return "", nil, false, fmt.Errorf("java state codec: %s has no Java waterlogged state", javaName)
		}
		return "", nil, true, nil
	}
	if isMultiFaceBlock(javaName) && isMultiFaceProperty(key) {
		bits, err := javaMultiFaceBits(javaProps)
		return "multi_face_direction_bits", int32(bits), false, err
	}
	if key == "distance" && isLeafBlock(javaName) {
		// Java leaf distance is derived from nearby logs. The Bedrock palette stores only
		// persistent_bit/update_bit; recomputing distance requires the neighborhood, so it
		// is intentionally reconstructed as the conservative distance 7 by CanonicalState.
		return "", nil, true, nil
	}
	if key == "type" && strings.HasSuffix(javaName, "_slab") {
		text, ok := stringProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid slab type %v", value)
		}
		if text == "double" {
			return "", nil, true, nil
		}
		if _, ok := schema["minecraft:vertical_half"]; ok {
			return "minecraft:vertical_half", text, false, nil
		}
	}
	if key == "leaves" && javaName == "minecraft:bamboo" {
		if _, ok := schema["bamboo_leaf_size"]; ok {
			leaves, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid bamboo leaves %v", value)
			}
			switch leaves {
			case "none":
				return "bamboo_leaf_size", "no_leaves", false, nil
			case "small":
				return "bamboo_leaf_size", "small_leaves", false, nil
			case "large":
				return "bamboo_leaf_size", "large_leaves", false, nil
			}
			return "", nil, false, fmt.Errorf("java state codec: unsupported bamboo leaves %q", leaves)
		}
	}
	if key == "age" && javaName == "minecraft:bamboo" {
		return "", nil, true, nil
	}
	if key == "half" && javaName == "minecraft:tall_seagrass" && nativeName == "minecraft:seagrass" {
		return "", nil, true, nil
	}
	if key == "shape" && isRail(javaName) {
		shape, ok := stringProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid rail shape %v", value)
		}
		direction, ok := javaRailDirection(shape)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: unsupported rail shape %q", shape)
		}
		return "rail_direction", int32(direction), false, nil
	}
	if key == "shape" && strings.HasSuffix(javaName, "_stairs") {
		if _, ok := schema["minecraft:corner"]; ok {
			shape, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid stair shape %v", value)
			}
			if shape == "straight" {
				shape = "none"
			}
			return "minecraft:corner", shape, false, nil
		}
	}
	if key == "face" && isGrindstone(javaName) {
		if _, ok := schema["attachment"]; ok {
			face, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid grindstone face %v", value)
			}
			attachment := grindstoneAttachment(face)
			if attachment == "" {
				return "", nil, false, fmt.Errorf("java state codec: unsupported grindstone face %q", face)
			}
			return "attachment", attachment, false, nil
		}
	}
	if key == "facing" && (javaName == "minecraft:bee_nest" || javaName == "minecraft:beehive") {
		if _, ok := schema["direction"]; ok {
			direction, err := convertJavaHorizontalFacing(value)
			return "direction", direction, false, err
		}
	}
	if key == "attachment" && isBell(javaName) {
		if _, ok := schema["attachment"]; ok {
			text, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid bell attachment %v", value)
			}
			attachment := bellAttachment(text)
			if attachment == "" {
				return "", nil, false, fmt.Errorf("java state codec: unsupported bell attachment %q", text)
			}
			return "attachment", attachment, false, nil
		}
	}
	if (key == "face" || key == "facing") && isButton(javaName) {
		if _, ok := schema["facing_direction"]; ok {
			direction, err := buttonFacing(javaProps)
			return "facing_direction", direction, false, err
		}
	}
	if (key == "face" || key == "facing") && isLever(javaName) {
		if _, ok := schema["lever_direction"]; ok {
			direction, err := leverDirection(javaProps)
			return "lever_direction", direction, false, err
		}
	}
	if key == "tilt" && javaName == "minecraft:big_dripleaf" {
		if _, ok := schema["big_dripleaf_tilt"]; ok {
			text, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid big dripleaf tilt %v", value)
			}
			switch text {
			case "partial":
				text = "partial_tilt"
			case "full":
				text = "full_tilt"
			case "none", "unstable":
			default:
				return "", nil, false, fmt.Errorf("java state codec: unsupported big dripleaf tilt %q", text)
			}
			return "big_dripleaf_tilt", text, false, nil
		}
	}
	if key == "facing" && isTrapdoor(javaName) {
		if _, ok := schema["direction"]; ok {
			direction, err := convertJavaTrapdoorFacing(value)
			return "direction", direction, false, err
		}
	}
	if key == "facing" && isButton(javaName) {
		if _, ok := schema["facing_direction"]; ok {
			direction, err := buttonFacing(javaProps)
			return "facing_direction", direction, false, err
		}
	}
	if key == "facing" && isLever(javaName) {
		if _, ok := schema["lever_direction"]; ok {
			direction, err := leverDirection(javaProps)
			return "lever_direction", direction, false, err
		}
	}
	if key == "facing" && isBell(javaName) {
		if _, ok := schema["direction"]; ok {
			direction, err := convertJavaHorizontalFacing(value)
			return "direction", direction, false, err
		}
	}
	if key == "facing" && isGrindstone(javaName) {
		if _, ok := schema["direction"]; ok {
			direction, err := convertJavaHorizontalFacing(value)
			return "direction", direction, false, err
		}
	}
	if key == "facing" && isSkull(javaName) {
		if _, ok := schema["facing_direction"]; ok {
			mapped, err := convertJavaFacing("facing_direction", value, javaName)
			return "facing_direction", mapped, false, err
		}
	}
	if key == "facing" && strings.HasSuffix(javaName, "_door") {
		if _, ok := schema["minecraft:cardinal_direction"]; ok {
			facing, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid door facing %v", value)
			}
			switch facing {
			case "north":
				return "minecraft:cardinal_direction", "east", false, nil
			case "south":
				return "minecraft:cardinal_direction", "west", false, nil
			case "west":
				return "minecraft:cardinal_direction", "north", false, nil
			case "east":
				return "minecraft:cardinal_direction", "south", false, nil
			default:
				return "", nil, false, fmt.Errorf("java state codec: unsupported door facing %q", facing)
			}
		}
	}
	if key == "facing" && strings.HasSuffix(javaName, "_coral_wall_fan") {
		if _, ok := schema["coral_direction"]; ok {
			facing, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid coral wall fan facing %v", value)
			}
			switch facing {
			case "west":
				return "coral_direction", int32(0), false, nil
			case "east":
				return "coral_direction", int32(1), false, nil
			case "north":
				return "coral_direction", int32(2), false, nil
			case "south":
				return "coral_direction", int32(3), false, nil
			default:
				return "", nil, false, fmt.Errorf("java state codec: unsupported coral wall fan facing %q", facing)
			}
		}
	}
	if key == "facing" {
		for _, candidate := range facingPropertyCandidates {
			if _, ok := schema[candidate]; ok {
				mapped, err := convertJavaFacing(candidate, value, javaName)
				return candidate, mapped, false, err
			}
		}
	}
	if key == "rotation" {
		for _, candidate := range rotationPropertyCandidates {
			if _, ok := schema[candidate]; ok {
				return candidate, valueForSchema(value, schema[candidate]), false, nil
			}
		}
	}
	if key == "rotation" && isSkull(javaName) {
		// Bedrock's skull palette has only six facing states; the Java 16-step
		// floor rotation is retained by its block-entity payload, not block state.
		if _, ok := schema["facing_direction"]; ok {
			return "facing_direction", int32(1), false, nil
		}
	}
	if key == "half" {
		if _, ok := schema["minecraft:vertical_half"]; ok {
			return "minecraft:vertical_half", valueForSchema(value, schema["minecraft:vertical_half"]), false, nil
		}
		if _, ok := schema["upside_down_bit"]; ok {
			text, ok := stringProperty(value)
			if !ok || (text != "top" && text != "bottom") {
				return "", nil, false, fmt.Errorf("java state codec: invalid half %v on %s", value, javaName)
			}
			return "upside_down_bit", nativeBool(text == "top", schema["upside_down_bit"]), false, nil
		}
		if _, ok := schema["upper_block_bit"]; ok {
			text, ok := stringProperty(value)
			if !ok || (text != "upper" && text != "lower" && text != "top" && text != "bottom") {
				return "", nil, false, fmt.Errorf("java state codec: invalid half %v on %s", value, javaName)
			}
			return "upper_block_bit", nativeBool(text == "upper" || text == "top", schema["upper_block_bit"]), false, nil
		}
	}
	if key == "part" && (javaName == "minecraft:bed" || javaName == "minecraft:straw_bed") {
		if _, ok := schema["head_piece_bit"]; ok {
			part, ok := stringProperty(value)
			if !ok || (part != "head" && part != "foot") {
				return "", nil, false, fmt.Errorf("java state codec: invalid bed part %v", value)
			}
			return "head_piece_bit", nativeBool(part == "head", schema["head_piece_bit"]), false, nil
		}
	}
	if key == "bottom" && javaName == "minecraft:pale_moss_carpet" {
		if _, ok := schema["upper_block_bit"]; ok {
			bottom, ok := boolProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid pale moss bottom property %v", value)
			}
			return "upper_block_bit", nativeBool(!bottom, schema["upper_block_bit"]), false, nil
		}
	}
	if key == "in_wall" {
		if _, ok := schema["in_wall_bit"]; ok {
			return "in_wall_bit", valueForSchema(value, schema["in_wall_bit"]), false, nil
		}
	}
	if javaName == "minecraft:note_block" && (key == "instrument" || key == "note" || key == "powered") {
		// Bedrock stores note-block sound, pitch and power in its block actor, not palette state.
		return "", nil, true, nil
	}
	if javaName == "minecraft:jukebox" && key == "has_record" {
		// Record presence is represented by the jukebox block actor.
		return "", nil, true, nil
	}
	if javaName == "minecraft:lectern" && key == "has_book" {
		// Book contents and presence are represented by the lectern block actor.
		return "", nil, true, nil
	}
	if javaName == "minecraft:decorated_pot" && key == "cracked" {
		// Pot sherds and cracking are carried by the decorated-pot block actor.
		return "", nil, true, nil
	}
	if key == "power" && isRedstoneWire(javaName) {
		if _, ok := schema["redstone_signal"]; ok {
			n, ok := intProperty(value)
			if !ok || n < 0 || n > 15 {
				return "", nil, false, fmt.Errorf("java state codec: invalid redstone wire power %v", value)
			}
			return "redstone_signal", int32(n), false, nil
		}
	}
	if key == "hinge" {
		if _, ok := schema["door_hinge_bit"]; ok {
			text, ok := stringProperty(value)
			if !ok || (text != "left" && text != "right") {
				return "", nil, false, fmt.Errorf("java state codec: invalid door hinge %v", value)
			}
			return "door_hinge_bit", nativeBool(text == "right", schema["door_hinge_bit"]), false, nil
		}
	}
	if key == "persistent" && isLeafBlock(javaName) {
		if _, ok := schema["persistent_bit"]; ok {
			b, ok := boolProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid persistent leaf property %v", value)
			}
			return "persistent_bit", nativeBool(b, schema["persistent_bit"]), false, nil
		}
	}
	if key == "north" || key == "south" || key == "east" || key == "west" {
		if isWall(javaName) {
			nativeKey := "wall_connection_type_" + key
			if _, ok := schema[nativeKey]; ok {
				side, err := javaWallSide(value)
				return nativeKey, side, false, err
			}
		}
		if javaName == "minecraft:pale_moss_carpet" {
			nativeKey := "pale_moss_carpet_side_" + key
			if _, ok := schema[nativeKey]; ok {
				side, ok := stringProperty(value)
				if !ok || (side != "none" && side != "low" && side != "tall") {
					return "", nil, false, fmt.Errorf("java state codec: invalid pale moss side %s=%v", key, value)
				}
				if side == "low" {
					side = "short"
				}
				return nativeKey, side, false, nil
			}
		}
		if isRedstoneWire(javaName) {
			// Wire shape is recalculated from adjacent blocks; Bedrock stores only signal power.
			return "", nil, true, nil
		}
		if isConnectionBlock(javaName) {
			nativeKey := "minecraft:connection_" + key
			if _, ok := schema[nativeKey]; ok {
				b, ok := boolProperty(value)
				if !ok {
					return "", nil, false, fmt.Errorf("java state codec: invalid connection %s=%v", key, value)
				}
				return nativeKey, nativeBool(b, schema[nativeKey]), false, nil
			}
		}
	}
	if key == "up" && isWall(javaName) {
		if _, ok := schema["wall_post_bit"]; ok {
			b, ok := boolProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid wall post property %v", value)
			}
			return "wall_post_bit", nativeBool(b, schema["wall_post_bit"]), false, nil
		}
	}
	if key == "powered" {
		for _, candidate := range poweredPropertyCandidates {
			if _, ok := schema[candidate]; ok {
				b, ok := boolProperty(value)
				if !ok {
					return "", nil, false, fmt.Errorf("java state codec: invalid powered property %v", value)
				}
				return candidate, nativeBool(b, schema[candidate]), false, nil
			}
		}
		if _, ok := schema["redstone_signal"]; ok {
			b, ok := boolProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid powered property %v", value)
			}
			n := int32(0)
			if b {
				n = 15
			}
			return "redstone_signal", n, false, nil
		}
	}
	if (javaName == "minecraft:sculk_sensor" || javaName == "minecraft:calibrated_sculk_sensor") && key == "sculk_sensor_phase" {
		if _, ok := schema["sculk_sensor_phase"]; ok {
			phase, ok := stringProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid sculk sensor phase %v", value)
			}
			switch phase {
			case "inactive":
				return "sculk_sensor_phase", int32(0), false, nil
			case "active":
				return "sculk_sensor_phase", int32(1), false, nil
			case "cooldown":
				return "sculk_sensor_phase", int32(2), false, nil
			default:
				return "", nil, false, fmt.Errorf("java state codec: unsupported sculk sensor phase %q", phase)
			}
		}
	}
	if javaName == "minecraft:sculk_shrieker" && key == "shrieking" {
		if _, ok := schema["active"]; ok {
			b, ok := boolProperty(value)
			if !ok {
				return "", nil, false, fmt.Errorf("java state codec: invalid shrieking property %v", value)
			}
			return "active", nativeBool(b, schema["active"]), false, nil
		}
	}
	if javaName == "minecraft:turtle_egg" && (key == "eggs" || key == "hatch") || javaName == "minecraft:sniffer_egg" && key == "hatch" {
		n, ok := intProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid %s property %s=%v", javaName, key, value)
		}
		if key == "eggs" {
			if n < 1 || n > 4 {
				return "", nil, false, fmt.Errorf("java state codec: unsupported turtle egg count %d", n)
			}
			return "turtle_egg_count", [...]string{"", "one_egg", "two_egg", "three_egg", "four_egg"}[n], false, nil
		}
		if n < 0 || n > 2 {
			return "", nil, false, fmt.Errorf("java state codec: unsupported %s hatch %d", javaName, n)
		}
		return "cracked_state", [...]string{"no_cracks", "cracked", "max_cracked"}[n], false, nil
	}
	if javaName == "minecraft:scaffolding" && key == "distance" {
		if _, ok := schema["stability"]; ok {
			return "stability", valueForSchema(value, schema["stability"]), false, nil
		}
	}
	if javaName == "minecraft:scaffolding" && key == "bottom" {
		return "", nil, true, nil
	}
	if strings.HasSuffix(javaName, "_coral_fan") && key == "coral_fan_direction" {
		return "", nil, true, nil
	}
	if javaName == "minecraft:dried_ghast" && key == "hydration" {
		if _, ok := schema["rehydration_level"]; ok {
			return "rehydration_level", valueForSchema(value, schema["rehydration_level"]), false, nil
		}
	}
	// Bedrock stores this Java enum as an integer palette property.
	if javaName == "minecraft:trial_spawner" && key == "trial_spawner_state" {
		state, ok := stringProperty(value)
		if !ok {
			return "", nil, false, fmt.Errorf("java state codec: invalid trial spawner state %v", value)
		}
		switch state {
		case "inactive":
			return "trial_spawner_state", int32(0), false, nil
		case "waiting_for_players":
			return "trial_spawner_state", int32(1), false, nil
		case "active":
			return "trial_spawner_state", int32(2), false, nil
		case "waiting_for_reward_ejection":
			return "trial_spawner_state", int32(3), false, nil
		case "ejecting_reward":
			return "trial_spawner_state", int32(4), false, nil
		case "cooldown":
			return "trial_spawner_state", int32(5), false, nil
		default:
			return "", nil, false, fmt.Errorf("java state codec: unsupported trial spawner state %q", state)
		}
	}
	if key == "age" {
		for _, candidate := range agePropertyCandidates {
			if _, ok := schema[candidate]; ok {
				return candidate, valueForSchema(value, schema[candidate]), false, nil
			}
		}
	}
	if key == "stage" && javaName == "minecraft:bamboo" {
		if _, ok := schema["age_bit"]; ok {
			n, ok := intProperty(value)
			if !ok || (n != 0 && n != 1) {
				return "", nil, false, fmt.Errorf("java state codec: invalid bamboo stage %v", value)
			}
			return "age_bit", nativeBool(n == 1, schema["age_bit"]), false, nil
		}
	}
	if key == "stage" {
		if _, ok := schema["age_bit"]; ok {
			n, ok := intProperty(value)
			if !ok || (n != 0 && n != 1) {
				return "", nil, false, fmt.Errorf("java state codec: invalid plant stage %v", value)
			}
			return "age_bit", nativeBool(n == 1, schema["age_bit"]), false, nil
		}
	}
	if key == "level" && javaName == "minecraft:light" {
		return "", nil, true, nil
	}
	if key == "level" && (javaName == "minecraft:water_cauldron" || javaName == "minecraft:powder_snow_cauldron") {
		level, ok := intProperty(value)
		if !ok || level < 1 || level > 3 {
			return "", nil, false, fmt.Errorf("java state codec: invalid cauldron level %v", value)
		}
		return "fill_level", int32(level*2 - 1), false, nil
	}
	if javaName == "minecraft:composter" && key == "level" {
		if _, ok := schema["composter_fill_level"]; ok {
			return "composter_fill_level", valueForSchema(value, schema["composter_fill_level"]), false, nil
		}
	}
	if key == "level" {
		for _, candidate := range levelPropertyCandidates {
			if _, ok := schema[candidate]; ok {
				return candidate, valueForSchema(value, schema[candidate]), false, nil
			}
		}
	}
	if key == "type" && isChest(javaName) {
		// Chest left/right/single is neighbor-derived; Bedrock infers the pair from adjacent chests.
		if _, ok := stringProperty(value); ok {
			return "", nil, true, nil
		}
	}
	if key == "shape" && isConnectionBlock(javaName) {
		// Pane/fence/cable connections are Java neighbor-derived and represented by their four side properties.
		return "", nil, true, nil
	}
	if key == "face" && isTorchLike(javaName) {
		if _, ok := schema["torch_facing_direction"]; ok {
			text, err := nativeTorchFacing(javaProps)
			return "torch_facing_direction", text, false, err
		}
	}
	if nativeKey, ok := javaPropertyNativeKeys[key]; ok {
		if _, exists := schema[nativeKey]; exists {
			return nativeKey, valueForSchema(value, schema[nativeKey]), false, nil
		}
	}
	if _, exists := schema[key]; exists {
		return key, valueForSchema(value, schema[key]), false, nil
	}
	return "", nil, false, nil
}

var javaPropertyNativeKeys = map[string]string{
	"open": "open_bit", "lit": "lit", "extended": "extended_bit", "triggered": "triggered_bit", "locked": "open_bit",
	"bites": "bite_counter", "candles": "candles", "honey_level": "honey_level", "layers": "height", "age": "growth",
	"berries": "berry", "attached": "attached_bit", "hanging": "hanging", "instrument": "instrument", "mode": "output_subtract_bit",
	"conditional": "conditional_bit", "enabled": "enabled_bit", "occupied": "occupied_bit", "part": "head_piece_bit", "disarmed": "disarmed_bit",
	"short": "short", "waterlogged": "waterlogged_bit", "leaves": "leaves", "vertical_direction": "vertical_direction",
	"axis":     "pillar_axis",
	"eye":      "end_portal_eye_bit",
	"charges":  "respawn_anchor_charge",
	"unstable": "explode_bit",
	"dusted":   "brushed_progress",
	"drag":     "drag_down",
}

func reverseNativeState(registry chunk.BlockRegistry, state NativeState) (BlockState, error) {
	if registry == nil {
		return BlockState{}, fmt.Errorf("java state codec: nil block registry")
	}
	if state.Block == registry.AirRuntimeID() && state.Liquid != registry.AirRuntimeID() {
		liquidName, _, ok := registry.RuntimeIDToState(state.Liquid)
		if !ok {
			return BlockState{}, fmt.Errorf("java state codec: invalid native liquid runtime ID %d", state.Liquid)
		}
		if liquidName == "minecraft:water" || liquidName == "minecraft:flowing_water" || liquidName == "minecraft:lava" || liquidName == "minecraft:flowing_lava" {
			// Free fluid may occupy the secondary layer without a waterlogged block.
			state.Block, state.Liquid = state.Liquid, registry.AirRuntimeID()
		}
	}
	name, properties, ok := registry.RuntimeIDToState(state.Block)
	if !ok {
		return BlockState{}, fmt.Errorf("java state codec: invalid native block runtime ID %d", state.Block)
	}
	if isSkull(name) && intPropertyValue(properties["facing_direction"]) == 0 {
		return BlockState{}, fmt.Errorf("java state codec: native ceiling skull has no Java block-state representation")
	}
	typeName := javaBlockTypeForNative(name, properties)
	if name == "minecraft:melon_stem" || name == "minecraft:pumpkin_stem" {
		typeName = nativePlantStemJavaType(name, properties)
	}
	javaName := javaNameForNative(name)
	if isSkull(name) || name == "minecraft:torch" || name == "minecraft:soul_torch" || name == "minecraft:copper_torch" || name == "minecraft:redstone_torch" || name == "minecraft:unlit_redstone_torch" || name == "minecraft:lit_redstone_torch" || name == "minecraft:cauldron" || name == "minecraft:big_dripleaf" || name == "minecraft:melon_stem" || name == "minecraft:pumpkin_stem" {
		javaName = typeName
	}
	if name == "minecraft:torch" || name == "minecraft:soul_torch" || name == "minecraft:copper_torch" || name == "minecraft:redstone_torch" || name == "minecraft:unlit_redstone_torch" || name == "minecraft:lit_redstone_torch" {
		switch nativePropertyText(properties, "torch_facing_direction") {
		case "north", "south", "east", "west":
		case "top":
		case "unknown":
			return BlockState{}, fmt.Errorf("java state codec: native torch orientation %q has no Java block-state representation", "unknown")
		default:
			return BlockState{}, fmt.Errorf("java state codec: invalid native torch orientation %q", nativePropertyText(properties, "torch_facing_direction"))
		}
	}
	if name == "minecraft:cauldron" {
		switch nativePropertyText(properties, "cauldron_liquid") {
		case "water", "powder_snow", "lava", "empty", "":
		default:
			return BlockState{}, fmt.Errorf("java state codec: unsupported native cauldron liquid %q", nativePropertyText(properties, "cauldron_liquid"))
		}
	}
	javaProps, err := javaProperties(name, javaName, properties)
	if err != nil {
		return BlockState{}, err
	}
	javaName = typeName
	if strings.HasPrefix(name, "minecraft:cave_vines") {
		javaProps["berries"] = strings.HasSuffix(name, "with_berries")
	}
	if name == "minecraft:daylight_detector_inverted" {
		javaProps["inverted"] = true
	} else if name == "minecraft:daylight_detector" {
		javaProps["inverted"] = false
	}
	if strings.HasPrefix(name, "minecraft:light_block_") {
		level, parseErr := strconv.Atoi(strings.TrimPrefix(name, "minecraft:light_block_"))
		if parseErr != nil || level < 0 || level > 15 {
			return BlockState{}, fmt.Errorf("java state codec: invalid native light block name %q", name)
		}
		javaProps["level"] = int32(level)
	}
	liquidName, liquidProps, ok := registry.RuntimeIDToState(state.Liquid)
	if !ok {
		return BlockState{}, fmt.Errorf("java state codec: invalid native liquid runtime ID %d", state.Liquid)
	}
	liquidClear := state.Liquid == registry.AirRuntimeID()
	if !liquidClear && liquidName != "minecraft:water" && liquidName != "minecraft:flowing_water" {
		return BlockState{}, fmt.Errorf("java state codec: secondary layer %s%v is not clear or water", liquidName, liquidProps)
	}
	if !liquidClear && hasJavaWaterloggedProperty(javaName) {
		javaProps["waterlogged"] = true
	} else if liquidClear && hasJavaWaterloggedProperty(javaName) {
		javaProps["waterlogged"] = false
	} else if !liquidClear && !isIntrinsicWaterBlock(javaName) {
		return BlockState{}, fmt.Errorf("java state codec: secondary water layer is not valid for native block %s", name)
	}
	switch name {
	case "minecraft:furnace", "minecraft:smoker", "minecraft:blast_furnace", "minecraft:redstone_lamp", "minecraft:redstone_ore", "minecraft:deepslate_redstone_ore":
		javaProps["lit"] = false
	case "minecraft:redstone_torch", "minecraft:lit_redstone_torch":
		javaProps["lit"] = true
	case "minecraft:unlit_redstone_torch":
		javaProps["lit"] = false
	case "minecraft:lit_redstone_lamp", "minecraft:lit_redstone_ore", "minecraft:lit_deepslate_redstone_ore":
		javaProps["lit"] = true
	}
	if name == "minecraft:lit_furnace" || name == "minecraft:lit_smoker" || name == "minecraft:lit_blast_furnace" {
		javaProps["lit"] = true
	}
	if strings.HasPrefix(name, "minecraft:powered_") || strings.HasPrefix(name, "minecraft:unpowered_") {
		javaProps["powered"] = strings.HasPrefix(name, "minecraft:powered_")
	}
	if name == "minecraft:seagrass" {
		switch nativePropertyText(properties, "sea_grass_type") {
		case "double_top":
			javaProps["half"] = "upper"
		case "double_bot":
			javaProps["half"] = "lower"
		}
		delete(javaProps, "sea_grass_type")
	}
	if isNativeDoubleSlab(name) {
		javaProps["type"] = "double"
		delete(javaProps, "half")
	}
	if name == "minecraft:red_flower" {
		flowerType := nativePropertyText(properties, "red_flower_type", "flower_type", "type")
		if _, found := nativeRedFlowerJavaTypes[flowerType]; !found {
			return BlockState{}, fmt.Errorf("java state codec: unsupported native red flower type %q", flowerType)
		}
	}
	if name == "minecraft:double_plant" {
		typ := nativePropertyText(properties, "double_plant_type", "tall_grass_type", "type")
		if _, found := nativeDoublePlantJavaTypes[typ]; !found {
			return BlockState{}, fmt.Errorf("java state codec: unsupported native double plant type %q", typ)
		}
		if upper, _ := boolProperty(properties["upper_block_bit"]); upper {
			javaProps["half"] = "upper"
		} else {
			javaProps["half"] = "lower"
		}
	}
	if name == "minecraft:big_dripleaf" {
		delete(javaProps, "big_dripleaf_head")
	}
	if javaName == "" {
		return BlockState{}, fmt.Errorf("java state codec: no Java alias for native block %q", name)
	}
	return BlockState{Name: javaName, Properties: javaProps}, nil
}

func jigsawOrientationParts(orientation string) (front string, rotation int32, ok bool) {
	switch orientation {
	case "north_up":
		return "north", 0, true
	case "south_up":
		return "south", 0, true
	case "west_up":
		return "west", 0, true
	case "east_up":
		return "east", 0, true
	case "up_north":
		return "up", 0, true
	case "up_east":
		return "up", 1, true
	case "up_south":
		return "up", 2, true
	case "up_west":
		return "up", 3, true
	case "down_north":
		return "down", 0, true
	case "down_west":
		return "down", 1, true
	case "down_south":
		return "down", 2, true
	case "down_east":
		return "down", 3, true
	default:
		return "", 0, false
	}
}

func nativeJigsawOrientation(properties map[string]any) (string, error) {
	front, ok := nativeFacing("facing_direction", properties["facing_direction"])
	if !ok {
		return "", fmt.Errorf("java state codec: unsupported native jigsaw facing_direction %v", properties["facing_direction"])
	}
	rotation, ok := intProperty(properties["rotation"])
	if !ok || rotation < 0 || rotation > 3 {
		return "", fmt.Errorf("java state codec: invalid native jigsaw rotation %v", properties["rotation"])
	}
	if front != "up" && front != "down" {
		if rotation != 0 {
			return "", fmt.Errorf("java state codec: native jigsaw orientation front=%s rotation=%d has no Java FrontAndTop state", front, rotation)
		}
		return front + "_up", nil
	}
	if front == "up" {
		top := [...]string{"north", "east", "south", "west"}[rotation]
		return "up_" + top, nil
	}
	top := [...]string{"north", "west", "south", "east"}[rotation]
	return "down_" + top, nil
}

func javaProperties(nativeName, javaName string, nativeProps map[string]any) (map[string]any, error) {
	if strings.HasSuffix(nativeName, "_hanging_sign") {
		return javaHangingSignProperties(nativeName, nativeProps)
	}
	out := make(map[string]any, len(nativeProps)+2)
	for key, value := range nativeProps {
		switch {
		case key == "minecraft:cardinal_direction" || key == "facing_direction" || key == "minecraft:facing_direction":
			if (nativeName == "minecraft:melon_stem" && javaName == "minecraft:melon_stem") || (nativeName == "minecraft:pumpkin_stem" && javaName == "minecraft:pumpkin_stem") {
				break
			}
			facing, ok := nativeFacing(key, value)
			if !ok {
				return nil, fmt.Errorf("java state codec: cannot reverse native facing %s=%v on %s", key, value, nativeName)
			}
			if isPistonFamily(javaName) {
				facing = pistonNativeHorizontal(facing)
			}
			if javaName != "minecraft:jigsaw" {
				if strings.HasSuffix(javaName, "_door") {
					switch facing {
					case "east":
						facing = "north"
					case "west":
						facing = "south"
					case "north":
						facing = "west"
					case "south":
						facing = "east"
					}
				}
				if isSkull(javaName) && facing == "up" {
					out["rotation"] = int32(0)
				} else {
					out["facing"] = facing
				}
			}
		case key == "direction":
			if isTrapdoor(javaName) {
				if facing, ok := nativeTrapdoorFacing(value); ok {
					out["facing"] = facing
				}
			} else if isSign(nativeName) || isRail(nativeName) {
				break
			} else if javaName == "minecraft:bee_nest" || javaName == "minecraft:beehive" {
				if facing, ok := nativeDirection(value); ok {
					out["facing"] = facing
				} else {
					return nil, fmt.Errorf("java state codec: unsupported native bee direction %v", value)
				}
			} else if facing, ok := nativeDirection(value); ok {
				out["facing"] = facing
			}
		case key == "ground_sign_direction":
			if isStandingSign(nativeName) || isBanner(nativeName) || isSkull(nativeName) {
				out["rotation"] = intPropertyValue(value)
			}
		case key == "rotation":
			if javaName != "minecraft:jigsaw" {
				out["rotation"] = intPropertyValue(value)
			}
		case key == "pillar_axis":
			out["axis"] = value
		case key == "portal_axis":
			axis, ok := stringProperty(value)
			if !ok || axis != "x" && axis != "z" {
				return nil, fmt.Errorf("java state codec: native portal axis %v has no Java block-state representation", value)
			}
			out["axis"] = axis
		case key == "end_portal_eye_bit":
			out["eye"] = nativeBoolValue(value)
		case key == "minecraft:vertical_half":
			if isNativeDoubleSlab(nativeName) {
				out["type"] = "double"
			} else if strings.HasSuffix(javaName, "_slab") {
				out["type"] = value
			} else {
				out["half"] = value
			}
		case key == "upside_down_bit":
			if b, ok := boolProperty(value); ok {
				if b {
					out["half"] = "top"
				} else {
					out["half"] = "bottom"
				}
			}
		case key == "upper_block_bit":
			if javaName == "minecraft:bed" {
				if b, ok := boolProperty(value); ok {
					if b {
						out["part"] = "head"
					} else {
						out["part"] = "foot"
					}
				}
			} else if javaName == "minecraft:pale_moss_carpet" {
				out["bottom"] = !nativeBoolValue(value)
			} else if b, ok := boolProperty(value); ok {
				if b {
					out["half"] = "upper"
				} else {
					out["half"] = "lower"
				}
			}
		case key == "door_hinge_bit":
			if b, ok := boolProperty(value); ok {
				if b {
					out["hinge"] = "right"
				} else {
					out["hinge"] = "left"
				}
			}
		case key == "rail_direction":
			if shape, ok := nativeRailShape(value); ok {
				out["shape"] = shape
			}
		case key == "rail_data_bit":
			if b, ok := boolProperty(value); ok {
				out["powered"] = b
			}
		case key == "minecraft:corner":
			if text, ok := stringProperty(value); ok {
				if text == "none" {
					text = "straight"
				}
				out["shape"] = text
			}
		case key == "weirdo_direction":
			if facing, ok := nativeStairFacing(value); ok {
				out["facing"] = facing
			}
		case key == "persistent_bit":
			if b, ok := boolProperty(value); ok {
				out["persistent"] = b
			}
		case key == "update_bit":
			// Bedrock's update scheduling flag and flower-pot refresh flag are not Java state.
		case key == "covered_bit":
			// Java snow-layer geometry has no equivalent covered flag.
		case key == "moisturized_amount":
			if javaName == "minecraft:farmland" {
				out["moisture"] = intPropertyValue(value)
			}

		case key == "twisting_vines_age" || key == "weeping_vines_age" || key == "propagule_stage":
			out["age"] = intPropertyValue(value)
		case key == "growing_plant_age":
			if !strings.HasPrefix(javaName, "minecraft:cave_vines") {
				return nil, fmt.Errorf("java state codec: unsupported native growing_plant_age on %s", nativeName)
			}
			if nativeName != "minecraft:cave_vines_body_with_berries" {
				out["age"] = intPropertyValue(value)
			}
		case key == "growth":
			if javaName == "minecraft:leaf_litter" {
				growth := intPropertyValue(value)
				if growth < 0 || growth > 3 {
					return nil, fmt.Errorf("java state codec: native leaf litter growth %d exceeds Java segment count", growth)
				}
				out["segment_amount"] = growth + 1
			} else if javaName == "minecraft:pink_petals" || javaName == "minecraft:wildflowers" {
				out["flower_amount"] = intPropertyValue(value) + 1
			} else if javaName == "minecraft:attached_melon_stem" || javaName == "minecraft:attached_pumpkin_stem" {
				// Attached stem age is represented by its block type.
			} else {
				out["age"] = intPropertyValue(value)
			}
		case key == "candles":
			if javaName == "minecraft:candle" || strings.HasSuffix(javaName, "_candle") {
				out["candles"] = intPropertyValue(value) + 1
			}
		case key == "structure_block_type":
			mode, ok := stringProperty(value)
			if !ok {
				return nil, fmt.Errorf("java state codec: invalid native structure block type %v", value)
			}
			switch mode {
			case "data", "save", "load", "corner":
				out["mode"] = mode
			default:
				return nil, fmt.Errorf("java state codec: unsupported native structure block type %q", mode)
			}
		case key == "lit":
			out["lit"] = nativeBoolValue(value)
		case key == "extinguished":
			if isCampfire(javaName) {
				out["lit"] = !nativeBoolValue(value)
			}
		case key == "signal_fire":
			// Java recalculates this property from the block below.
		case key == "books_stored":
			if javaName == "minecraft:chiseled_bookshelf" {
				mask, ok := intProperty(value)
				if !ok || mask < 0 || mask > 63 {
					return nil, fmt.Errorf("java state codec: invalid native bookshelf mask %v", value)
				}
				for slot := range 6 {
					out[fmt.Sprintf("slot_%d_occupied", slot)] = mask&(1<<slot) != 0
				}
			}
		case key == "powered_shelf_type":
			if isShelf(javaName) {
				switch intPropertyValue(value) {
				case 0:
					out["side_chain"] = "unconnected"
				case 1:
					out["side_chain"] = "right"
				case 2:
					out["side_chain"] = "center"
				case 3:
					out["side_chain"] = "left"
				default:
					return nil, fmt.Errorf("java state codec: invalid native shelf type %v", value)
				}
			}
		case key == "brewing_stand_slot_a_bit":
			out["has_bottle_0"] = nativeBoolValue(value)
		case key == "brewing_stand_slot_b_bit":
			out["has_bottle_1"] = nativeBoolValue(value)
		case key == "brewing_stand_slot_c_bit":
			out["has_bottle_2"] = nativeBoolValue(value)
		case key == "repeater_delay":
			delay := intPropertyValue(value)
			if delay < 0 || delay > 3 {
				return nil, fmt.Errorf("java state codec: invalid native repeater delay %v", value)
			}
			out["delay"] = delay + 1
		case key == "huge_mushroom_bits":
			if isMushroomBlock(javaName) {
				variant := intPropertyValue(value)
				if variant < 0 || variant > 15 {
					return nil, fmt.Errorf("java state codec: invalid native mushroom variant %v", value)
				}
				for bit, side := range [...]string{"north", "east", "south", "west"} {
					out[side] = variant&(1<<bit) != 0
				}
				out["up"], out["down"] = false, false
			}
		case key == "vine_direction_bits":
			variant := intPropertyValue(value)
			if variant < 0 || variant > 15 {
				return nil, fmt.Errorf("java state codec: invalid native vine direction bits %v", value)
			}
			for bit, side := range [...]string{"south", "west", "north", "east"} {
				out[side] = variant&(1<<bit) != 0
			}
			out["up"] = false
		case key == "growing_plant_age" || key == "suspended_bit":
			// Bedrock stores transient growth/attachment metadata not represented by Java state.
		case key == "explode_bit":
			if javaName == "minecraft:tnt" {
				out["unstable"] = nativeBoolValue(value)
			}
		case key == "disarmed_bit":
			out["disarmed"] = nativeBoolValue(value)
		case key == "enabled_bit":
			out["enabled"] = nativeBoolValue(value)
		case key == "conditional_bit":
			out["conditional"] = nativeBoolValue(value)
		case key == "triggered_bit":
			out["triggered"] = nativeBoolValue(value)
		case key == "extended_bit":
			out["extended"] = nativeBoolValue(value)
		case key == "occupied_bit":
			out["occupied"] = nativeBoolValue(value)
		case key == "berry":
			out["berries"] = nativeBoolValue(value)
		case key == "bite_counter":
			out["bites"] = intPropertyValue(value)
		case key == "can_summon":
			out["can_summon"] = nativeBoolValue(value)
		case key == "natural":
			out["natural"] = nativeBoolValue(value)
		case key == "ominous":
			out["ominous"] = nativeBoolValue(value)
		case key == "respawn_anchor_charge":
			out["charges"] = intPropertyValue(value)
		case key == "turtle_egg_count":
			count, ok := stringProperty(value)
			if !ok {
				return nil, fmt.Errorf("java state codec: invalid native turtle egg count %v", value)
			}
			switch count {
			case "one_egg":
				out["eggs"] = int32(1)
			case "two_egg":
				out["eggs"] = int32(2)
			case "three_egg":
				out["eggs"] = int32(3)
			case "four_egg":
				out["eggs"] = int32(4)
			default:
				return nil, fmt.Errorf("java state codec: unsupported native turtle egg count %q", count)
			}
		case key == "cracked_state":
			cracked, ok := stringProperty(value)
			if !ok {
				return nil, fmt.Errorf("java state codec: invalid native turtle egg crack state %v", value)
			}
			switch cracked {
			case "no_cracks":
				out["hatch"] = int32(0)
			case "cracked":
				out["hatch"] = int32(1)
			case "max_cracked":
				out["hatch"] = int32(2)
			default:
				return nil, fmt.Errorf("java state codec: unsupported native turtle egg crack state %q", cracked)
			}
		case key == "sculk_sensor_phase":
			switch intPropertyValue(value) {
			case 0:
				out["sculk_sensor_phase"] = "inactive"
			case 1:
				out["sculk_sensor_phase"] = "active"
			case 2:
				out["sculk_sensor_phase"] = "cooldown"
			default:
				return nil, fmt.Errorf("java state codec: unsupported native sculk sensor phase %v", value)
			}
		case key == "trial_spawner_state":
			switch intPropertyValue(value) {
			case 0:
				out["trial_spawner_state"] = "inactive"
			case 1:
				out["trial_spawner_state"] = "waiting_for_players"
			case 2:
				out["trial_spawner_state"] = "active"
			case 3:
				out["trial_spawner_state"] = "waiting_for_reward_ejection"
			case 4:
				out["trial_spawner_state"] = "ejecting_reward"
			case 5:
				out["trial_spawner_state"] = "cooldown"
			default:
				return nil, fmt.Errorf("java state codec: unsupported native trial spawner state %v", value)
			}
		case key == "active":
			if javaName == "minecraft:sculk_shrieker" {
				out["shrieking"] = nativeBoolValue(value)
			}
		case key == "stability":
			if javaName == "minecraft:scaffolding" {
				out["distance"] = intPropertyValue(value)
			}
		case key == "rehydration_level":
			if javaName == "minecraft:dried_ghast" {
				out["hydration"] = intPropertyValue(value)
			}
		case key == "stability_check":
			// Bedrock support-check metadata has no Java block-state counterpart.
		case key == "coral_fan_direction":
			// Bedrock distinguishes native fan orientation; Java coral fans do not.
		case key == "multi_face_direction_bits":
			if isMultiFaceBlock(javaName) {
				bits, ok := intProperty(value)
				if !ok || bits < 0 || bits > 63 {
					return nil, fmt.Errorf("java state codec: invalid native multiface mask %v", value)
				}
				for bit, side := range [...]string{"down", "up", "south", "west", "north", "east"} {
					out[side] = bits&(1<<bit) != 0
				}
			}
		case key == "bloom":
			// Bedrock's bloom animation state has no Java block-state counterpart.
		case key == "composter_fill_level":
			out["level"] = intPropertyValue(value)
		case key == "liquid_depth":
			out["level"] = intPropertyValue(value)
		case key == "age_bit":
			if b, ok := boolProperty(value); ok {
				if javaName == "minecraft:bamboo" {
					out["stage"] = int32(javaStateBoolInt(b))
				} else if strings.Contains(javaName, "sapling") {
					out["stage"] = int32(javaStateBoolInt(b))
				} else {
					out["age"] = int32(javaStateBoolInt(b))
				}
			}
		case strings.HasPrefix(key, "minecraft:connection_"):
			out[strings.TrimPrefix(key, "minecraft:connection_")] = nativeBoolValue(value)
		case strings.HasPrefix(key, "wall_connection_type_"):
			if text, ok := stringProperty(value); ok {
				side := strings.TrimPrefix(key, "wall_connection_type_")
				if isWall(javaName) {
					if text == "short" {
						text = "low"
					}
					out[side] = text
				} else if javaName == "minecraft:pale_moss_carpet" {
					out[side] = text
				} else {
					out[side] = text != "none"
				}
			}
		case strings.HasPrefix(key, "pale_moss_carpet_side_"):
			side, ok := stringProperty(value)
			if !ok {
				return nil, fmt.Errorf("java state codec: invalid pale moss carpet side %s=%v", key, value)
			}
			if side == "short" {
				side = "low"
			}
			out[strings.TrimPrefix(key, "pale_moss_carpet_side_")] = side
		case key == "head_piece_bit":
			if javaName == "minecraft:bed" || javaName == "minecraft:straw_bed" {
				if nativeBoolValue(value) {
					out["part"] = "head"
				} else {
					out["part"] = "foot"
				}
			}
		case key == "in_wall_bit":
			out["in_wall"] = nativeBoolValue(value)
		case key == "coral_direction":
			if strings.HasSuffix(javaName, "_coral_wall_fan") {
				switch intPropertyValue(value) {
				case 0:
					out["facing"] = "west"
				case 1:
					out["facing"] = "east"
				case 2:
					out["facing"] = "north"
				case 3:
					out["facing"] = "south"
				default:
					return nil, fmt.Errorf("java state codec: unsupported coral wall fan direction %v", value)
				}
			}
		case key == "wall_post_bit":
			out["up"] = nativeBoolValue(value)
		case key == "open_bit":
			if isLever(javaName) {
				out["powered"] = nativeBoolValue(value)
			} else {
				out["open"] = nativeBoolValue(value)
			}
		case key == "button_pressed_bit":
			out["powered"] = nativeBoolValue(value)
		case key == "powered_bit":
			out["powered"] = nativeBoolValue(value)
		case key == "output_lit_bit":
			out["powered"] = nativeBoolValue(value)
		case key == "output_subtract_bit":
			if nativeBoolValue(value) {
				out["mode"] = "subtract"
			} else {
				out["mode"] = "compare"
			}
		case key == "lever_direction":
			if face, facing, ok := nativeLeverDirection(value); ok {
				out["face"], out["facing"] = face, facing
			} else {
				return nil, fmt.Errorf("java state codec: unsupported native lever direction %v", value)
			}
		case key == "attachment":
			if isBell(javaName) {
				if attachment, ok := javaBellAttachment(value); ok {
					out["attachment"] = attachment
				} else {
					return nil, fmt.Errorf("java state codec: unsupported native bell attachment %v", value)
				}
			} else if isGrindstone(javaName) {
				if face, ok := javaGrindstoneFace(value); ok {
					out["face"] = face
				} else {
					return nil, fmt.Errorf("java state codec: unsupported native grindstone attachment %v", value)
				}
			} else {
				out["attachment"] = value
			}
		case key == "cluster_count":
			out["pickles"] = intPropertyValue(value) + 1
		case key == "dead_bit":
			if javaName != "minecraft:sea_pickle" {
				return nil, fmt.Errorf("java state codec: unsupported dead_bit on %s", nativeName)
			}
			// Bedrock sea-pickle state is not the Java waterlogged property; liquid uses the secondary layer.
		case key == "big_dripleaf_head":
			// Native head/stem variants are converted to Java block names after property decoding.
		case key == "big_dripleaf_tilt":
			if javaName == "minecraft:big_dripleaf_stem" {
				break
			}
			tilt, ok := stringProperty(value)
			if !ok {
				return nil, fmt.Errorf("java state codec: invalid native big dripleaf tilt %v", value)
			}
			switch tilt {
			case "partial_tilt":
				tilt = "partial"
			case "full_tilt":
				tilt = "full"
			case "none", "unstable":
			default:
				return nil, fmt.Errorf("java state codec: unsupported native big dripleaf tilt %q", tilt)
			}
			out["tilt"] = tilt
		case key == "bamboo_leaf_size":
			leaves, ok := stringProperty(value)
			if !ok {
				return nil, fmt.Errorf("java state codec: invalid native bamboo leaf size %v", value)
			}
			switch leaves {
			case "no_leaves":
				out["leaves"] = "none"
			case "small_leaves":
				out["leaves"] = "small"
			case "large_leaves":
				out["leaves"] = "large"
			default:
				return nil, fmt.Errorf("java state codec: unsupported bamboo leaf size %q", leaves)
			}
		case key == "bamboo_stalk_thickness":
			// Bedrock's thin/thick stalk distinction has no Java block-state property.
		case key == "redstone_signal":
			if isRedstoneWire(javaName) {
				out["power"] = intPropertyValue(value)
			} else if strings.HasSuffix(javaName, "_pressure_plate") {
				out["powered"] = intPropertyValue(value) > 0
			} else {
				out["power"] = intPropertyValue(value)
			}
		case key == "torch_facing_direction":
			if text, ok := stringProperty(value); ok {
				switch text {
				case "north":
					out["facing"] = "south"
				case "south":
					out["facing"] = "north"
				case "east":
					out["facing"] = "west"
				case "west":
					out["facing"] = "east"
				case "top":
				default:
					return nil, fmt.Errorf("java state codec: unsupported native torch facing %q", text)
				}
			}
		case key == "minecraft:block_face":
			out["facing"] = value
		case key == "dripstone_thickness":
			if javaName == "minecraft:pointed_dripstone" || javaName == "minecraft:sulfur_spike" {
				thickness, ok := stringProperty(value)
				if !ok {
					return nil, fmt.Errorf("java state codec: invalid native dripstone thickness %v", value)
				}
				if thickness == "merge" {
					thickness = "tip_merge"
				}
				out["thickness"] = thickness
			}
		case key == "hanging":
			if javaName == "minecraft:pointed_dripstone" || javaName == "minecraft:sulfur_spike" {
				if nativeBoolValue(value) {
					out["vertical_direction"] = "down"
				} else {
					out["vertical_direction"] = "up"
				}
			} else if javaName != "minecraft:suspicious_sand" && javaName != "minecraft:suspicious_gravel" {
				out["hanging"] = nativeBoolValue(value)
			}
		case key == "brushed_progress":
			out["dusted"] = intPropertyValue(value)
		case key == "attached_bit":
			out["attached"] = nativeBoolValue(value)
		case key == "cauldron_liquid":
			// The native liquid variant selects the Java cauldron block name.
		case key == "fill_level":
			if javaName == "minecraft:water_cauldron" || javaName == "minecraft:powder_snow_cauldron" {
				out["level"] = intPropertyValue(value)/2 + intPropertyValue(value)%2
			}
		case key == "toggle_bit":
			if javaName == "minecraft:hopper" {
				out["enabled"] = !nativeBoolValue(value)
			} else if javaName == "minecraft:bell" {
				out["powered"] = nativeBoolValue(value)
			}
		case key == "height":
			out["layers"] = intPropertyValue(value) + 1
		case key == "honey_level":
			out["honey_level"] = value
		case key == "drag_down" && javaName == "minecraft:bubble_column":
			out["drag"] = nativeBoolValue(value)
		case key == "red_flower_type" || key == "flower_type" || key == "double_plant_type" || key == "tall_grass_type":
		case key == "kelp_age":
			out["age"] = intPropertyValue(value)
		case key == "sea_grass_type":
			// Converted to Java seagrass/tall_seagrass by the block-family adapter.
		case key == "infiniburn_bit":
			// Bedrock combustion metadata has no Java block-state counterpart.
			// Handled by the block-name alias after all properties are read.
		default:
			if strings.HasPrefix(key, "minecraft:") {
				// Native-only metadata is not automatically discarded. It is accepted only for
				// the documented family metadata below; other keys must be mapped explicitly.
				if isKnownNativeMetadata(javaName, key) {
					continue
				}
				return nil, fmt.Errorf("java state codec: unhandled native property %q=%v on %s", key, value, nativeName)
			}
			out[key] = value
		}
	}
	if javaName == "minecraft:jigsaw" {
		orientation, err := nativeJigsawOrientation(nativeProps)
		if err != nil {
			return nil, err
		}
		out["orientation"] = orientation
	}
	if javaName == "minecraft:piston_head" {
		if nativeName == "minecraft:sticky_piston_arm_collision" {
			out["type"] = "sticky"
		} else {
			out["type"] = "normal"
		}
	}
	if isRepeater(javaName) {
		out["locked"] = false
	}
	if javaName == "minecraft:piston" || javaName == "minecraft:sticky_piston" {
		out["extended"] = false
	}
	if javaName == "minecraft:piston_head" {
		out["short"] = false
	}
	if isSkull(javaName) {
		out["powered"] = false
	}
	if javaName == "minecraft:sculk_sensor" || javaName == "minecraft:calibrated_sculk_sensor" {
		out["power"] = int32(0)
	}
	if javaName == "minecraft:scaffolding" {
		out["bottom"] = false
	}
	if isCampfire(javaName) {
		out["signal_fire"] = false
	}
	if isRedstoneWire(javaName) {
		out["north"], out["east"], out["south"], out["west"] = "none", "none", "none", "none"
	}
	if isButton(javaName) {
		switch out["facing"] {
		case "up":
			out["face"], out["facing"] = "floor", "north"
		case "down":
			out["face"], out["facing"] = "ceiling", "north"
		case "north", "south", "east", "west":
			out["face"] = "wall"
		}
	}
	if isLeafBlock(javaName) {
		if _, exists := out["persistent"]; !exists {
			out["persistent"] = false
		}
		// Bedrock has no distance property: Java derives distance from logs within
		// seven steps during block-state updates. Standalone native snapshots expose 7.
		if _, exists := out["distance"]; !exists {
			out["distance"] = int32(7)
		}
	}
	if javaName == "minecraft:target" {
		out["power"] = int32(0)
	}
	if isSnowyGroundBlock(javaName) {
		out["snowy"] = false
	}
	return out, nil
}

func isKnownNativeMetadata(javaName, key string) bool {
	if isLeafBlock(javaName) && (key == "update_bit" || key == "persistent_bit") {
		return true
	}
	if (isConnectionBlock(javaName) || isWall(javaName)) && key == "minecraft:corner" {
		return true
	}
	if isBanner(javaName) && key == "ground_sign_direction" {
		return true
	}
	if key == "minecraft:vertical_half" || key == "minecraft:block_face" {
		return true
	}
	if strings.HasPrefix(key, "minecraft:connection_") || strings.HasPrefix(key, "wall_connection_type_") {
		return true
	}
	return false
}
func javaNameForNative(name string) string {
	if javaName := nativeToJavaBlockNames[name]; javaName != "" {
		return javaName
	}
	switch name {
	case "minecraft:standing_sign":
		return "minecraft:oak_sign"
	case "minecraft:wall_sign":
		return "minecraft:oak_wall_sign"
	case "minecraft:golden_rail":
		return "minecraft:powered_rail"
	case "minecraft:powered_repeater", "minecraft:unpowered_repeater":
		return "minecraft:repeater"
	case "minecraft:powered_comparator", "minecraft:unpowered_comparator":
		return "minecraft:comparator"
	case "minecraft:lit_redstone_torch", "minecraft:unlit_redstone_torch":
		return "minecraft:redstone_torch"
	case "minecraft:lit_redstone_lamp":
		return "minecraft:redstone_lamp"
	case "minecraft:lit_redstone_ore":
		return "minecraft:redstone_ore"
	case "minecraft:lit_deepslate_redstone_ore":
		return "minecraft:deepslate_redstone_ore"
	}
	if strings.HasSuffix(name, "_standing_sign") {
		wood := strings.TrimSuffix(name, "_standing_sign")
		if wood == "minecraft:darkoak" {
			wood = "minecraft:dark_oak"
		}
		return wood + "_sign"
	}
	if strings.HasSuffix(name, "double_cut_copper_slab") {
		return strings.TrimSuffix(name, "double_cut_copper_slab") + "cut_copper_slab"
	}
	if strings.HasSuffix(name, "_double_slab") {
		baseName := strings.TrimSuffix(name, "_double_slab") + "_slab"
		if javaName := nativeToJavaBlockNames[baseName]; javaName != "" {
			return javaName
		}
		return baseName
	}
	if strings.HasPrefix(name, "minecraft:light_block_") {
		return "minecraft:light"
	}
	return name
}

func isNativeDoubleSlab(name string) bool {
	return strings.HasSuffix(name, "_double_slab") || strings.HasSuffix(name, "double_cut_copper_slab")
}

// javaHangingSignProperties ignores inactive native orientation fields. Wall
// signs use facing_direction; ceiling signs use either a cardinal facing or
// the sixteen-step ground rotation.
func javaHangingSignProperties(name string, properties map[string]any) (map[string]any, error) {
	if !nativeBoolValue(properties["hanging"]) {
		facing, ok := nativeFacing("facing_direction", properties["facing_direction"])
		if !ok || facing == "up" || facing == "down" {
			return nil, fmt.Errorf("java state codec: native wall hanging sign %s has non-horizontal facing %v", name, properties["facing_direction"])
		}
		return map[string]any{"facing": facing}, nil
	}
	attached := nativeBoolValue(properties["attached_bit"])
	rotation := intPropertyValue(properties["ground_sign_direction"])
	facing := intPropertyValue(properties["facing_direction"])
	if !attached && facing >= 2 && facing <= 5 {
		rotation = [...]int32{8, 0, 4, 12}[facing-2]
	}
	return map[string]any{"attached": attached, "rotation": rotation}, nil
}

// javaBlockTypeForNative selects the Java block type represented by a native
// block state. The returned type may depend on native properties even when
// those properties do not exist in Java's state model.
func javaBlockTypeForNative(name string, properties map[string]any) string {
	javaName := javaNameForNative(name)
	if strings.HasSuffix(name, "_hanging_sign") && !nativeBoolValue(properties["hanging"]) {
		return strings.TrimSuffix(javaName, "_hanging_sign") + "_wall_hanging_sign"
	}
	if isSkull(name) {
		facing := intPropertyValue(properties["facing_direction"])
		if facing >= 2 && facing <= 5 {
			if strings.HasSuffix(name, "_skull") {
				return strings.TrimSuffix(name, "_skull") + "_wall_skull"
			}
			if strings.HasSuffix(name, "_head") {
				return strings.TrimSuffix(name, "_head") + "_wall_head"
			}
		}
	}
	switch name {
	case "minecraft:torch", "minecraft:soul_torch", "minecraft:copper_torch", "minecraft:redstone_torch", "minecraft:unlit_redstone_torch", "minecraft:lit_redstone_torch":
		switch nativePropertyText(properties, "torch_facing_direction") {
		case "north", "south", "east", "west":
			switch {
			case strings.Contains(name, "redstone"):
				return "minecraft:redstone_wall_torch"
			case name == "minecraft:soul_torch":
				return "minecraft:soul_wall_torch"
			case name == "minecraft:copper_torch":
				return "minecraft:copper_wall_torch"
			default:
				return "minecraft:wall_torch"
			}
		}
	case "minecraft:cauldron":
		level := intPropertyValue(properties["fill_level"])
		switch nativePropertyText(properties, "cauldron_liquid") {
		case "water":
			if level > 0 {
				return "minecraft:water_cauldron"
			}
		case "powder_snow":
			if level > 0 {
				return "minecraft:powder_snow_cauldron"
			}
		case "lava":
			if level > 0 {
				return "minecraft:lava_cauldron"
			}
		}
	case "minecraft:seagrass":
		switch nativePropertyText(properties, "sea_grass_type") {
		case "double_top", "double_bot":
			return "minecraft:tall_seagrass"
		}
	case "minecraft:red_flower":
		if javaName, ok := nativeRedFlowerJavaTypes[nativePropertyText(properties, "red_flower_type", "flower_type", "type")]; ok {
			return javaName
		}
	case "minecraft:yellow_flower":
		return "minecraft:dandelion"
	case "minecraft:double_plant":
		if javaName, ok := nativeDoublePlantJavaTypes[nativePropertyText(properties, "double_plant_type", "tall_grass_type", "type")]; ok {
			return javaName
		}
	case "minecraft:big_dripleaf":
		if head, _ := boolProperty(properties["big_dripleaf_head"]); head {
			return "minecraft:big_dripleaf"
		}
		return "minecraft:big_dripleaf_stem"
	}
	if strings.HasPrefix(name, "minecraft:cave_vines") {
		if !strings.HasSuffix(name, "body_with_berries") {
			return "minecraft:cave_vines"
		}
		return "minecraft:cave_vines_plant"
	}
	if strings.HasPrefix(name, "minecraft:light_block_") {
		return "minecraft:light"
	}
	return javaName
}

func nativePlantStemJavaType(name string, properties map[string]any) string {
	if intPropertyValue(properties["growth"]) == 7 {
		facing, ok := nativeFacing("facing_direction", properties["facing_direction"])
		if ok && facing != "up" && facing != "down" {
			return "minecraft:attached_" + strings.TrimPrefix(name, "minecraft:")
		}
	}
	return name
}

func NormaliseBlockName(name string) string {
	if name == "" {
		return ""
	}
	if !strings.Contains(name, ":") {
		return "minecraft:" + name
	}
	return name
}

func valueForSchema(value, schemaValue any) any {
	switch schemaValue.(type) {
	case bool:
		if b, ok := boolProperty(value); ok {
			return b
		}
	case uint8:
		if b, ok := boolProperty(value); ok {
			if b {
				return uint8(1)
			}
			return uint8(0)
		}
		if n, ok := intProperty(value); ok {
			return uint8(n)
		}
	case int32:
		if b, ok := boolProperty(value); ok {
			return int32(javaStateBoolInt(b))
		}
		if n, ok := intProperty(value); ok {
			return int32(n)
		}
	case int:
		if n, ok := intProperty(value); ok {
			return int(n)
		}
	case string:
		if text, ok := stringProperty(value); ok {
			return text
		}
	}
	return value
}

func nativeBool(b bool, schema any) any {
	if _, ok := schema.(uint8); ok {
		if b {
			return uint8(1)
		}
		return uint8(0)
	}
	return b
}

func boolProperty(value any) (bool, bool) {
	switch value := value.(type) {
	case bool:
		return value, true
	case uint8:
		return value != 0, true
	case int8:
		return value != 0, true
	case int32:
		if value == 0 || value == 1 {
			return value == 1, true
		}
	case int:
		if value == 0 || value == 1 {
			return value == 1, true
		}
	case string:
		if value == "true" {
			return true, true
		}
		if value == "false" {
			return false, true
		}
	}
	return false, false
}

func stringProperty(value any) (string, bool) {
	text, ok := value.(string)
	if ok {
		return text, true
	}
	return "", false
}

func intProperty(value any) (int, bool) {
	switch value := value.(type) {
	case int:
		return value, true
	case int8:
		return int(value), true
	case int16:
		return int(value), true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case uint8:
		return int(value), true
	case uint16:
		return int(value), true
	case uint32:
		return int(value), true
	case string:
		n, err := strconv.Atoi(value)
		return n, err == nil
	}
	return 0, false
}

func javaStateBoolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func intPropertyValue(value any) int32 {
	n, _ := intProperty(value)
	return int32(n)
}

func nativeBoolValue(value any) bool {
	b, _ := boolProperty(value)
	return b
}

func convertJavaFacing(target string, value any, javaName string) (any, error) {
	facing, ok := stringProperty(value)
	if !ok {
		return nil, fmt.Errorf("java state codec: invalid facing %v on %s", value, javaName)
	}
	if target == "torch_facing_direction" {
		switch facing {
		case "up":
			return "top", nil
		case "north":
			return "south", nil
		case "south":
			return "north", nil
		case "east":
			return "west", nil
		case "west":
			return "east", nil
		default:
			return nil, fmt.Errorf("java state codec: invalid torch facing %q on %s", facing, javaName)
		}
	}
	if target == "minecraft:block_face" || target == "minecraft:cardinal_direction" ||
		target == "minecraft:facing_direction" && !isHorizontalOnly(javaName) {
		return facing, nil
	}
	if target == "facing_direction" || target == "minecraft:facing_direction" {
		if isPistonFamily(javaName) {
			facing = pistonNativeHorizontal(facing)
		}
		switch facing {
		case "down":
			return int32(0), nil
		case "up":
			return int32(1), nil
		case "north":
			return int32(2), nil
		case "south":
			return int32(3), nil
		case "west":
			return int32(4), nil
		case "east":
			return int32(5), nil
		}
	}
	if target == "direction" {
		switch facing {
		case "south":
			return int32(0), nil
		case "west":
			return int32(1), nil
		case "north":
			return int32(2), nil
		case "east":
			return int32(3), nil
		}
	}
	if target == "weirdo_direction" {
		switch facing {
		case "east":
			return int32(0), nil
		case "west":
			return int32(1), nil
		case "south":
			return int32(2), nil
		case "north":
			return int32(3), nil
		}
	}
	return nil, fmt.Errorf("java state codec: facing %q cannot map to native property %q on %s", facing, target, javaName)
}

func nativeFacing(key string, value any) (string, bool) {
	if text, ok := stringProperty(value); ok {
		return text, true
	}
	n, ok := intProperty(value)
	if !ok {
		return "", false
	}
	if key == "minecraft:cardinal_direction" {
		return "", false
	}
	if key == "facing_direction" || key == "minecraft:facing_direction" {
		directions := [...]string{"down", "up", "north", "south", "west", "east"}
		if n >= 0 && n < len(directions) {
			return directions[n], true
		}
	}
	return "", false
}

// isPistonFamily reports whether a Java block is a piston or its head, whose
// Bedrock facing_direction numbers the horizontal directions the other way
// round: on BDS, facing_direction 4 pushes east and 5 west.
func isPistonFamily(javaName string) bool {
	return javaName == "minecraft:piston" || javaName == "minecraft:sticky_piston" || javaName == "minecraft:piston_head"
}

// pistonNativeHorizontal swaps a horizontal facing between Java's direction
// and the one Bedrock's piston facing_direction number names.
func pistonNativeHorizontal(facing string) string {
	switch facing {
	case "north":
		return "south"
	case "south":
		return "north"
	case "east":
		return "west"
	case "west":
		return "east"
	}
	return facing
}

func nativeDirection(value any) (string, bool) {
	n, ok := intProperty(value)
	if !ok {
		return "", false
	}
	directions := [...]string{"south", "west", "north", "east"}
	if n >= 0 && n < len(directions) {
		return directions[n], true
	}
	return "", false
}

func nativeStairFacing(value any) (string, bool) {
	n, ok := intProperty(value)
	if !ok {
		return "", false
	}
	directions := [...]string{"east", "west", "south", "north"}
	if n >= 0 && n < len(directions) {
		return directions[n], true
	}
	return "", false
}

func javaRailDirection(shape string) (int, bool) {
	switch shape {
	case "north_south":
		return 0, true
	case "east_west":
		return 1, true
	case "ascending_east":
		return 2, true
	case "ascending_west":
		return 3, true
	case "ascending_north":
		return 4, true
	case "ascending_south":
		return 5, true
	case "south_east":
		return 6, true
	case "south_west":
		return 7, true
	case "north_west":
		return 8, true
	case "north_east":
		return 9, true
	default:
		return 0, false
	}
}

func nativeRailShape(value any) (string, bool) {
	n, ok := intProperty(value)
	if !ok {
		return "", false
	}
	shapes := [...]string{"north_south", "east_west", "ascending_east", "ascending_west", "ascending_north", "ascending_south", "south_east", "south_west", "north_west", "north_east"}
	if n >= 0 && n < len(shapes) {
		return shapes[n], true
	}
	return "", false
}

func needsWaterLayer(name string, props map[string]any) bool {
	if waterlogged, ok := boolProperty(props["waterlogged"]); ok && waterlogged {
		return true
	}
	return isIntrinsicWaterBlock(name)
}

func hasJavaWaterloggedProperty(name string) bool {
	return gamedata.BlockHasProperty(name, "waterlogged")
}

func isLeafBlock(name string) bool {
	return strings.HasSuffix(name, "_leaves") || strings.HasSuffix(name, "_leaves_flowered") || name == "minecraft:azalea_leaves"
}

func isRail(name string) bool {
	return strings.HasSuffix(name, "_rail") || name == "minecraft:rail"
}

func isWall(name string) bool { return strings.HasSuffix(name, "_wall") }
func isSnowyGroundBlock(name string) bool {
	return name == "minecraft:grass_block" || name == "minecraft:podzol" || name == "minecraft:mycelium" || name == "minecraft:dirt_path"
}
func isRedstoneWire(name string) bool { return name == "minecraft:redstone_wire" }
func isMushroomBlock(name string) bool {
	return name == "minecraft:brown_mushroom_block" || name == "minecraft:red_mushroom_block" || name == "minecraft:mushroom_stem"
}
func isMultiFaceBlock(name string) bool {
	return name == "minecraft:glow_lichen" || name == "minecraft:resin_clump" || name == "minecraft:sculk_vein"
}

func isMultiFaceProperty(name string) bool {
	switch name {
	case "down", "up", "north", "south", "west", "east":
		return true
	}
	return false
}

func javaMultiFaceBits(properties map[string]any) (int, error) {
	bits := 0
	for bit, side := range [...]string{"down", "up", "south", "west", "north", "east"} {
		value, exists := properties[side]
		if !exists {
			continue
		}
		attached, ok := boolProperty(value)
		if !ok {
			return 0, fmt.Errorf("java state codec: invalid multiface side %s=%v", side, value)
		}
		if attached {
			bits |= 1 << bit
		}
	}
	return bits, nil
}

func mushroomVariant(properties map[string]any) (int, error) {
	variant := 0
	for bit, key := range [...]string{"north", "east", "south", "west"} {
		value, exists := properties[key]
		if !exists {
			continue
		}
		side, ok := boolProperty(value)
		if !ok {
			return 0, fmt.Errorf("java state codec: invalid mushroom side %s=%v", key, value)
		}
		if side {
			variant |= 1 << bit
		}
	}
	return variant, nil
}

func occupiedSlotIndex(key string) (int, bool) {
	if !strings.HasPrefix(key, "slot_") || !strings.HasSuffix(key, "_occupied") {
		return 0, false
	}
	index, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(key, "slot_"), "_occupied"))
	return index, err == nil
}

func occupiedSlotMask(properties map[string]any, slots int) (int, error) {
	mask := 0
	for index := range slots {
		value, exists := properties[fmt.Sprintf("slot_%d_occupied", index)]
		if !exists {
			continue
		}
		occupied, ok := boolProperty(value)
		if !ok {
			return 0, fmt.Errorf("java state codec: invalid bookshelf slot %d occupancy %v", index, value)
		}
		if occupied {
			mask |= 1 << index
		}
	}
	return mask, nil
}

func occupiedSlotCount(properties map[string]any, slots int) (int, error) {
	count := 0
	for index := range slots {
		value, exists := properties[fmt.Sprintf("slot_%d_occupied", index)]
		if !exists {
			continue
		}
		occupied, ok := boolProperty(value)
		if !ok {
			return 0, fmt.Errorf("java state codec: invalid shelf slot %d occupancy %v", index, value)
		}
		if occupied {
			count++
		}
	}
	return count, nil
}

func vineDirectionBits(properties map[string]any) (int, error) {
	bits := 0
	for bit, key := range [...]string{"south", "west", "north", "east"} {
		value, exists := properties[key]
		if !exists {
			continue
		}
		attached, ok := boolProperty(value)
		if !ok {
			return 0, fmt.Errorf("java state codec: invalid vine attachment %s=%v", key, value)
		}
		if attached {
			bits |= 1 << bit
		}
	}
	return bits, nil
}

func isShelf(name string) bool { return strings.HasSuffix(name, "_shelf") }
func isCampfire(name string) bool {
	return name == "minecraft:campfire" || name == "minecraft:soul_campfire"
}
func isFire(name string) bool     { return name == "minecraft:fire" || name == "minecraft:soul_fire" }
func isRepeater(name string) bool { return name == "minecraft:repeater" }

func javaWallSide(value any) (string, error) {
	if b, ok := boolProperty(value); ok {
		if b {
			return "short", nil
		}
		return "none", nil
	}
	text, ok := stringProperty(value)
	if !ok {
		return "", fmt.Errorf("java state codec: invalid wall side %v", value)
	}
	switch text {
	case "none":
		return "none", nil
	case "low":
		return "short", nil
	case "tall":
		return "tall", nil
	default:
		return "", fmt.Errorf("java state codec: unsupported wall side %q", text)
	}
}

func isConnectionBlock(name string) bool {
	return strings.HasSuffix(name, "_fence") || strings.HasSuffix(name, "_pane") || strings.HasSuffix(name, "_bars") ||
		name == "minecraft:iron_bars" || name == "minecraft:glass_pane" || name == "minecraft:tripwire"
}

func isChest(name string) bool { return strings.HasSuffix(name, "_chest") || name == "minecraft:chest" }

func isStandingSign(name string) bool { return strings.HasSuffix(name, "_standing_sign") }

func isSign(name string) bool { return strings.Contains(name, "sign") }

func isBanner(name string) bool { return strings.Contains(name, "banner") }

func isSkull(name string) bool {
	switch name {
	case "minecraft:skeleton_skull", "minecraft:skeleton_wall_skull",
		"minecraft:wither_skeleton_skull", "minecraft:wither_skeleton_wall_skull",
		"minecraft:zombie_head", "minecraft:zombie_wall_head",
		"minecraft:player_head", "minecraft:player_wall_head",
		"minecraft:creeper_head", "minecraft:creeper_wall_head",
		"minecraft:dragon_head", "minecraft:dragon_wall_head",
		"minecraft:piglin_head", "minecraft:piglin_wall_head":
		return true
	}
	return false
}

func isHorizontalOnly(name string) bool {
	return isRail(name) || strings.HasSuffix(name, "_stairs") || isWall(name) || isStandingSign(name) ||
		name == "minecraft:attached_melon_stem" || name == "minecraft:attached_pumpkin_stem"
}

func isTorchLike(name string) bool {
	return strings.Contains(name, "torch") || name == "minecraft:redstone_torch" || name == "minecraft:soul_torch"
}

func nativePropertyText(props map[string]any, keys ...string) string {
	for _, key := range keys {
		if text, ok := stringProperty(props[key]); ok {
			return text
		}
	}
	return ""
}

func isIntrinsicWaterBlock(name string) bool {
	switch name {
	case "minecraft:kelp", "minecraft:kelp_plant", "minecraft:seagrass", "minecraft:tall_seagrass", "minecraft:bubble_column":
		return true
	}
	return false
}

func isRedstoneLitBlock(name string) bool {
	switch name {
	case "minecraft:redstone_torch", "minecraft:redstone_wall_torch", "minecraft:redstone_lamp", "minecraft:redstone_ore", "minecraft:deepslate_redstone_ore":
		return true
	}
	return false
}

func isButton(name string) bool {
	return strings.HasSuffix(name, "_button") || name == "minecraft:button"
}

func isLever(name string) bool { return name == "minecraft:lever" }

func isTrapdoor(name string) bool { return strings.HasSuffix(name, "_trapdoor") }

func isBell(name string) bool { return name == "minecraft:bell" }

func isGrindstone(name string) bool { return name == "minecraft:grindstone" }

func buttonFacing(properties map[string]any) (int32, error) {
	face, ok := stringProperty(properties["face"])
	if !ok {
		return 0, fmt.Errorf("java state codec: button has no face property")
	}
	switch face {
	case "floor":
		return 1, nil
	case "ceiling":
		return 0, nil
	case "wall":
		facing, ok := stringProperty(properties["facing"])
		if !ok {
			return 0, fmt.Errorf("java state codec: wall button has no facing property")
		}
		switch facing {
		case "north":
			return 2, nil
		case "south":
			return 3, nil
		case "west":
			return 4, nil
		case "east":
			return 5, nil
		}
	}
	return 0, fmt.Errorf("java state codec: unsupported button face/facing %v/%v", face, properties["facing"])
}

func leverDirection(properties map[string]any) (string, error) {
	face, faceOK := stringProperty(properties["face"])
	facing, facingOK := stringProperty(properties["facing"])
	if !faceOK || !facingOK {
		return "", fmt.Errorf("java state codec: lever needs face and facing properties")
	}
	switch face {
	case "wall":
		if facing == "north" || facing == "south" || facing == "east" || facing == "west" {
			return facing, nil
		}
	case "floor", "ceiling":
		if facing == "north" || facing == "south" {
			if face == "floor" {
				return "down_north_south", nil
			}
			return "up_north_south", nil
		}
		if facing == "east" || facing == "west" {
			if face == "floor" {
				return "down_east_west", nil
			}
			return "up_east_west", nil
		}
	}
	return "", fmt.Errorf("java state codec: unsupported lever face/facing %q/%q", face, facing)
}

func nativeLeverDirection(value any) (face, facing string, ok bool) {
	text, ok := stringProperty(value)
	if !ok {
		return "", "", false
	}
	switch text {
	case "down_east_west":
		return "floor", "east", true
	case "down_north_south":
		return "floor", "north", true
	case "up_east_west":
		return "ceiling", "east", true
	case "up_north_south":
		return "ceiling", "north", true
	case "north", "south", "east", "west":
		return "wall", text, true
	}
	return "", "", false
}

func convertJavaHorizontalFacing(value any) (int32, error) {
	facing, ok := stringProperty(value)
	if !ok {
		return 0, fmt.Errorf("java state codec: invalid horizontal facing %v", value)
	}
	switch facing {
	case "south":
		return 0, nil
	case "west":
		return 1, nil
	case "north":
		return 2, nil
	case "east":
		return 3, nil
	}
	return 0, fmt.Errorf("java state codec: unsupported horizontal facing %q", facing)
}

func convertJavaTrapdoorFacing(value any) (int32, error) {
	facing, ok := stringProperty(value)
	if !ok {
		return 0, fmt.Errorf("java state codec: invalid trapdoor facing %v", value)
	}
	switch facing {
	case "east":
		return 0, nil
	case "west":
		return 1, nil
	case "south":
		return 2, nil
	case "north":
		return 3, nil
	}
	return 0, fmt.Errorf("java state codec: unsupported trapdoor facing %q", facing)
}

func nativeTrapdoorFacing(value any) (string, bool) {
	direction, ok := intProperty(value)
	if !ok {
		return "", false
	}
	directions := [...]string{"east", "west", "south", "north"}
	if direction >= 0 && direction < len(directions) {
		return directions[direction], true
	}
	return "", false
}

func grindstoneAttachment(face string) string {
	switch face {
	case "floor":
		return "standing"
	case "ceiling":
		return "hanging"
	case "wall":
		return "side"
	}
	return ""
}

func bellAttachment(attachment string) string {
	switch attachment {
	case "floor":
		return "standing"
	case "ceiling":
		return "hanging"
	case "single_wall":
		return "side"
	case "double_wall":
		return "multiple"
	}
	return ""
}

func javaGrindstoneFace(value any) (string, bool) {
	text, ok := stringProperty(value)
	if !ok {
		return "", false
	}
	switch text {
	case "standing":
		return "floor", true
	case "hanging":
		return "ceiling", true
	case "side", "multiple":
		return "wall", true
	}
	return "", false
}

func javaBellAttachment(value any) (string, bool) {
	text, ok := stringProperty(value)
	if !ok {
		return "", false
	}
	switch text {
	case "standing":
		return "floor", true
	case "hanging":
		return "ceiling", true
	case "side":
		return "single_wall", true
	case "multiple":
		return "double_wall", true
	}
	return "", false
}
func nativeTorchFacing(properties map[string]any) (string, error) {
	face, ok := stringProperty(properties["face"])
	if !ok {
		return "", fmt.Errorf("java state codec: torch face is not a string")
	}
	switch face {
	case "floor":
		return "top", nil
	case "ceiling":
		return "unknown", nil
	case "wall":
		facing, ok := stringProperty(properties["facing"])
		if ok && (facing == "north" || facing == "south" || facing == "east" || facing == "west") {
			return facing, nil
		}
		return "", fmt.Errorf("java state codec: wall torch has unsupported facing %v", properties["facing"])
	}
	return "", fmt.Errorf("java state codec: unsupported torch face %q", face)
}
