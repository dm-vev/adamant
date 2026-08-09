package item

import (
	"bytes"
	"encoding/gob"
	"sort"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// WriteNBT encodes a Stack into a map that can be encoded using NBT. disk
// selects the disk format over the network one, which differ in how an item's
// damage is written and in whether the stack is wrapped with its name and count.
func WriteNBT(s Stack, disk bool) map[string]any {
	tag := make(map[string]any)
	if s.Empty() {
		return tag
	}
	if nbt, ok := s.Item().(world.NBTer); ok {
		for k, v := range nbt.EncodeNBT() {
			tag[k] = v
		}
	}
	writeAnvilCost(tag, s)
	writeDamage(tag, s, disk)
	writeDisplay(tag, s)
	writeDragonflyData(tag, s)
	writeEnchantments(tag, s)
	writeItemLock(tag, s)
	writeUnbreakable(tag, s)

	data := make(map[string]any)
	if disk {
		writeItemStack(data, tag, s)
	} else {
		for k, v := range tag {
			data[k] = v
		}
	}
	return data
}

// ReadNBT decodes the data of an item into an item stack. A nil s selects the
// disk format, in which the item's name and count are read from data itself;
// otherwise the tags are applied to the stack passed.
func ReadNBT(data map[string]any, s *Stack, registries ...world.BlockRegistry) Stack {
	disk, tag := s == nil, data
	if disk {
		t, ok := data["tag"].(map[string]any)
		if !ok {
			t = map[string]any{}
		}
		tag = t

		a := readItemStack(data, tag, registries...)
		s = &a
	}

	readAnvilCost(tag, s)
	readDamage(tag, s, disk)
	readDisplay(tag, s)
	readDragonflyData(tag, s)
	readEnchantments(tag, s)
	readItemLock(tag, s)
	readUnbreakable(tag, s)
	return *s
}

// MapNBT converts an item's name, count, damage (and properties when it is a
// block) in a map obtained by decoding NBT at key k to a Stack.
func MapNBT(x map[string]any, k string, registries ...world.BlockRegistry) Stack {
	if m, ok := x[k].(map[string]any); ok {
		if len(registries) == 0 {
			registries = []world.BlockRegistry{world.BlockRegistryFromNBT(x)}
		}
		return ReadNBT(m, nil, registries...)
	}
	return Stack{}
}

// writeItemStack writes the name, metadata value, count and NBT of an item to a map ready for NBT encoding.
func writeItemStack(m, t map[string]any, s Stack) {
	m["Name"], m["Damage"] = s.Item().EncodeItem()
	if b, ok := s.Item().(world.Block); ok {
		v := map[string]any{}
		writeBlock(v, b)
		m["Block"] = v
	}
	m["Count"] = byte(s.Count())
	if len(t) > 0 {
		m["tag"] = t
	}
}

// writeBlock writes the name, properties and version of a block to a map ready for NBT encoding.
func writeBlock(m map[string]any, b world.Block) {
	m["name"], m["states"] = b.EncodeBlock()
	m["version"] = chunk.CurrentBlockVersion
}

// writeDragonflyData writes additional data associated with a Stack to a map for NBT encoding.
func writeDragonflyData(m map[string]any, s Stack) {
	if v := s.Values(); len(v) != 0 {
		buf := new(bytes.Buffer)
		if err := gob.NewEncoder(buf).Encode(mapToSlice(v)); err != nil {
			return
		}
		m["dragonflyData"] = buf.Bytes()
	}
}

// mapToSlice converts a map to a slice of the type mapValue and orders the slice by the keys in the map to ensure a
// deterministic order.
func mapToSlice(m map[string]any) []mapValue {
	values := make([]mapValue, 0, len(m))
	for k, v := range m {
		values = append(values, mapValue{K: k, V: v})
	}
	sort.Slice(values, func(i, j int) bool {
		return values[i].K < values[j].K
	})
	return values
}

// mapValue represents a value in a map. It is used to convert maps to a slice and order the slice before encoding to
// NBT to ensure a deterministic output.
//
// Its name and field names are part of the on-disk format: gob identifies the
// type by name, so worlds written before this moved package still decode.
type mapValue struct {
	K string
	V any
}

// writeEnchantments writes the enchantments of an item to a map for NBT encoding.
func writeEnchantments(m map[string]any, s Stack) {
	if len(s.Enchantments()) != 0 {
		var enchantments []map[string]any
		for _, e := range s.Enchantments() {
			if eType, ok := EnchantmentID(e.Type()); ok {
				enchantments = append(enchantments, map[string]any{
					"id":  int16(eType),
					"lvl": int16(e.Level()),
				})
			}
		}
		m["ench"] = enchantments
	}
}

// writeDisplay writes the display name and lore of an item to a map for NBT encoding.
func writeDisplay(m map[string]any, s Stack) {
	name, lore := s.CustomName(), s.Lore()
	v := map[string]any{}
	if name != "" {
		v["Name"] = name
	}
	if len(lore) != 0 {
		v["Lore"] = lore
	}
	if len(v) != 0 {
		m["display"] = v
	}
}

// writeDamage writes the damage to a Stack (either an int16 for disk or int32 for network) to a map for NBT
// encoding.
func writeDamage(m map[string]any, s Stack, disk bool) {
	if v, ok := m["Damage"]; ok {
		if known, zero := isZeroNumber(v); !known || !zero {
			return
		}
	}
	if _, ok := s.Item().(Durable); !ok {
		return
	}
	damage := s.MaxDurability() - s.Durability()
	if disk {
		m["Damage"] = int16(damage)
		return
	}
	m["Damage"] = int32(damage)
}

// writeAnvilCost ...
func writeAnvilCost(m map[string]any, s Stack) {
	if cost := s.AnvilCost(); cost > 0 {
		m["RepairCost"] = int32(cost)
	}
}

// writeUnbreakable writes the unbreakable tag to an item stack if it is unbreakable.
func writeUnbreakable(m map[string]any, s Stack) {
	if s.Unbreakable() {
		m["Unbreakable"] = byte(1)
	}
}

func writeItemLock(m map[string]any, s Stack) {
	if s.Locked() {
		m["minecraft:item_lock"] = s.LockMode().LegacyValue()
	}
}

// readItemStack reads a Stack from the NBT in the map passed.
func readItemStack(m, t map[string]any, registries ...world.BlockRegistry) Stack {
	var it world.Item
	if blockItem, ok := nbtBlock(m, "Block", registries...).(world.Item); ok {
		it = blockItem
	}
	if it == nil {
		if v, ok := world.ItemByName(nbtString(m, "Name"), nbtInt16(m, "Damage")); ok {
			it = v
		}
	}
	if it == nil {
		return Stack{}
	}
	if n, ok := it.(world.NBTer); ok {
		it = world.DecodeNBT(n, t, registries...).(world.Item)
	}
	return NewStack(it, int(nbtUint8(m, "Count")))
}

// readDamage reads the damage value stored in the NBT with the Damage tag and saves it to the Stack passed.
func readDamage(m map[string]any, s *Stack, disk bool) {
	if disk {
		*s = s.Damage(int(nbtInt16(m, "Damage")))
		return
	}
	*s = s.Damage(int(nbtInt32(m, "Damage")))
}

// readAnvilCost ...
func readAnvilCost(m map[string]any, s *Stack) {
	*s = s.WithAnvilCost(int(nbtInt32(m, "RepairCost")))
}

// readEnchantments reads the enchantments stored in the ench tag of the NBT passed and stores it into a Stack.
func readEnchantments(m map[string]any, s *Stack) {
	enchantments, ok := m["ench"].([]map[string]any)
	if !ok {
		for _, e := range nbtSlice(m, "ench") {
			if v, ok := e.(map[string]any); ok {
				enchantments = append(enchantments, v)
			}
		}
	}
	for _, ench := range enchantments {
		if t, ok := EnchantmentByID(int(nbtInt16(ench, "id"))); ok {
			// NewEnchantment panics below level 1, and this NBT is read from client packets
			// and player saves, so it cannot be trusted to hold a sensible level.
			*s = s.WithForcedEnchantments(NewEnchantment(t, int(max(nbtInt16(ench, "lvl"), 1))))
		}
	}
}

// readDisplay reads the display data present in the display field in the NBT. It includes a custom name of the item
// and the lore.
func readDisplay(m map[string]any, s *Stack) {
	if display, ok := m["display"].(map[string]any); ok {
		if name, ok := display["Name"].(string); ok {
			// Only add the custom name if actually set.
			*s = s.WithCustomName(name)
		}
		if lore, ok := display["Lore"].([]string); ok {
			*s = s.WithLore(lore...)
		} else if lore, ok := display["Lore"].([]any); ok {
			loreLines := make([]string, 0, len(lore))
			for _, l := range lore {
				if line, ok := l.(string); ok {
					loreLines = append(loreLines, line)
				}
			}
			if len(loreLines) != 0 {
				*s = s.WithLore(loreLines...)
			}
		}
	}
}

// readDragonflyData reads data written to the dragonflyData field in the NBT of an item and adds it to the Stack
// passed.
func readDragonflyData(m map[string]any, s *Stack) {
	if customData, ok := m["dragonflyData"]; ok {
		d, ok := customData.([]byte)
		if !ok {
			if itf, ok := customData.([]int8); ok {
				d = make([]byte, 0, len(itf))
				for _, v := range itf {
					d = append(d, byte(v))
				}
			}
			if itf, ok := customData.([]any); ok {
				for _, v := range itf {
					switch b := v.(type) {
					case byte:
						d = append(d, b)
					case int8:
						d = append(d, byte(b))
					}
				}
			}
		}
		if len(d) == 0 {
			return
		}
		var values []mapValue
		if err := gob.NewDecoder(bytes.NewBuffer(d)).Decode(&values); err != nil {
			return
		}
		for _, val := range values {
			*s = s.WithValue(val.K, val.V)
		}
	}
}

func readItemLock(m map[string]any, s *Stack) {
	if mode, ok := lockModeFromValue(m["minecraft:item_lock"]); ok {
		*s = s.WithLockMode(mode)
		return
	}
	if mode, ok := lockModeFromValue(m["item_lock"]); ok {
		*s = s.WithLockMode(mode)
	}
}

func lockModeFromValue(v any) (LockMode, bool) {
	switch v := v.(type) {
	case map[string]any:
		mode, _ := v["mode"].(string)
		return ParseLockMode(mode)
	case uint8:
		return LockModeFromLegacyValue(v)
	case int8:
		if v >= 0 {
			return LockModeFromLegacyValue(uint8(v))
		}
	case int16:
		if v >= 0 && v <= 255 {
			return LockModeFromLegacyValue(uint8(v))
		}
	case int32:
		if v >= 0 && v <= 255 {
			return LockModeFromLegacyValue(uint8(v))
		}
	case int64:
		if v >= 0 && v <= 255 {
			return LockModeFromLegacyValue(uint8(v))
		}
	case string:
		return ParseLockMode(v)
	}
	return NotLocked, false
}

func isZeroNumber(v any) (bool, bool) {
	switch n := v.(type) {
	case int:
		return true, n == 0
	case int8:
		return true, n == 0
	case int16:
		return true, n == 0
	case int32:
		return true, n == 0
	case int64:
		return true, n == 0
	case uint:
		return true, n == 0
	case uint8:
		return true, n == 0
	case uint16:
		return true, n == 0
	case uint32:
		return true, n == 0
	case uint64:
		return true, n == 0
	case float32:
		return true, n == 0
	case float64:
		return true, n == 0
	default:
		return false, false
	}
}

// readUnbreakable reads the unbreakable value stored in the NBT with the Unbreakable tag and saves it to the Stack
// passed.
func readUnbreakable(m map[string]any, s *Stack) {
	if nbtBool(m, "Unbreakable") {
		*s = s.AsUnbreakable()
	}
}

// The readers below mirror the exported helpers in server/internal/nbtconv.
// They are duplicated rather than imported because nbtconv depends on this
// package: the item codec has to live here for the dependency to point the
// right way, and these are the only helpers it needs.

// nbtBool reads a uint8 value from a map at key k and returns true if it equals 1.
func nbtBool(m map[string]any, k string) bool { return nbtUint8(m, k) == 1 }

// nbtUint8 reads a uint8 value from a map at key k.
func nbtUint8(m map[string]any, k string) uint8 {
	v, _ := m[k].(uint8)
	return v
}

// nbtString reads a string value from a map at key k.
func nbtString(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

// nbtInt16 reads an int16 value from a map at key k.
func nbtInt16(m map[string]any, k string) int16 {
	v, _ := m[k].(int16)
	return v
}

// nbtInt32 reads an int32 value from a map at key k.
func nbtInt32(m map[string]any, k string) int32 {
	v, _ := m[k].(int32)
	return v
}

// nbtSlice reads a []any value from a map at key k.
func nbtSlice(m map[string]any, k string) []any {
	v, _ := m[k].([]any)
	return v
}

// nbtBlock decodes the data of a block in a map at key k into a world.Block.
func nbtBlock(m map[string]any, k string, registries ...world.BlockRegistry) world.Block {
	if mk, ok := m[k].(map[string]any); ok {
		name, _ := mk["name"].(string)
		properties, _ := mk["states"].(map[string]any)
		var registry world.BlockRegistry = world.DefaultBlockRegistry
		if len(registries) != 0 && registries[0] != nil {
			registry = registries[0]
		}
		b, _ := registry.BlockByName(name, properties)
		return b
	}
	return nil
}
