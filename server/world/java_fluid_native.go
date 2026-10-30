package world

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

// javaFluidBlock keeps canonical source states and native-only blocks distinct.
// Native extensions use their actual liquid capabilities and positional model;
// they are never assigned a fabricated Java air/stone identity.
type javaFluidBlock struct {
	state  uint32
	native Block
}

// NativeLiquidBoundaryOpen preserves native-only extension blocks' positional
// collision and SideClosed contracts when a liquid uses its own tick handler.
// Registered vanilla blocks keep that handler's native flow rules; mixed
// boundaries use the existing shared aperture calculation without inventing a
// canonical source identity for an extension.
func (tx *Tx) NativeLiquidBoundaryOpen(from, to cube.Pos) bool {
	c := javaShapeContext{tx: tx, pos: from}
	first, _ := javaFluidStateAt(&c, from)
	second, _ := javaFluidStateAt(&c, to)
	if first.native == nil && second.native == nil {
		return true
	}
	for _, face := range cube.Faces() {
		if from.Side(face) == to {
			return javaFluidMixedWallPass(&c, first, second, from, int(face))
		}
	}
	return false
}

func (b javaFluidBlock) is(name string) bool {
	return b.native == nil && javaName(b.state) == name
}

func (b javaFluidBlock) tag(c *javaShapeContext, tag string) bool {
	return b.native == nil && c.tag(b.state, tag)
}

func (b javaFluidBlock) solid(c *javaShapeContext, pos cube.Pos) bool {
	if b.native != nil {
		// Native source conversion uses the existing upper support-face contract.
		return b.native.Model().FaceSolid(pos, cube.FaceUp, c.tx)
	}
	return c.solid(b.state)
}

func javaFluidStateAt(c *javaShapeContext, pos cube.Pos) (javaFluidBlock, javaFluidState) {
	if c.generation != nil {
		id := c.at(pos)
		return javaFluidBlock{state: id}, javaFluidForState(id)
	}
	id := c.tx.javaBlockStateID(pos)
	if id != 0 {
		return javaFluidBlock{state: id}, javaFluidForState(id)
	}
	state := javaFluidBlock{native: c.tx.Block(pos)}
	liquid, ok := c.tx.Liquid(pos)
	if !ok {
		return state, javaFluidState{}
	}
	kind := javaFluidEmpty
	switch liquid.LiquidType() {
	case "water":
		kind = javaFluidWater
	case "lava":
		kind = javaFluidLava
	default:
		return state, javaFluidState{}
	}
	return state, javaFluidState{kind: kind, amount: int32(liquid.LiquidDepth()), falling: liquid.LiquidFalling(), source: liquid.LiquidDepth() == 8 && !liquid.LiquidFalling()}
}

func javaFluidSet(c *javaShapeContext, pos cube.Pos, block javaFluidBlock, fluid javaFluidState) javaFluidBlock {
	if block.native != nil {
		if _, foregroundLiquid := block.native.(Liquid); !foregroundLiquid {
			c.tx.SetLiquid(pos, c.nativeFluid(fluid))
			return block
		}
	}
	state := javaLegacyFluidBlock(fluid)
	c.set(pos, state, 3)
	return javaFluidBlock{state: state}
}

// javaFluidBoundary exposes a canonical boundary or a native model's boundary
// without copying either into a temporary rectangle slice.
type javaFluidBoundary struct {
	source     gamedata.GenerationCollisionBoundary
	boxes      []cube.BBox
	axis, u, v int
	positive   bool
}

func javaFluidBlockBoundary(c *javaShapeContext, state javaFluidBlock, pos cube.Pos, direction int) javaFluidBoundary {
	if state.native == nil {
		boundary, err := gamedata.GenerationCollisionBoundaryID(state.state, direction)
		if err != nil {
			panic(err)
		}
		return javaFluidBoundary{source: boundary}
	}
	boundary := javaFluidBoundary{boxes: state.native.Model().BBox(pos, c.tx), positive: direction&1 != 0}
	switch direction / 2 {
	case 0:
		boundary.axis, boundary.u, boundary.v = 1, 0, 2
	case 1:
		boundary.axis, boundary.u, boundary.v = 2, 0, 1
	case 2:
		boundary.axis, boundary.u, boundary.v = 0, 1, 2
	default:
		panic("invalid fluid boundary direction")
	}
	// Shapes.mergedFaceOccludes selects the outermost slice only when the
	// whole shape's extreme lies on the shared block face. A shape extending
	// beyond that face must not be sliced as if its maximum were exactly one.
	extreme := math.Inf(1)
	if boundary.positive {
		extreme = math.Inf(-1)
	}
	for _, box := range boundary.boxes {
		if boundary.positive {
			extreme = max(extreme, box.Max()[boundary.axis])
		} else {
			extreme = min(extreme, box.Min()[boundary.axis])
		}
	}
	edge := 0.0
	if boundary.positive {
		edge = 1
	}
	if math.Abs(extreme-edge) > 1e-7 {
		boundary.boxes = nil
	}
	return boundary
}

func (b *javaFluidBoundary) len() int {
	return b.source.Len() + len(b.boxes)
}

func (b *javaFluidBoundary) rectangle(index int) ([4]float64, bool) {
	if b.boxes == nil {
		return b.source.Rectangle(index), true
	}
	box := b.boxes[index]
	low, high := box.Min(), box.Max()
	if b.positive {
		if math.Abs(high[b.axis]-1) > 1e-7 {
			return [4]float64{}, false
		}
	} else if math.Abs(low[b.axis]) > 1e-7 {
		return [4]float64{}, false
	}
	return [4]float64{low[b.u], low[b.v], high[b.u], high[b.v]}, true
}

func javaFluidMixedWallPass(c *javaShapeContext, from, to javaFluidBlock, pos cube.Pos, direction int) bool {
	next := pos.Side(cube.Face(direction))
	if displacer, ok := from.native.(LiquidDisplacer); ok && displacer.SideClosed(pos, next, c.tx) {
		return false
	}
	if displacer, ok := to.native.(LiquidDisplacer); ok && displacer.SideClosed(next, pos, c.tx) {
		return false
	}
	first := javaFluidBlockBoundary(c, from, pos, direction)
	second := javaFluidBlockBoundary(c, to, next, direction^1)
	// Sweep each distinct U edge, then cover V using the union of both faces.
	// Complementary half-faces close the wall even if neither is full alone.
	boundaries := [2]*javaFluidBoundary{&first, &second}
	for u := 0.0; u < 1; {
		nextU := 1.0
		for _, boundary := range boundaries {
			for i := range boundary.len() {
				r, ok := boundary.rectangle(i)
				if !ok {
					continue
				}
				if r[0] > u && r[0] < nextU {
					nextU = r[0]
				}
				if r[2] > u && r[2] < nextU {
					nextU = r[2]
				}
			}
		}
		for v := 0.0; v < 1; {
			nextV := v
			for _, boundary := range boundaries {
				for i := range boundary.len() {
					r, ok := boundary.rectangle(i)
					if ok && r[0] <= u && r[2] >= nextU && r[1] <= v && r[3] > nextV {
						nextV = r[3]
					}
				}
			}
			if nextV <= v {
				return true
			}
			v = nextV
		}
		u = nextU
	}
	return false
}
