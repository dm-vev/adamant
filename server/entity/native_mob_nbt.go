package entity

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// NativeMobAttribute is one Bedrock actor Attributes entry. BDS 1.26.52.3
// stores these as named compounds with Base, Current, DefaultMin, DefaultMax,
// Min and Max numeric fields. Raw retains any additional fields.
type NativeMobAttribute struct {
	Name                   string
	Base, Current          float64
	DefaultMin, DefaultMax float64
	Min, Max               float64
	Raw                    map[string]any
}

// NativeMobItem is a native Bedrock actor equipment/item record. The probed
// 1.26.52.3 item compounds use Name, Count, Damage and WasPickedUp; chest
// inventory entries additionally use Slot. Raw preserves unrecognized tags.
type NativeMobItem struct {
	Name        string
	Count       byte
	Damage      int32
	WasPickedUp byte
	Slot        *int32
	Raw         map[string]any
}

// NativeMobEquipment contains native actor inventory/equipment lists.
type NativeMobEquipment struct {
	Armor, Mainhand, Offhand, ChestItems []NativeMobItem
}

func nativeMobItemFromStack(stack item.Stack) NativeMobItem {
	raw := item.WriteNBT(stack, true)
	if stack.Empty() {
		return NativeMobItem{Raw: raw}
	}
	name, damage := stack.Item().EncodeItem()
	wear := int32(damage)
	if stack.MaxDurability() > 0 {
		wear = int32(stack.MaxDurability() - stack.Durability())
	}
	return NativeMobItem{Name: name, Count: byte(stack.Count()), Damage: wear, Raw: raw}
}

// CloneNativeMobNBT returns a deep copy of an actor payload. EntityData keeps
// this alongside decoded behavior so serializers can round-trip fields they do
// not understand.
func CloneNativeMobNBT(data map[string]any) map[string]any { return world.CloneNativeEntityNBT(data) }

// MergeNativeMobNBT overlays canonical actor fields while retaining unknown
// Bedrock actor tags and same-slot equipment/attribute extensions.
func MergeNativeMobNBT(retained, canonical map[string]any) map[string]any {
	return world.MergeNativeEntityNBT(retained, canonical)
}

// ReadNativeMobAttributes reads BDS's Attributes compound list. Each Raw map
// is independent and retains fields not represented by this typed view.
func ReadNativeMobAttributes(data map[string]any) []NativeMobAttribute {
	list, ok := data["Attributes"].([]any)
	if !ok {
		return nil
	}
	attrs := make([]NativeMobAttribute, 0, len(list))
	for _, raw := range list {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, ok := m["Name"].(string)
		if !ok || name == "" {
			continue
		}
		attrs = append(attrs, NativeMobAttribute{Name: name, Base: nativeNBTNumber(m["Base"]), Current: nativeNBTNumber(m["Current"]), DefaultMin: nativeNBTNumber(m["DefaultMin"]), DefaultMax: nativeNBTNumber(m["DefaultMax"]), Min: nativeNBTNumber(m["Min"]), Max: nativeNBTNumber(m["Max"]), Raw: CloneNativeMobNBT(m)})
	}
	return attrs
}

// EncodeNativeMobAttributes returns a BDS Attributes list. Existing same-name
// entries are merged so fields unknown to this implementation are preserved.
func EncodeNativeMobAttributes(existing []NativeMobAttribute, updates []NativeMobAttribute) []any {
	byName := make(map[string]NativeMobAttribute, len(existing)+len(updates))
	order := make([]string, 0, len(existing)+len(updates))
	for _, attr := range existing {
		if attr.Name == "" {
			continue
		}
		if _, ok := byName[attr.Name]; !ok {
			order = append(order, attr.Name)
		}
		byName[attr.Name] = attr
	}
	for _, attr := range updates {
		if attr.Name == "" {
			continue
		}
		if previous, ok := byName[attr.Name]; ok {
			attr.Raw = MergeNativeMobNBT(previous.Raw, attr.Raw)
		} else {
			order = append(order, attr.Name)
		}
		byName[attr.Name] = attr
	}
	out := make([]any, 0, len(order))
	for _, name := range order {
		a := byName[name]
		m := CloneNativeMobNBT(a.Raw)
		if m == nil {
			m = make(map[string]any, 7)
		}
		m["Name"], m["Base"], m["Current"] = a.Name, float32(a.Base), float32(a.Current)
		m["DefaultMin"], m["DefaultMax"] = float32(a.DefaultMin), float32(a.DefaultMax)
		m["Min"], m["Max"] = float32(a.Min), float32(a.Max)
		out = append(out, m)
	}
	return out
}

// ReadNativeMobEquipment reads the probed Bedrock actor equipment compounds.
func ReadNativeMobEquipment(data map[string]any) NativeMobEquipment {
	return NativeMobEquipment{Armor: readNativeMobItems(data, "Armor"), Mainhand: readNativeMobItems(data, "Mainhand"), Offhand: readNativeMobItems(data, "Offhand"), ChestItems: readNativeMobItems(data, "ChestItems")}
}

// EncodeNativeMobEquipment returns only non-nil lists so a caller can update a
// subset of equipment. Empty, non-nil slices explicitly clear their slots.
func EncodeNativeMobEquipment(e NativeMobEquipment) map[string]any {
	out := make(map[string]any, 4)
	if e.Armor != nil {
		out["Armor"] = encodeNativeMobItems(e.Armor, "Armor")
	}
	if e.Mainhand != nil {
		out["Mainhand"] = encodeNativeMobItems(e.Mainhand, "Mainhand")
	}
	if e.Offhand != nil {
		out["Offhand"] = encodeNativeMobItems(e.Offhand, "Offhand")
	}
	if e.ChestItems != nil {
		out["ChestItems"] = encodeNativeMobItems(e.ChestItems, "ChestItems")
	}
	return out
}

