package structure

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	nbtTagEnd       = 0
	nbtTagByte      = 1
	nbtTagShort     = 2
	nbtTagInt       = 3
	nbtTagLong      = 4
	nbtTagFloat     = 5
	nbtTagDouble    = 6
	nbtTagByteArray = 7
	nbtTagString    = 8
	nbtTagList      = 9
	nbtTagCompound  = 10
	nbtTagIntArray  = 11
	nbtTagLongArray = 12
)

// nbtInternTable interns frequent Java structure NBT keys, block-state property
// names/values, and common block identifiers so map[string]string lookups on
// []byte avoid per-string heap allocations.
var nbtInternTable = func() map[string]string {
	words := []string{
		// Core structure template & block-entity keys
		"pos", "state", "nbt", "Name", "name", "Properties", "properties",
		"id", "blocks", "palette", "palettes", "size", "entities", "blockPos",
		"DataVersion", "final_state", "joint", "pool", "target", "selection_priority",
		"placement_priority", "LootTable", "LootTableSeed", "Items", "Slot",
		"count", "components", "Pos", "Motion", "Rotation", "FallDistance",
		"Fire", "Air", "OnGround", "Invulnerable", "PortalCooldown", "UUID",
		"CustomName", "PersistenceRequired", "CanPickUpLoot", "Health",
		"AbsorptionAmount", "HurtTime", "HurtByTimestamp", "DeathTime",
		"LeftHanded", "NoAI", "Silent", "NoGravity", "Glowing", "Tags",
		"Brain", "memories", "attributes", "ArmorItems", "HandItems",
		"ArmorDropChances", "HandDropChances", "VillagerData", "profession",
		"type", "level", "Facing", "facing", "Item", "Fixed", "Invisible",
		"TileX", "TileY", "TileZ", "variant", "x", "y", "z", "keepPacked",
		// Frequent block-state property keys
		"half", "shape", "waterlogged", "axis", "lit", "open", "powered",
		"hinge", "part", "occupied", "stage", "age", "distance", "persistent",
		"snowy", "north", "south", "east", "west", "up", "down", "in_wall",
		"attached", "disarmed", "triggered", "conditional", "inverted",
		"extended", "short", "eye", "eggs", "hatch", "honey_level", "moisture",
		"note", "instrument", "rotation", "hanging", "face", "drag", "has_book",
		"has_record", "locked", "signal_fire", "unstable", "berries", "candles",
		"thickness", "vertical_direction", "tilt", "sculk_sensor_phase",
		"shrieking", "can_summon", "ominous", "trial_spawner_state",
		"vault_state", "crafter_orientation", "crafting", "bloom", "charges",
		"dusted", "cracked", "layers", "bites", "delay", "power", "mode",
		"orientation", "leaves", "bottom", "has_bottle_0", "has_bottle_1",
		"has_bottle_2", "slot_0_occupied", "slot_1_occupied", "slot_2_occupied",
		"slot_3_occupied", "slot_4_occupied", "slot_5_occupied",
		// Frequent property values
		"true", "false", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9",
		"10", "11", "12", "13", "14", "15", "top", "double", "single",
		"left", "right", "straight", "inner_left", "inner_right", "outer_left",
		"outer_right", "lower", "upper", "head", "foot", "floor", "wall",
		"ceiling", "none", "low", "tall", "side", "normal", "sticky",
		"rollable", "aligned", "north_south", "east_west", "ascending_east",
		"ascending_west", "ascending_north", "ascending_south", "south_east",
		"south_west", "north_west", "north_east", "tip", "frustum", "middle",
		"base", "tip_merge", "inactive", "active", "cooldown", "waiting_for_players",
		"ejecting_reward", "unlocking",
		// Frequent Java block IDs
		"minecraft:air", "minecraft:cave_air", "minecraft:void_air",
		"minecraft:structure_void", "minecraft:stone", "minecraft:deepslate",
		"minecraft:cobbled_deepslate", "minecraft:deepslate_bricks",
		"minecraft:deepslate_tiles", "minecraft:polished_deepslate",
		"minecraft:cracked_deepslate_bricks", "minecraft:cracked_deepslate_tiles",
		"minecraft:chiseled_deepslate", "minecraft:cobblestone",
		"minecraft:mossy_cobblestone", "minecraft:stone_bricks",
		"minecraft:mossy_stone_bricks", "minecraft:cracked_stone_bricks",
		"minecraft:dirt", "minecraft:grass_block", "minecraft:water",
		"minecraft:lava", "minecraft:oak_planks", "minecraft:spruce_planks",
		"minecraft:birch_planks", "minecraft:dark_oak_planks",
		"minecraft:acacia_planks", "minecraft:sandstone",
		"minecraft:smooth_sandstone", "minecraft:cut_sandstone",
		"minecraft:tuff", "minecraft:tuff_bricks", "minecraft:polished_tuff",
		"minecraft:chiseled_tuff", "minecraft:chiseled_tuff_bricks",
		"minecraft:oxidized_copper", "minecraft:waxed_oxidized_copper",
		"minecraft:waxed_copper_block", "minecraft:waxed_weathered_copper",
		"minecraft:waxed_exposed_copper", "minecraft:waxed_cut_copper",
		"minecraft:waxed_oxidized_cut_copper", "minecraft:waxed_copper_bulb",
		"minecraft:waxed_oxidized_copper_bulb", "minecraft:waxed_copper_grate",
		"minecraft:waxed_oxidized_copper_grate", "minecraft:waxed_chiseled_copper",
		"minecraft:jigsaw", "minecraft:chest", "minecraft:barrel",
		"minecraft:spawner", "minecraft:trial_spawner", "minecraft:vault",
		"minecraft:sculk", "minecraft:sculk_vein", "minecraft:gray_wool",
		"minecraft:blue_wool", "minecraft:light_blue_wool", "minecraft:cyan_wool",
		"minecraft:red_wool", "minecraft:white_wool", "minecraft:black_wool",
		"minecraft:torch", "minecraft:soul_lantern", "minecraft:lantern",
		"minecraft:iron_bars", "minecraft:chain", "minecraft:ladder",
		"minecraft:glass_pane", "minecraft:oak_log", "minecraft:spruce_log",
		"minecraft:dark_oak_log", "minecraft:stripped_oak_log",
		"minecraft:stripped_spruce_log", "minecraft:stripped_dark_oak_log",
		"minecraft:oak_fence", "minecraft:spruce_fence", "minecraft:oak_stairs",
		"minecraft:spruce_stairs", "minecraft:cobblestone_stairs",
		"minecraft:deepslate_brick_stairs", "minecraft:deepslate_tile_stairs",
		"minecraft:cobbled_deepslate_stairs", "minecraft:tuff_brick_stairs",
		"minecraft:oak_slab", "minecraft:spruce_slab",
		"minecraft:deepslate_brick_slab", "minecraft:deepslate_tile_slab",
		"minecraft:cobbled_deepslate_slab", "minecraft:tuff_brick_slab",
		"minecraft:deepslate_brick_wall", "minecraft:deepslate_tile_wall",
		"minecraft:cobbled_deepslate_wall", "minecraft:bedrock",
		"minecraft:gravel", "minecraft:sand", "minecraft:netherrack",
		"minecraft:blackstone", "minecraft:polished_blackstone_bricks",
		"minecraft:basalt", "minecraft:end_stone_bricks",
		"minecraft:purpur_block", "minecraft:purpur_pillar",
	}
	m := make(map[string]string, len(words))
	for _, w := range words {
		m[w] = w
	}
	return m
}()

