package world

import (
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
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
// Both native IDs are registry-local. StateID retains source properties
// that cannot be recovered from those layers; zero means the projection is exact.
type NativeState struct {
	Block   uint32
	Liquid  uint32
	StateID uint32
}

// CanonicalID resolves a retained or exactly projected source identity
// without materialising its property map. Zero denotes a native-only state.
func (state NativeState) CanonicalID(registry chunk.BlockRegistry) uint32 {
	if state.StateID != 0 {
		return state.StateID
	}
	return nativeJavaStateID(registry, state)
}

// NativeStateAt reads both native layers and their retained source identity.
func NativeStateAt(c *chunk.Chunk, x uint8, y int16, z uint8) NativeState {
	return NativeState{Block: c.Block(x, y, z, 0), Liquid: c.Block(x, y, z, 1), StateID: c.StateID(x, y, z)}
}

// SetNativeState publishes both layers before attaching their source identity.
func SetNativeState(c *chunk.Chunk, x uint8, y int16, z uint8, state NativeState) {
	c.SetBlock(x, y, z, 0, state.Block)
	c.SetBlock(x, y, z, 1, state.Liquid)
	c.SetJavaStateID(x, y, z, state.StateID)
}

// BlockStateID reads a canonical source state without materialising its
// properties. Like Block, it may load its column. Out-of-height positions are air.
func (tx *Tx) BlockStateID(pos cube.Pos) (uint32, error) {
	id := tx.javaBlockStateID(pos)
	if _, ok := gamedata.BlockStateName(id); !ok {
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
	id := state.StateID
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
	return native.WithCanonicalState(registry, state)
}

// CanonicalState returns the Java block-state view of a native block and liquid
// layer, for Java-side tags and predicates.
func CanonicalState(registry chunk.BlockRegistry, state NativeState) (BlockState, error) {
	id := state.StateID
	if id == 0 {
		id = nativeJavaStateID(registry, state)
	}
	if id != 0 {
		name, properties, ok := gamedata.BlockStateFromID(id)
		if !ok {
			return BlockState{}, fmt.Errorf("java state codec: invalid retained state ID %d", id)
		}
		return BlockState{Name: name, Properties: properties}, nil
	}
	return reverseNativeState(registry, state)
}

// javaCanonicalStater is implemented by blocks whose Java state their
// native runtime ID does not identify, as Bedrock keeps part of it in their
// block actor, such as the colour of a bed.
type javaCanonicalStater interface {
	JavaCanonicalState() BlockState
}

// CanonicalStateOf returns the Java block state of a registered block without
// liquid.
func CanonicalStateOf(b Block) (BlockState, error) {
	if s, ok := b.(javaCanonicalStater); ok {
		return s.JavaCanonicalState(), nil
	}
	air := DefaultBlockRegistry.AirRuntimeID()
	return CanonicalState(DefaultBlockRegistry, NativeState{Block: BlockRuntimeID(b), Liquid: air})
}

// WithCanonicalState attaches the known source state to its native projection. It
// omits metadata when the native layers already identify the complete source
// state. Actor-backed projections may supply their source state here as well.
func (state NativeState) WithCanonicalState(registry chunk.BlockRegistry, source BlockState) (NativeState, error) {
	id, err := gamedata.BlockStateID(NormaliseBlockName(source.Name), source.Properties)
	if err != nil {
		return NativeState{}, err
	}
	state.StateID = 0
	if nativeJavaStateID(registry, state) != id {
		state.StateID = id
	}
	return state, nil
}

// nativeJavaStateID caches the canonical inverse by registry-local layer pair,
// avoiding repeated inverse-map allocation for every generated placement.
func nativeJavaStateID(registry chunk.BlockRegistry, state NativeState) uint32 {
	key := uint64(state.Block)<<32 | uint64(state.Liquid)
	basic, cache := registry.(*BasicBlockRegistry)
	var dry *atomic.Uint32
	if cache {
		if state.Liquid == basic.AirRuntimeID() {
			if dry = basic.dryJavaStateID(state.Block); dry != nil {
				if id := dry.Load(); id != 0 {
					return id - 1
				}
			}
		} else if id, ok := basic.javaStateIDs.Load(key); ok {
			return id.(uint32)
		}
	}
	var id uint32
	if source, err := reverseNativeState(registry, state); err == nil {
		id, _ = gamedata.BlockStateID(source.Name, source.Properties)
	}
	switch {
	case dry != nil:
		dry.Store(id + 1)
	case cache:
		basic.javaStateIDs.Store(key, id)
	}
	return id
}

// dryJavaStateID returns the cache slot of a primary runtime ID without a
// liquid layer, or nil for an ID outside the registry.
func (br *BasicBlockRegistry) dryJavaStateID(rid uint32) *atomic.Uint32 {
	ids := br.dryJavaStateIDs.Load()
	if ids == nil {
		fresh := make([]atomic.Uint32, br.BlockCount())
		br.dryJavaStateIDs.CompareAndSwap(nil, &fresh)
		ids = br.dryJavaStateIDs.Load()
	}
	if rid >= uint32(len(*ids)) {
		return nil
	}
	return &(*ids)[rid]
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
