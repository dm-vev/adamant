package world

import (
	"errors"
	"maps"
)

// NBT returns a detached native NBT representation of the entity in tx.
func (h *EntityHandle) NBT(tx *Tx) map[string]any {
	if h == nil || tx == nil || tx.World() == nil || h.w != tx.World() {
		return nil
	}
	return CloneNativeEntityNBT(h.encodeNBT())
}

// MergeNBT merges patch into the entity's native NBT and refreshes its live
// entity view. Fields omitted from patch, including identity and position, are
// retained from the current handle state.
func (h *EntityHandle) MergeNBT(tx *Tx, patch map[string]any) error {
	if h == nil || tx == nil || tx.World() == nil || h.w != tx.World() {
		return errors.New("entity is not in the transaction world")
	}
	if len(patch) == 0 {
		return nil
	}
	before := h.encodeNBT()
	merged := CloneNativeEntityNBT(before)
	mergeNBTMaps(merged, patch)
	// Keep the engine-owned Bedrock actor identifier. It is not command NBT.
	if id, ok := before["UniqueID"]; ok {
		merged["UniqueID"] = id
	}
	h.decodeNBT(merged)
	h.t.DecodeNBT(merged, &h.data)
	if state := h.state; state != nil {
		// Entity.Close is a removal operation for most entity types, so replace
		// the cached view without closing it.
		state.ent, state.binds, state.binder = nil, false, nil
		state.isTicker, state.tickerChecked, state.ticker = false, false, nil
	}
	return nil
}

func mergeNBTMaps(destination, patch map[string]any) {
	for key, value := range patch {
		if value == nil {
			delete(destination, key)
			continue
		}
		patchMap, patchIsMap := value.(map[string]any)
		if patchIsMap {
			current, ok := destination[key].(map[string]any)
			if !ok {
				current = make(map[string]any)
			}
			mergeNBTMaps(current, patchMap)
			destination[key] = current
			continue
		}
		destination[key] = cloneNBTValue(value)
	}
}

func cloneNBTValue(value any) any {
	if nested, ok := value.(map[string]any); ok {
		return maps.Clone(nested)
	}
	return value
}
