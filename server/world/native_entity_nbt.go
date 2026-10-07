package world

import "strconv"

// CloneNativeEntityNBT deep-copies native entity NBT, including typed NBT
// arrays and nested compounds.
func CloneNativeEntityNBT(data map[string]any) map[string]any {
	if data == nil { return nil }
	out := make(map[string]any, len(data))
	for key, value := range data { out[key] = cloneNativeEntityNBTValue(value) }
	return out
}

// MergeNativeEntityNBT overlays canonical entity fields on retained native NBT.
// Attribute and equipment lists merge by their native identity (attribute Name,
// equipment position, or ChestItems Slot) to retain unrecognized fields.
func MergeNativeEntityNBT(retained, canonical map[string]any) map[string]any {
	out := CloneNativeEntityNBT(retained)
	if out == nil { out = make(map[string]any, len(canonical)) }
	for key, value := range canonical {
		if previous, ok := out[key]; ok {
			switch key {
			case "Attributes", "Armor", "Mainhand", "Offhand", "ChestItems":
				value = mergeNativeEntityNBTList(key, previous, value)
			}
			if oldCompound, ok := out[key].(map[string]any); ok {
				if newCompound, ok := value.(map[string]any); ok { value = MergeNativeEntityNBT(oldCompound, newCompound) }
			}
		}
		out[key] = cloneNativeEntityNBTValue(value)
	}
	return out
}

func mergeNativeEntityNBTList(key string, retained, canonical any) any {
	oldList, okOld := retained.([]any)
	newList, okNew := canonical.([]any)
	if !okOld || !okNew { return canonical }
	out := make([]any, 0, max(len(oldList), len(newList)))
	indexes := make(map[string]int, len(oldList))
	identity := func(index int, item map[string]any) string {
		if key == "Attributes" { if name, ok := item["Name"].(string); ok { return "n:" + name }; return "" }
		if key == "ChestItems" { if slot, ok := nativeEntityNBTInt(item["Slot"]); ok { return "s:" + strconv.Itoa(slot) }; return "" }
		return "i:" + strconv.Itoa(index)
	}
	for i, raw := range oldList {
		compound, ok := raw.(map[string]any)
		if !ok { out = append(out, cloneNativeEntityNBTValue(raw)); continue }
		if id := identity(i, compound); id != "" { indexes[id] = len(out) }
		out = append(out, cloneNativeEntityNBTValue(compound))
	}
	for i, raw := range newList {
		compound, ok := raw.(map[string]any)
		if !ok { out = append(out, cloneNativeEntityNBTValue(raw)); continue }
		if id := identity(i, compound); id != "" {
			if index, exists := indexes[id]; exists { oldCompound, _ := out[index].(map[string]any); out[index] = MergeNativeEntityNBT(oldCompound, compound); continue }
			indexes[id] = len(out)
		}
		out = append(out, cloneNativeEntityNBTValue(compound))
	}
	return out
}

func nativeEntityNBTInt(value any) (int, bool) {
	switch v := value.(type) {
	case byte: return int(v), true
	case int8: return int(v), true
	case int16: return int(v), true
	case int32: return int(v), true
	case int64: return int(v), int64(int(v)) == v
	case int: return v, true
	}
	return 0, false
}

func cloneNativeEntityNBTValue(value any) any {
	switch v := value.(type) {
	case map[string]any: return CloneNativeEntityNBT(v)
	case []any: out := make([]any, len(v)); for i := range v { out[i] = cloneNativeEntityNBTValue(v[i]) }; return out
	case []byte: return append([]byte(nil), v...)
	case []int8: return append([]int8(nil), v...)
	case []int16: return append([]int16(nil), v...)
	case []int32: return append([]int32(nil), v...)
	case []int64: return append([]int64(nil), v...)
	case []uint16: return append([]uint16(nil), v...)
	case []uint32: return append([]uint32(nil), v...)
	case []uint64: return append([]uint64(nil), v...)
	case []float32: return append([]float32(nil), v...)
	case []float64: return append([]float64(nil), v...)
	case []string: return append([]string(nil), v...)
	default: return value
	}
}
