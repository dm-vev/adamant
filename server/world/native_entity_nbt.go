package world

import "strconv"

// CloneNativeEntityNBT deep-copies native entity NBT, including typed NBT
// arrays and nested compounds, excluding the temporary decoder registry context.
func CloneNativeEntityNBT(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		if key == blockRegistryNBTKey {
			continue
		}
		out[key] = cloneNativeEntityNBTValue(value, false)
	}
	return out
}

// CloneJavaEntityNBT deep-copies a source payload for the native NBT encoder.
// Java signed bytes and byte arrays retain their payload bits using Go's native
// unsigned-byte representation. Other NBT fields are unchanged; the temporary
// decoder registry context is excluded.
func CloneJavaEntityNBT(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	out := make(map[string]any, len(data))
	for key, value := range data {
		if key == blockRegistryNBTKey {
			continue
		}
		out[key] = cloneNativeEntityNBTValue(value, true)
	}
	return out
}

// MergeNativeEntityNBT overlays canonical entity fields on retained native NBT.
// Attribute and equipment lists merge by their native identity (attribute Name,
// equipment position, or ChestItems Slot) to retain unrecognized fields. Native
// item tags are complete snapshots: removed item metadata must not reappear.
func MergeNativeEntityNBT(retained, canonical map[string]any) map[string]any {
	_, itemName := canonical["Name"].(string)
	_, itemCount := canonical["Count"]
	itemSnapshot := itemName && itemCount
	out := make(map[string]any, max(len(retained), len(canonical)))
	for key, value := range retained {
		if itemSnapshot && key == "tag" {
			continue
		}
		out[key] = cloneNativeEntityNBTValue(value, false)
	}
	for key, value := range canonical {
		if previous, ok := out[key]; ok {
			switch key {
			case "Attributes", "Armor", "Mainhand", "Offhand", "ChestItems":
				value = mergeNativeEntityNBTList(key, previous, value)
			}
			if oldCompound, ok := out[key].(map[string]any); ok {
				if newCompound, ok := value.(map[string]any); ok {
					value = MergeNativeEntityNBT(oldCompound, newCompound)
				}
			}
		}
		out[key] = cloneNativeEntityNBTValue(value, false)
	}
	return out
}

func mergeNativeEntityNBTList(key string, retained, canonical any) any {
	oldList, okOld := retained.([]any)
	newList, okNew := canonical.([]any)
	if !okOld || !okNew {
		return canonical
	}
	out := make([]any, 0, max(len(oldList), len(newList)))
	indexes := make(map[string]int, len(oldList))
	identity := func(index int, item map[string]any) string {
		if key == "Attributes" {
			if name, ok := item["Name"].(string); ok {
				return "n:" + name
			}
			return ""
		}
		if key == "ChestItems" {
			if slot, ok := nativeEntityNBTInt(item["Slot"]); ok {
				return "s:" + strconv.Itoa(slot)
			}
			return ""
		}
		return "i:" + strconv.Itoa(index)
	}
	for i, raw := range oldList {
		compound, ok := raw.(map[string]any)
		if !ok {
			out = append(out, cloneNativeEntityNBTValue(raw, false))
			continue
		}
		if id := identity(i, compound); id != "" {
			indexes[id] = len(out)
		}
		out = append(out, cloneNativeEntityNBTValue(compound, false))
	}
	for i, raw := range newList {
		compound, ok := raw.(map[string]any)
		if !ok {
			out = append(out, cloneNativeEntityNBTValue(raw, false))
			continue
		}
		if id := identity(i, compound); id != "" {
			if index, exists := indexes[id]; exists {
				oldCompound, _ := out[index].(map[string]any)
				out[index] = MergeNativeEntityNBT(oldCompound, compound)
				continue
			}
			indexes[id] = len(out)
		}
		out = append(out, cloneNativeEntityNBTValue(compound, false))
	}
	return out
}

func nativeEntityNBTInt(value any) (int, bool) {
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

func cloneNativeEntityNBTValue(value any, javaBytes bool) any {
	switch v := value.(type) {
	case map[string]any:
		if javaBytes {
			return CloneJavaEntityNBT(v)
		}
		return CloneNativeEntityNBT(v)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = cloneNativeEntityNBTValue(v[i], javaBytes)
		}
		return out
	case int8:
		if javaBytes {
			return byte(v)
		}
		return v
	case []byte:
		return append([]byte(nil), v...)
	case []int8:
		if !javaBytes {
			return append([]int8(nil), v...)
		}
		out := make([]byte, len(v))
		for i, value := range v {
			out[i] = byte(value)
		}
		return out
	case []int16:
		return append([]int16(nil), v...)
	case []int32:
		return append([]int32(nil), v...)
	case []int64:
		return append([]int64(nil), v...)
	case []uint16:
		return append([]uint16(nil), v...)
	case []uint32:
		return append([]uint32(nil), v...)
	case []uint64:
		return append([]uint64(nil), v...)
	case []float32:
		return append([]float32(nil), v...)
	case []float64:
		return append([]float64(nil), v...)
	case []string:
		return append([]string(nil), v...)
	default:
		return value
	}
}
