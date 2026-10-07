package world

import (
	"fmt"
	"strconv"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

func DecodeStateProperties(properties map[string]any) map[string]any {
	if len(properties) == 0 { return nil }
	out := make(map[string]any, len(properties))
	for key, value := range properties {
		if text, ok := value.(string); ok {
			switch text {
			case "true": value = true
			case "false": value = false
			default:
				if n, err := strconv.ParseInt(text, 10, 32); err == nil { value = int32(n) }
			}
		}
		out[key] = value
	}
	return out
}

// NativeState stores a Bedrock block and its optional liquid layer.
// Both IDs are registry-local and Liquid is the registry's air ID when clear.
type NativeState struct {
	Block  uint32
	Liquid uint32
}

// ResolveState translates a Java block state to its Bedrock palette
// representation. Java properties are never passed directly to the native
// registry: the adapter maps each supported state family explicitly.
func ResolveState(registry chunk.BlockRegistry, state BlockState) (NativeState, error) {
	return resolveJavaState(registry, state)
}

// JavaState returns the Java block-state view of a native block and liquid
// layer, for Java-side tags and predicates.
func JavaState(registry chunk.BlockRegistry, state NativeState) (BlockState, error) {
	return reverseNativeState(registry, state)
}

// ResolveBlock resolves a Dragonfly block's own native encoder without applying
// Java property conversions. Entity-like Java placements use this for blocks
// that have no Java BlockState representation (for example item frames).
func ResolveBlock(registry chunk.BlockRegistry, block Block) (NativeState, error) {
	if registry == nil {
		return NativeState{}, fmt.Errorf("java structure: nil block registry")
	}
	if block == nil {
		return NativeState{}, fmt.Errorf("java structure: nil native block")
	}
	name, properties := block.EncodeBlock()
	rid, ok := registry.StateToRuntimeID(name, properties)
	if !ok {
		return NativeState{}, fmt.Errorf("java structure: unknown native block state %s%v", name, properties)
	}
	return NativeState{Block: rid, Liquid: registry.AirRuntimeID()}, nil
}