type nbtReader struct {
	buf []byte
	off int
}

func newNBTReader(b []byte) *nbtReader {
	return &nbtReader{buf: b}
}

func (r *nbtReader) readByte() (byte, error) {
	if r.off >= len(r.buf) {
		return 0, io.EOF
	}
	b := r.buf[r.off]
	r.off++
	return b, nil
}

func (r *nbtReader) readInt16() (int16, error) {
	if r.off+2 > len(r.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := int16(binary.BigEndian.Uint16(r.buf[r.off:]))
	r.off += 2
	return v, nil
}

func (r *nbtReader) readInt32() (int32, error) {
	if r.off+4 > len(r.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := int32(binary.BigEndian.Uint32(r.buf[r.off:]))
	r.off += 4
	return v, nil
}

func (r *nbtReader) readInt64() (int64, error) {
	if r.off+8 > len(r.buf) {
		return 0, io.ErrUnexpectedEOF
	}
	v := int64(binary.BigEndian.Uint64(r.buf[r.off:]))
	r.off += 8
	return v, nil
}

func (r *nbtReader) readFloat32() (float32, error) {
	v, err := r.readInt32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(uint32(v)), nil
}

func (r *nbtReader) readFloat64() (float64, error) {
	v, err := r.readInt64()
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(uint64(v)), nil
}

func (r *nbtReader) readStringBytes() ([]byte, error) {
	n, err := r.readInt16()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("nbt: negative string length %d", n)
	}
	if n == 0 {
		return nil, nil
	}
	end := r.off + int(n)
	if end > len(r.buf) {
		return nil, io.ErrUnexpectedEOF
	}
	b := r.buf[r.off:end]
	r.off = end
	return b, nil
}

func (r *nbtReader) readString() (string, error) {
	b, err := r.readStringBytes()
	if err != nil || len(b) == 0 {
		return "", err
	}
	if interned, ok := nbtInternTable[string(b)]; ok {
		return interned, nil
	}
	return string(b), nil
}

func (r *nbtReader) readByteArray() ([]byte, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("nbt: negative byte array length %d", n)
	}
	if int(n) > len(r.buf)-r.off {
		return nil, io.ErrUnexpectedEOF
	}
	b := make([]byte, n)
	copy(b, r.buf[r.off:r.off+int(n)])
	r.off += int(n)
	return b, nil
}