func readNativeMobItems(data map[string]any, key string) []NativeMobItem {
	values, ok := data[key].([]any)
	if !ok {
		return nil
	}
	items := make([]NativeMobItem, 0, len(values))
	for _, value := range values {
		raw, ok := value.(map[string]any)
		if !ok {
			items = append(items, NativeMobItem{})
			continue
		}
		item := NativeMobItem{Raw: world.CloneNativeEntityNBT(raw)}
		item.Name, _ = raw["Name"].(string)
		item.Count = nativeNBTByte(raw["Count"])
		if damage, ok := nativeNBTInt(raw["Damage"]); ok {
			item.Damage = int32(damage)
		}
		item.WasPickedUp = nativeNBTByte(raw["WasPickedUp"])
		if slot, ok := nativeNBTInt(raw["Slot"]); ok && slot >= -1<<31 && slot <= 1<<31-1 {
			value := int32(slot)
			item.Slot = &value
		}
		items = append(items, item)
	}
	return items
}

func encodeNativeMobItems(items []NativeMobItem, key string) []any {
	out := make([]any, len(items))
	for i, item := range items {
		raw := world.CloneNativeEntityNBT(item.Raw)
		if raw == nil {
			raw = make(map[string]any, 5)
		}
		raw["Name"] = item.Name
		raw["Count"] = item.Count
		raw["Damage"] = item.Damage
		raw["WasPickedUp"] = item.WasPickedUp
		if key == "ChestItems" && item.Slot != nil {
			raw["Slot"] = byte(*item.Slot)
		}
		out[i] = raw
	}
	return out
}

// ReadNativeMobJavaState reads the Adamant-only namespace used for Java source
// data that has no evidenced Bedrock actor field. It is not a native BDS tag.
func ReadNativeMobJavaState(data map[string]any) map[string]any {
	state, _ := data["adamant:java"].(map[string]any)
	return CloneNativeMobNBT(state)
}

// MergeNativeMobJavaState stores Java-only compatibility data without
// conflating UUIDs, anger timers or other Java fields with native actor tags.
func MergeNativeMobJavaState(native, javaState map[string]any) map[string]any {
	out := CloneNativeMobNBT(native)
	if out == nil {
		out = make(map[string]any)
	}
	if javaState == nil {
		delete(out, "adamant:java")
		return out
	}
	out["adamant:java"] = CloneNativeMobNBT(javaState)
	return out
}

// NativeMobUniqueID reads the BDS actor identity as a scalar signed int64. It
// must not be confused with a Java UUID or its four-int array encoding.
func NativeMobUniqueID(data map[string]any) (int64, bool) { return nativeNBTInt64(data["UniqueID"]) }

// NativeMobOwnerID reads the Bedrock actor owner reference, a scalar signed
// UniqueID in OwnerNew. It is not Java's UUID int-array representation.
func NativeMobOwnerID(data map[string]any) (int64, bool) { return nativeNBTInt64(data["OwnerNew"]) }

// NativeMobAngry reads BDS's observed IsAngry byte flag; it is not a Java
// AngerTime or target UUID.
func NativeMobAngry(data map[string]any) bool { return nativeNBTByte(data["IsAngry"]) != 0 }

// NativeMobVariant reads the observed numeric Bedrock Variant field.
func NativeMobVariant(data map[string]any) (int32, bool) {
	v, ok := nativeNBTInt64(data["Variant"])
	return int32(v), ok && v >= -1<<31 && v <= 1<<31-1
}

// NativeMobColor reads the observed byte-valued Bedrock Color field.
func NativeMobColor(data map[string]any) (byte, bool) {
	v, ok := data["Color"]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case byte:
		return n, true
	case int8:
		return byte(n), true
	case int32:
		return byte(n), n >= 0 && n <= 255
	case int64:
		return byte(n), n >= 0 && n <= 255
	}
	return 0, false
}

// SetNativeMobOwnerID writes the scalar signed-int64 OwnerNew field.
func SetNativeMobOwnerID(data map[string]any, ownerID int64) map[string]any {
	return MergeNativeMobNBT(data, map[string]any{"OwnerNew": ownerID})
}

// SetNativeMobAngry writes BDS's observed byte-valued IsAngry flag.
func SetNativeMobAngry(data map[string]any, angry bool) map[string]any {
	value := byte(0)
	if angry {
		value = 1
	}
	return MergeNativeMobNBT(data, map[string]any{"IsAngry": value})
}

// SetNativeMobVariant writes the observed numeric int32 Variant field.
func SetNativeMobVariant(data map[string]any, variant int32) map[string]any {
	return MergeNativeMobNBT(data, map[string]any{"Variant": variant})
}

// SetNativeMobColor writes the observed byte-valued Color field.
func SetNativeMobColor(data map[string]any, color byte) map[string]any {
	return MergeNativeMobNBT(data, map[string]any{"Color": color})
}

func nativeNBTByte(value any) byte {
	switch v := value.(type) {
	case byte:
		return v
	case int8:
		return byte(v)
	case int16:
		return byte(v)
	case int32:
		return byte(v)
	case int64:
		return byte(v)
	case int:
		return byte(v)
	}
	return 0
}
func nativeNBTNumber(value any) float64 {
	switch v := value.(type) {
	case byte:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	}
	return 0
}
func nativeNBTInt64(value any) (int64, bool) {
	switch v := value.(type) {
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case int:
		return int64(v), true
	}
	return 0, false
}
func nativeNBTInt(value any) (int, bool) {
	switch v := value.(type) {
	case byte:
		return int(v), true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), int64(int(v)) == v
	case int:
		return v, true
	}
	return 0, false
}
