package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
)

// IndirectShaper is a block that shapes the blocks of its kind diagonally
// above and below it, one block to a side, not only its neighbours, as
// redstone wire does, which climbs and descends blocks to the wire there.
// When such a block appears at a position or goes from it, the world sends
// the blocks that implement IndirectShaper at those eight positions a
// neighbour update from it, as Java's updateIndirectNeighbourShapes shapes
// them. ShapesIndirectNeighbours reports whether the block does so.
type IndirectShaper interface {
	ShapesIndirectNeighbours() bool
}

// shapesIndirectNeighbours reports whether b is an IndirectShaper that
// shapes the blocks at the corners of its neighbours.
func shapesIndirectNeighbours(b Block) bool {
	s, ok := b.(IndirectShaper)
	return ok && s.ShapesIndirectNeighbours()
}

// updateAroundSetBlock sends the neighbour updates of a block set at pos
// over old: those of its neighbours, and if one of the two shapes the
// blocks at the corners of its neighbours and the other does not, those of
// the IndirectShapers there.
func (tx *Tx) updateAroundSetBlock(pos cube.Pos, old, b Block) {
	tx.doBlockUpdatesAround(pos)
	if shapesIndirectNeighbours(old) != shapesIndirectNeighbours(b) {
		tx.updateIndirectNeighbours(pos)
	}
}

// updateIndirectNeighbours sends the IndirectShapers diagonally above and
// below pos, one block to a side, a neighbour update from pos.
func (tx *Tx) updateIndirectNeighbours(pos cube.Pos) {
	for _, face := range cube.HorizontalFaces() {
		side := pos.Side(face)
		for _, corner := range [...]cube.Pos{side.Side(cube.FaceDown), side.Side(cube.FaceUp)} {
			if b, ok := tx.blockLoaded(corner); ok && shapesIndirectNeighbours(b) {
				tx.updateNeighbour(corner, pos)
			}
		}
	}
}

// ShapeUpdate returns the block at pos as Java's BlockState.updateShape
// leaves it once the block at changed has changed, and true if that is
// another block. changed is a neighbour of pos, or a block diagonally above
// or below one of its horizontal neighbours, as an IndirectShaper tells,
// which shapes the block toward that neighbour.
//
// The block is shaped by the rules of its Java block, read from the Java
// states of the loaded blocks around it, and resolved to a native block.
// ShapeUpdate changes nothing, and runs none of the ticks or writes the
// rules of the Java block may make. It is false if the block or a block it
// reads has no Java state or is not loaded, if the shape would be air, as
// a block that cannot survive is, which is the native block's own to
// decide, or if the block has a block entity.
func (tx *Tx) ShapeUpdate(pos, changed cube.Pos) (Block, bool) {
	face, ok := shapeFaceToward(pos, changed)
	if !ok {
		return nil, false
	}
	c := javaShapeContext{tx: tx, pos: pos, pure: true}
	state := c.at(pos)
	if c.unknown {
		return nil, false
	}
	shaped := c.updateShape(state, face, c.at(pos.Side(face)))
	if c.unknown || shaped == state || javaIsAir(shaped) {
		return nil, false
	}
	return nativeShape(tx.World().conf.Blocks, shaped)
}

// shapeFaceToward returns the face of pos toward changed: that of a
// neighbour, or the horizontal one of a block diagonally above or below a
// horizontal neighbour.
func shapeFaceToward(pos, changed cube.Pos) (cube.Face, bool) {
	d := changed.Sub(pos)
	switch {
	case abs(d[0])+abs(d[1])+abs(d[2]) == 1:
		return pos.Face(changed), true
	case abs(d[1]) == 1 && abs(d[0])+abs(d[2]) == 1:
		return pos.Face(cube.Pos{changed[0], pos[1], changed[2]}), true
	}
	return 0, false
}