func (r *nbtReader) readInt32Array() ([]int32, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("nbt: negative int array length %d", n)
	}
	if int64(n)*4 > int64(len(r.buf)-r.off) {
		return nil, io.ErrUnexpectedEOF
	}
	a := make([]int32, n)
	for i := int32(0); i < n; i++ {
		a[i] = int32(binary.BigEndian.Uint32(r.buf[r.off:]))
		r.off += 4
	}
	return a, nil
}

func (r *nbtReader) readInt64Array() ([]int64, error) {
	n, err := r.readInt32()
	if err != nil {
		return nil, err
	}
	if n < 0 {
		return nil, fmt.Errorf("nbt: negative long array length %d", n)
	}
	if int64(n)*8 > int64(len(r.buf)-r.off) {
		return nil, io.ErrUnexpectedEOF
	}
	a := make([]int64, n)
	for i := int32(0); i < n; i++ {
		a[i] = int64(binary.BigEndian.Uint64(r.buf[r.off:]))
		r.off += 8
	}
	return a, nil
}

func (r *nbtReader) readTagName() (string, error) {
	return r.readString()
}

func (r *nbtReader) skipBytes(n int64) error {
	if n < 0 {
		return fmt.Errorf("nbt: negative skip length %d", n)
	}
	if n > int64(len(r.buf)-r.off) {
		return io.ErrUnexpectedEOF
	}
	r.off += int(n)
	return nil
}

