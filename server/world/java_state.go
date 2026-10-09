package world

import (
	"fmt"
	"strconv"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func DecodeStateProperties(properties map[string]any) map[string]any {
	if len(properties) == 0 {
		return nil
	}
	out := make(map[string]any, len(properties))
	for key, value := range properties {
		if text, ok := value.(string); ok {
			switch text {
			case "true":
				value = true
			case "false":
				value = false
			default:
				if n, err := strconv.ParseInt(text, 10, 32); err == nil {
					value = int32(n)
				}
			}
		}
		out[key] = value
	}
	return out
}

// NativeState stores a Bedrock block and its optional liquid layer.
// Both native IDs are registry-local. JavaStateID retains source properties
// that cannot be recovered from those layers; zero means the projection is exact.
type NativeState struct {
	Block       uint32
	Liquid      uint32
	JavaStateID uint32
}

// CanonicalJavaID resolves a retained or exactly projected source identity
// without materialising its property map. Zero denotes a native-only state.
func (state NativeState) CanonicalJavaID(registry chunk.BlockRegistry) uint32 {
	if state.JavaStateID != 0 {
		return state.JavaStateID
	}
	return nativeJavaStateID(registry, state)
}

// NativeStateAt reads both native layers and their retained source identity.
func NativeStateAt(c *chunk.Chunk, x uint8, y int16, z uint8) NativeState {
	return NativeState{Block: c.Block(x, y, z, 0), Liquid: c.Block(x, y, z, 1), JavaStateID: c.JavaStateID(x, y, z)}
}

// SetNativeState publishes both layers before attaching their source identity.
func SetNativeState(c *chunk.Chunk, x uint8, y int16, z uint8, state NativeState) {
	c.SetBlock(x, y, z, 0, state.Block)
	c.SetBlock(x, y, z, 1, state.Liquid)
	c.SetJavaStateID(x, y, z, state.JavaStateID)
}

// JavaBlockStateID reads a canonical source state without materialising its
// properties. Like Block, it may load its column. Out-of-height positions are air.
func (tx *Tx) JavaBlockStateID(pos cube.Pos) (uint32, error) {
	id := tx.javaBlockStateID(pos)
	if _, ok := javadata.BlockStateName(id); !ok {
		return 0, fmt.Errorf("Java block state at %v has no canonical source identity", pos)
	}
	return id, nil
}

// javaBlockStateID returns zero for native-only states without inventing a
// canonical identity. Native capability adapters may handle these separately.
func (tx *Tx) javaBlockStateID(pos cube.Pos) uint32 {
	registry := tx.World().conf.Blocks
	state := NativeState{Block: registry.AirRuntimeID(), Liquid: registry.AirRuntimeID()}
	if !pos.OutOfBounds(tx.Range()) {
		c := tx.chunk(chunkPosFromBlockPos(pos))
		state = NativeStateAt(c.Chunk, uint8(pos[0]&15), int16(pos[1]), uint8(pos[2]&15))
	}
	id := state.JavaStateID
	if id == 0 {
		id = nativeJavaStateID(registry, state)
	}
	return id
}

// ResolveState translates a Java block state to its Bedrock palette
// representation. Java properties are never passed directly to the native
// registry: the adapter maps each supported state family explicitly.
func ResolveState(registry chunk.BlockRegistry, state BlockState) (NativeState, error) {
	native, err := resolveJavaState(registry, state)
	if err != nil {
		return NativeState{}, err
	}
	return native.WithJavaState(registry, state)
}

// JavaState returns the Java block-state view of a native block and liquid
// layer, for Java-side tags and predicates.
func JavaState(registry chunk.BlockRegistry, state NativeState) (BlockState, error) {
	id := state.JavaStateID
	if id == 0 {
		id = nativeJavaStateID(registry, state)
	}
	if id != 0 {
		name, properties, ok := javadata.BlockStateFromID(id)
		if !ok {
			return BlockState{}, fmt.Errorf("java state codec: invalid retained state ID %d", id)
		}
		return BlockState{Name: name, Properties: properties}, nil
	}
	return reverseNativeState(registry, state)
}

// WithJavaState attaches the known source state to its native projection. It
// omits metadata when the native layers already identify the complete source
// state. Actor-backed projections may supply their source state here as well.
func (state NativeState) WithJavaState(registry chunk.BlockRegistry, source BlockState) (NativeState, error) {
	id, err := javadata.BlockStateID(NormaliseBlockName(source.Name), source.Properties)
	if err != nil {
		return NativeState{}, err
	}
	state.JavaStateID = 0
	if nativeJavaStateID(registry, state) != id {
		state.JavaStateID = id
	}
	return state, nil
}

// nativeJavaStateID caches the canonical inverse by registry-local layer pair,
// avoiding repeated inverse-map allocation for every generated placement.
func nativeJavaStateID(registry chunk.BlockRegistry, state NativeState) uint32 {
	key := uint64(state.Block)<<32 | uint64(state.Liquid)
	basic, cache := registry.(*BasicBlockRegistry)
	if cache {
		if id, ok := basic.javaStateIDs.Load(key); ok {
			return id.(uint32)
		}
	}
	var id uint32
	if source, err := reverseNativeState(registry, state); err == nil {
		id, _ = javadata.BlockStateID(source.Name, source.Properties)
	}
	if cache {
		basic.javaStateIDs.Store(key, id)
	}
	return id
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