// ShapeFromNeighbours returns b as the blocks of src around pos shape it
// when it is placed there, as Java's Block.updateFromNeighbourShapes, and
// true if that is another block than b. src is the transaction b is placed
// in, or another BlockSource, such as the one a MissingStateReconstructor
// is given. A face whose shape would be air, as for a block that cannot
// survive, is passed over: whether b survives is the native block's own to
// decide. As ShapeUpdate, it changes nothing, and is false if b or a block
// it reads has no Java state or is not loaded, or b has a block entity.
// Over a BlockSource other than a transaction, it shapes only blocks whose
// Java rules read no more than the states of the blocks around them.
func ShapeFromNeighbours(src BlockSource, pos cube.Pos, b Block) (Block, bool) {
	c := javaShapeContext{pos: pos, pure: true}
	var registry BlockRegistry = DefaultBlockRegistry
	switch src := src.(type) {
	case *Tx:
		c.tx, registry = src, src.World().conf.Blocks
	case *loadedWorldSource:
		c.source, registry = src, src.w.conf.Blocks
	default:
		c.source = src
	}
	state := javaStateIDOfBlock(registry, b)
	if state == 0 {
		return b, false
	}
	shaped := state
	for _, face := range [...]cube.Face{cube.FaceWest, cube.FaceEast, cube.FaceNorth, cube.FaceSouth, cube.FaceDown, cube.FaceUp} {
		if next := c.updateShape(shaped, face, c.at(pos.Side(face))); !javaIsAir(next) {
			shaped = next
		}
	}
	if c.unknown || shaped == state {
		return b, false
	}
	if native, ok := nativeShape(registry, shaped); ok {
		return native, true
	}
	return b, false
}

// nativeShape resolves a shaped Java state to the native block of a
// registry, false if it has none or has a block entity.
func nativeShape(registry BlockRegistry, id uint32) (Block, bool) {
	native, ok := NativeState{}, false
	if basic, cached := registry.(*BasicBlockRegistry); cached {
		native, ok = basic.resolvedJavaState(id)
	}
	if !ok {
		if native, ok = resolveJavaStateID(registry, id); !ok {
			return nil, false
		}
	}
	if registry.NBTBlock(native.Block) {
		return nil, false
	}
	return registry.BlockByRuntimeIDOrAir(native.Block), true
}

// resolveJavaStateID resolves a Java state as ResolveStateID does, false
// where ResolveStateID would panic.
func resolveJavaStateID(registry BlockRegistry, id uint32) (NativeState, bool) {
	name, properties, ok := gamedata.BlockStateFromID(id)
	if !ok {
		return NativeState{}, false
	}
	native, err := ResolveState(registry, BlockState{Name: name, Properties: properties})
	return native, err == nil
}

// javaStateIDOfBlock returns the Java state of a block of a registry
// without a liquid, 0 if it has none.
func javaStateIDOfBlock(registry BlockRegistry, b Block) uint32 {
	if s, ok := b.(javaCanonicalStater); ok {
		state := s.JavaCanonicalState()
		id, err := gamedata.BlockStateID(NormaliseBlockName(state.Name), state.Properties)
		if err != nil {
			return 0
		}
		return id
	}
	air := registry.AirRuntimeID()
	return nativeJavaStateID(registry, NativeState{Block: registry.BlockRuntimeID(b), Liquid: air})
}

// pureAt reads the Java state at pos for a pure shape: from the source if
// it is set, else from the loaded blocks of the transaction. A block that
// has none, or is not loaded, reads as air and sets unknown.
func (c *javaShapeContext) pureAt(pos cube.Pos) uint32 {
	var (
		id     uint32
		loaded bool
	)
	switch src := c.source.(type) {
	case nil:
		id, loaded = c.tx.javaStateIDLoaded(pos)
	case *loadedWorldSource:
		id, loaded = src.javaStateID(pos)
	default:
		id, loaded = javaStateIDOfBlock(DefaultBlockRegistry, src.Block(pos)), true
	}
	if _, known := gamedata.BlockStateName(id); !loaded || !known {
		c.unknown = true
		return javaAir()
	}
	return id
}

// javaStateIDLoaded returns the Java state of the block at pos as
// javaBlockStateID does, without loading its column: false if the column is
// not loaded and ready.
func (tx *Tx) javaStateIDLoaded(pos cube.Pos) (uint32, bool) {
	w := tx.w
	if pos.OutOfBounds(w.ra) {
		return w.javaStateIDInColumn(nil, pos), true
	}
	c, ok := tx.readyColumn(chunkPosFromBlockPos(pos))
	if !ok {
		return 0, false
	}
	return w.javaStateIDInColumn(c, pos), true
}

// javaStateID returns the Java state of the loaded block at pos, as Block
// returns the block, false if it is not loaded.
func (s *loadedWorldSource) javaStateID(pos cube.Pos) (uint32, bool) {
	if pos.OutOfBounds(s.w.ra) {
		return s.w.javaStateIDInColumn(nil, pos), true
	}
	// Block marks the source incomplete where the block is not loaded, or
	// waits for a reconstruction of its own.
	s.Block(pos)
	if _, ok := s.w.blockLoaded(pos); !ok {
		return 0, false
	}
	return s.w.javaStateIDInColumn(s.w.chunks[chunkPosFromBlockPos(pos)], pos), true
}