func (r *nbtReader) skipPayload(tagType byte, depth int) error {
	if depth > 512 {
		return fmt.Errorf("nbt: maximum nesting depth exceeded")
	}
	switch tagType {
	case nbtTagEnd:
		return nil
	case nbtTagByte:
		return r.skipBytes(1)
	case nbtTagShort:
		return r.skipBytes(2)
	case nbtTagInt, nbtTagFloat:
		return r.skipBytes(4)
	case nbtTagLong, nbtTagDouble:
		return r.skipBytes(8)
	case nbtTagString:
		n, err := r.readInt16()
		if err != nil {
			return err
		}
		if n < 0 {
			return fmt.Errorf("nbt: negative string length %d", n)
		}
		return r.skipBytes(int64(n))
	case nbtTagByteArray:
		n, err := r.readInt32()
		if err != nil {
			return err
		}
		return r.skipBytes(int64(n))
	case nbtTagIntArray:
		n, err := r.readInt32()
		if err != nil {
			return err
		}
		return r.skipBytes(int64(n) * 4)
	case nbtTagLongArray:
		n, err := r.readInt32()
		if err != nil {
			return err
		}
		return r.skipBytes(int64(n) * 8)
	case nbtTagList:
		return r.skipList(depth)
	case nbtTagCompound:
		for {
			t, err := r.readByte()
			if err != nil {
				return err
			}
			if t == nbtTagEnd {
				return nil
			}
			if _, err := r.readStringBytes(); err != nil {
				return err
			}
			if err := r.skipPayload(t, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("nbt: unsupported tag type %d", tagType)
	}
}

func (r *nbtReader) skipList(depth int) error {
	elemType, err := r.readByte()
	if err != nil {
		return err
	}
	length, err := r.readInt32()
	if err != nil {
		return err
	}
	if length < 0 {
		return fmt.Errorf("nbt: negative list length %d", length)
	}
	switch elemType {
	case nbtTagEnd:
		if length != 0 {
			return fmt.Errorf("nbt: non-empty list with end element type")
		}
		return nil
	case nbtTagByte:
		return r.skipBytes(int64(length))
	case nbtTagShort:
		return r.skipBytes(int64(length) * 2)
	case nbtTagInt, nbtTagFloat:
		return r.skipBytes(int64(length) * 4)
	case nbtTagLong, nbtTagDouble:
		return r.skipBytes(int64(length) * 8)
	default:
		for range length {
			if err := r.skipPayload(elemType, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
}

func (r *nbtReader) readPayload(tagType byte, depth int) (any, error) {
	if depth > 512 {
		return nil, fmt.Errorf("nbt: maximum nesting depth exceeded")
	}

	switch tagType {
	case nbtTagEnd:
		return nil, nil
	case nbtTagByte:
		v, err := r.readByte()
		return int8(v), err
	case nbtTagShort:
		v, err := r.readInt16()
		return v, err
	case nbtTagInt:
		v, err := r.readInt32()
		return v, err
	case nbtTagLong:
		v, err := r.readInt64()
		return v, err
	case nbtTagFloat:
		v, err := r.readFloat32()
		return v, err
	case nbtTagDouble:
		v, err := r.readFloat64()
		return v, err
	case nbtTagByteArray:
		return r.readByteArray()
	case nbtTagString:
		return r.readString()
	case nbtTagList:
		elemType, err := r.readByte()
		if err != nil {
			return nil, err
		}
		length, err := r.readInt32()
		if err != nil {
			return nil, err
		}
		if length < 0 {
			return nil, fmt.Errorf("nbt: negative list length %d", length)
		}

		switch elemType {
		case nbtTagEnd:
			// An empty list may use TAG_End as its element type.
			if length != 0 {
				return nil, fmt.Errorf("nbt: non-empty list with end element type")
			}
			return []any{}, nil
		case nbtTagInt:
			a := make([]int32, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readInt32()
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		case nbtTagLong:
			a := make([]int64, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readInt64()
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		case nbtTagByte:
			a := make([]int8, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readByte()
				if err != nil {
					return nil, err
				}
				a[i] = int8(v)
			}
			return a, nil
		case nbtTagCompound:
			a := make([]map[string]any, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readCompound(depth + 1)
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		default:
			// Generic slow-path.
			a := make([]any, length)
			for i := int32(0); i < length; i++ {
				v, err := r.readPayload(elemType, depth+1)
				if err != nil {
					return nil, err
				}
				a[i] = v
			}
			return a, nil
		}
	case nbtTagCompound:
		return r.readCompound(depth + 1)
	case nbtTagIntArray:
		return r.readInt32Array()
	case nbtTagLongArray:
		return r.readInt64Array()
	default:
		return nil, fmt.Errorf("nbt: unsupported tag type %d", tagType)
	}
}

func (r *nbtReader) readCompound(depth int) (map[string]any, error) {
	m := map[string]any{}
	for {
		t, err := r.readByte()
		if err != nil {
			return nil, err
		}
		if t == nbtTagEnd {
			return m, nil
		}
		name, err := r.readTagName()
		if err != nil {
			return nil, err
		}
		v, err := r.readPayload(t, depth)
		if err != nil {
			return nil, err
		}
		m[name] = v
	}
}

// decodeNBT decodes a (decompressed) Java Edition NBT payload into a root compound.
func decodeNBT(b []byte) (map[string]any, error) {
	r := newNBTReader(b)
	rootType, err := r.readByte()
	if err != nil {
		return nil, err
	}
	if rootType != nbtTagCompound {
		return nil, fmt.Errorf("nbt: root tag type %d, expected compound", rootType)
	}
	// Root name is ignored.
	if _, err := r.readTagName(); err != nil {
		return nil, err
	}
	return r.readCompound(1)
}
