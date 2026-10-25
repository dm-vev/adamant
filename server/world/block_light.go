package world

import (
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// blockLightCache holds the light properties of runtime IDs and Java states,
// packed with one added so that zero is unresolved.
type blockLightCache struct {
	table     []chunk.LightProperties
	tableOnce sync.Once
	dry       atomic.Pointer[[]atomic.Uint32]
	wet       sync.Map // Native layer pair -> packed properties.
	state     sync.Map // Java state ID -> packed properties.
}

func packLight(p chunk.LightProperties) uint32 {
	return (uint32(p.Emission) | uint32(p.Dampening)<<4 | uint32(p.Occlusion)<<8) + 1
}

func unpackLight(v uint32) chunk.LightProperties {
	v--
	return chunk.LightProperties{Emission: uint8(v & 15), Dampening: uint8(v >> 4 & 15), Occlusion: uint8(v >> 8 & 63)}
}

// LightProperties returns the light properties Java's light engine reads of
// a cell: those of its Java block state, or of the Bedrock blocks for native
// blocks without one.
func (br *BasicBlockRegistry) LightProperties(state, fg, bg uint32) chunk.LightProperties {
	if state != 0 {
		if v, ok := br.light.state.Load(state); ok {
			return unpackLight(v.(uint32))
		}
		p := br.javaLightProperties(state, fg, bg)
		br.light.state.Store(state, packLight(p))
		return p
	}
	if bg == br.airRID {
		slots := br.light.dry.Load()
		if slots == nil {
			fresh := make([]atomic.Uint32, br.BlockCount())
			br.light.dry.CompareAndSwap(nil, &fresh)
			slots = br.light.dry.Load()
		}
		if fg < uint32(len(*slots)) {
			slot := &(*slots)[fg]
			if v := slot.Load(); v != 0 {
				return unpackLight(v)
			}
			p := br.javaLightProperties(nativeJavaStateID(br, NativeState{Block: fg, Liquid: bg}), fg, bg)
			slot.Store(packLight(p))
			return p
		}
	}
	key := uint64(fg)<<32 | uint64(bg)
	if v, ok := br.light.wet.Load(key); ok {
		return unpackLight(v.(uint32))
	}
	p := br.javaLightProperties(nativeJavaStateID(br, NativeState{Block: fg, Liquid: bg}), fg, bg)
	br.light.wet.Store(key, packLight(p))
	return p
}

// DryLightProperties returns the light properties of every runtime ID with
// no background block, computed once.
func (br *BasicBlockRegistry) DryLightProperties() []chunk.LightProperties {
	br.light.tableOnce.Do(func() {
		table := make([]chunk.LightProperties, br.BlockCount())
		for rid := range table {
			table[rid] = br.LightProperties(0, uint32(rid), br.airRID)
		}
		br.light.table = table
	})
	return br.light.table
}

// javaLightProperties returns the light properties of the Java state passed,
// falling back on the light values of the Bedrock blocks if there is none.
func (br *BasicBlockRegistry) javaLightProperties(state, fg, bg uint32) chunk.LightProperties {
	if state != 0 {
		if shape, err := gamedata.BlockGenerationShapeID(state); err == nil {
			p := chunk.LightProperties{Emission: shape.LightEmission, Dampening: shape.LightDampening}
			if !shape.LightEmptyShape {
				p.Occlusion = shape.Occlusion
			}
			return p
		}
	}
	return chunk.LightProperties{
		Emission:  max(br.LightBlock(fg), br.LightBlock(bg)),
		Dampening: max(br.FilteringBlock(fg), br.FilteringBlock(bg)),
	}
}
