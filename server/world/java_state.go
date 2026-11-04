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
	b, liquid, id := c.Native(x, y, z)
	return NativeState{Block: b, Liquid: liquid, StateID: id}
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

// JavaBlockState returns the Java block state that game logic reads at pos.
// Unlike BlockStateID it never fails on world data (gameJavaState):
//   - a block holding a liquid Java cannot waterlog it with, as a door or a
//     carpet in water, reads as the block alone, which is the block Java
//     has there;
//   - a native state Java has no state like, as a torch facing "unknown" or
//     a ceiling skull, reads as the default state of the Java block of the
//     same name, which has its tags;
//   - a block of no Java block at all, as an Education Edition torch or a
//     block the registry does not know, returns ok false. Game logic reads
//     such a block by its collision shape, in no tag and harmless.
//
// Like Block, it may load its column. Out-of-height positions are air.
func (tx *Tx) JavaBlockState(pos cube.Pos) (id uint32, ok bool) {
	var c *Column
	if !pos.OutOfBounds(tx.Range()) {
		c = tx.chunk(chunkPosFromBlockPos(pos))
	}
	w := tx.World()
	if id = w.javaStateIDInColumn(c, pos); validJavaState(id) || c == nil {
		return id, validJavaState(id)
	}
	return gameJavaState(w.conf.Blocks, NativeStateAt(c.Chunk, uint8(pos[0]&15), int16(pos[1]), uint8(pos[2]&15)))
}

// gameJavaState returns the Java block state that game logic reads for a
// native state, as JavaBlockState describes: its canonical state, else that
// of its block without the liquid it holds, else the default state of the
// Java block of the same name. ok is false for a block of no Java block.
func gameJavaState(registry chunk.BlockRegistry, state NativeState) (uint32, bool) {
	if id := state.CanonicalID(registry); validJavaState(id) {
		return id, true
	}
	if air := registry.AirRuntimeID(); state.Liquid != air {
		if id := nativeJavaStateID(registry, NativeState{Block: state.Block, Liquid: air}); validJavaState(id) {
			return id, true
		}
	}
	name, _, found := registry.RuntimeIDToState(state.Block)
	if !found {
		return 0, false
	}
	if java, renamed := nativeToJavaBlockNames[name]; renamed {
		name = java
	}
	id, err := gamedata.BlockStateID(name, nil)
	return id, err == nil
}

// validJavaState reports whether id is a Java block state gamedata knows.
func validJavaState(id uint32) bool {
	_, ok := gamedata.BlockStateName(id)
	return ok
}

// javaBlockStateID returns zero for native-only states without inventing a
// canonical identity. Native capability adapters may handle these separately.
func (tx *Tx) javaBlockStateID(pos cube.Pos) uint32 {
	var c *Column
	if !pos.OutOfBounds(tx.Range()) {
		c = tx.chunk(chunkPosFromBlockPos(pos))
	}
	return tx.World().javaStateIDInColumn(c, pos)
}

// javaStateIDInColumn returns the canonical source state of the block at pos
// in the column c holding it, or of air if c is nil.
func (w *World) javaStateIDInColumn(c *Column, pos cube.Pos) uint32 {
	registry := w.conf.Blocks
	state := NativeState{Block: registry.AirRuntimeID(), Liquid: registry.AirRuntimeID()}
	if c != nil {
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
// registry: the rules of java_state_rules.go map each one. A
// BasicBlockRegistry resolves each Java state once and then looks it up by
// its ID, for states whose properties have the types gamedata gives them.
func ResolveState(registry chunk.BlockRegistry, state BlockState) (NativeState, error) {
	basic, cached := registry.(*BasicBlockRegistry)
	var id uint32
	if cached {
		if id, cached = gamedata.CanonicalBlockStateID(state.Name, state.Properties); cached {
			if native, ok := basic.resolvedJavaState(id); ok {
				return native, nil
			}
		}
	}
	native, err := resolveJavaState(registry, state)
	if err != nil {
		return NativeState{}, err
	}
	if native, err = native.WithCanonicalState(registry, state); err == nil && cached {
		basic.storeResolvedJavaState(id, native)
	}
	return native, err
}

// The bits of a resolved Java state in BasicBlockRegistry.javaNativeStates:
// both layers, whether the state retains its source ID, and whether it was
// resolved at all.
const (
	resolvedLayerMask = 1<<31 - 1
	resolvedRetained  = 1 << 62
	resolvedKnown     = 1 << 63
)

// resolvedJavaState returns what ResolveState stored for a Java state ID.
func (br *BasicBlockRegistry) resolvedJavaState(id uint32) (NativeState, bool) {
	slots := br.javaNativeStates.Load()
	if slots == nil || id >= uint32(len(*slots)) {
		return NativeState{}, false
	}
	v := (*slots)[id].Load()
	if v&resolvedKnown == 0 {
		return NativeState{}, false
	}
	state := NativeState{Block: uint32(v & resolvedLayerMask), Liquid: uint32(v >> 31 & resolvedLayerMask)}
	if v&resolvedRetained != 0 {
		state.StateID = id
	}
	return state, true
}

// storeResolvedJavaState stores the native state of a Java state ID. A state
// retains at most its own ID, which is all a slot can hold.
func (br *BasicBlockRegistry) storeResolvedJavaState(id uint32, state NativeState) {
	if state.Block > resolvedLayerMask || state.Liquid > resolvedLayerMask || state.StateID != 0 && state.StateID != id {
		return
	}
	slots := br.javaNativeStates.Load()
	if slots == nil {
		fresh := make([]atomic.Uint64, gamedata.BlockStateCount()+1)
		br.javaNativeStates.CompareAndSwap(nil, &fresh)
		slots = br.javaNativeStates.Load()
	}
	if id >= uint32(len(*slots)) {
		return
	}
	v := uint64(resolvedKnown) | uint64(state.Liquid)<<31 | uint64(state.Block)
	if state.StateID != 0 {
		v |= resolvedRetained
	}
	(*slots)[id].Store(v)
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
