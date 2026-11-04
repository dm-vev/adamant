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
	"minecraft:standing_sign":                "minecraft:oak_sign",
	"minecraft:wall_sign":                    "minecraft:oak_wall_sign",
	"minecraft:golden_rail":                  "minecraft:powered_rail",
	"minecraft:powered_repeater":             "minecraft:repeater",
	"minecraft:unpowered_repeater":           "minecraft:repeater",
	"minecraft:powered_comparator":           "minecraft:comparator",
	"minecraft:unpowered_comparator":         "minecraft:comparator",
	"minecraft:lit_redstone_torch":           "minecraft:redstone_torch",
	"minecraft:unlit_redstone_torch":         "minecraft:redstone_torch",
	"minecraft:lit_redstone_lamp":            "minecraft:redstone_lamp",
	"minecraft:lit_redstone_ore":             "minecraft:redstone_ore",
	"minecraft:lit_deepslate_redstone_ore":   "minecraft:deepslate_redstone_ore",
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

func hasNativeBlock(registry chunk.BlockRegistry, name string) bool {
	_, ok := registry.StateToRuntimeID(name, nil)
	return ok
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
	return name == "minecraft:grass_block" || name == "minecraft:podzol" || name == "minecraft:mycelium"
}
func isRedstoneWire(name string) bool { return name == "minecraft:redstone_wire" }
func isMushroomBlock(name string) bool {
	return name == "minecraft:brown_mushroom_block" || name == "minecraft:red_mushroom_block" || name == "minecraft:mushroom_stem"
}
func isMultiFaceBlock(name string) bool {
	return name == "minecraft:glow_lichen" || name == "minecraft:resin_clump" || name == "minecraft:sculk_vein"
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

// mushroomVariant returns the Bedrock huge_mushroom_bits of the Java faces of
// a huge mushroom block. The variants are an enumeration, not a bitmask: 1-9
// put the cap texture on the top and on at most one north/south and one
// west/east side, 10 is a stem with pored ends and 14 and 15 cover all six
// faces with cap or stem. Faces no variant shows keep their Java state through
// the retained state ID. A face missing from properties has its default, the
// outside texture.
func mushroomVariant(name string, properties map[string]any) (int, error) {
	var faces [6]bool
	for i, key := range mushroomFaceNames {
		faces[i] = true
		if value, exists := properties[key]; exists {
			outside, ok := boolProperty(value)
			if !ok {
				return 0, fmt.Errorf("java state codec: invalid mushroom side %s=%v", key, value)
			}
			faces[i] = outside
		}
	}
	up, down, north, east, south, west := faces[0], faces[1], faces[2], faces[3], faces[4], faces[5]
	switch {
	case !up && !down && !north && !east && !south && !west:
		return 0, nil
	case name == "minecraft:mushroom_stem" && up && down && north && east && south && west:
		return 15, nil
	case name == "minecraft:mushroom_stem":
		return 10, nil
	case up && down && north && east && south && west:
		return 14, nil
	}
	row, column := 1, 1
	if north && !south {
		row = 0
	} else if south && !north {
		row = 2
	}
	if west && !east {
		column = 0
	} else if east && !west {
		column = 2
	}
	return 1 + row*3 + column, nil
}

// mushroomFaceNames are the Java faces of a huge mushroom block.
var mushroomFaceNames = [...]string{"up", "down", "north", "east", "south", "west"}

// mushroomVariantFaces returns the Java faces Bedrock huge_mushroom_bits
// variant shows with the outside texture. Unused variants show pores only.
func mushroomVariantFaces(variant int) map[string]bool {
	faces := make(map[string]bool, len(mushroomFaceNames))
	for _, key := range mushroomFaceNames {
		faces[key] = false
	}
	switch {
	case variant >= 1 && variant <= 9:
		row, column := (variant-1)/3, (variant-1)%3
		faces["up"] = true
		faces["north"], faces["south"] = row == 0, row == 2
		faces["west"], faces["east"] = column == 0, column == 2
	case variant == 10:
		faces["north"], faces["east"], faces["south"], faces["west"] = true, true, true, true
	case variant == 14 || variant == 15:
		for _, key := range mushroomFaceNames {
			faces[key] = true
		}
	}
	return faces
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
func isFire(name string) bool { return name == "minecraft:fire" || name == "minecraft:soul_fire" }

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
