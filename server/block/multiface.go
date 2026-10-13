package block

import (
	"math/bits"
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// MultiFace is the set of block faces a multi-face block (glow lichen, sculk
// vein, resin clump) covers. Its value is the Bedrock multi_face_direction_bits
// state: bit 0 down, 1 up, 2 south, 3 west, 4 north and 5 east.
type MultiFace uint8

// multiFaceBit returns the bit of face in a MultiFace.
func multiFaceBit(face cube.Face) MultiFace {
	switch face {
	case cube.FaceDown:
		return 1 << 0
	case cube.FaceUp:
		return 1 << 1
	case cube.FaceSouth:
		return 1 << 2
	case cube.FaceWest:
		return 1 << 3
	case cube.FaceNorth:
		return 1 << 4
	default:
		return 1 << 5
	}
}

// Has reports whether the block covers the side of its space toward face.
func (m MultiFace) Has(face cube.Face) bool { return m&multiFaceBit(face) != 0 }

// With returns m with face set or cleared.
func (m MultiFace) With(face cube.Face, covered bool) MultiFace {
	if covered {
		return m | multiFaceBit(face)
	}
	return m &^ multiFaceBit(face)
}

// Count returns the number of covered faces.
func (m MultiFace) Count() int { return bits.OnesCount8(uint8(m)) }

// Faces returns the covered faces in Java Direction order.
func (m MultiFace) Faces() []cube.Face {
	faces := make([]cube.Face, 0, m.Count())
	for _, face := range cube.Faces() {
		if m.Has(face) {
			faces = append(faces, face)
		}
	}
	return faces
}

// allMultiFaces returns every MultiFace value, including the empty set.
func allMultiFaces() []MultiFace {
	faces := make([]MultiFace, 64)
	for i := range faces {
		faces[i] = MultiFace(i)
	}
	return faces
}

// multifaceCanAttachTo ports MultifaceBlock.canAttachTo: the neighbour toward
// face must present a full side to pos.
func multifaceCanAttachTo(tx *world.Tx, pos cube.Pos, face cube.Face) bool {
	neighbour := pos.Side(face)
	return tx.Block(neighbour).Model().FaceSolid(neighbour, face.Opposite(), tx)
}

// multifaceSurvivingFaces ports MultifaceBlock.updateShape for every face at
// once: faces whose support is gone are dropped.
func multifaceSurvivingFaces(tx *world.Tx, pos cube.Pos, faces MultiFace) MultiFace {
	for _, face := range faces.Faces() {
		if !multifaceCanAttachTo(tx, pos, face) {
			faces = faces.With(face, false)
		}
	}
	return faces
}

// multifaceBlock is a block with a MultiFace state that a multifaceSpreader
// grows.
type multifaceBlock interface {
	world.Block
	multiFaces() MultiFace
	withMultiFaces(MultiFace) world.Block
}

// multifaceSpreadType ports MultifaceSpreader.SpreadType.
type multifaceSpreadType uint8

const (
	spreadSamePosition multifaceSpreadType = iota
	spreadSamePlane
	spreadWrapAround
)

var defaultMultifaceSpreadOrder = []multifaceSpreadType{spreadSamePosition, spreadSamePlane, spreadWrapAround}

// multifaceSpreadPos is a block position with the face to cover there.
type multifaceSpreadPos struct {
	pos  cube.Pos
	face cube.Face
}

func (t multifaceSpreadType) spreadPos(pos cube.Pos, spreadDirection, fromFace cube.Face) multifaceSpreadPos {
	switch t {
	case spreadSamePosition:
		return multifaceSpreadPos{pos: pos, face: spreadDirection}
	case spreadSamePlane:
		return multifaceSpreadPos{pos: pos.Side(spreadDirection), face: fromFace}
	default:
		return multifaceSpreadPos{pos: pos.Side(spreadDirection).Side(fromFace), face: spreadDirection.Opposite()}
	}
}

// multifaceSpreader ports MultifaceSpreader with its SpreadConfig.
type multifaceSpreader[T multifaceBlock] struct {
	// block is the empty-faced block placed by the spreader.
	block T
	// types is the spread order tried for every direction.
	types []multifaceSpreadType
	// stateCanBeReplaced overrides DefaultSpreaderConfig.stateCanBeReplaced.
	stateCanBeReplaced func(tx *world.Tx, source, placement cube.Pos, face cube.Face, existing world.Block) bool
	// otherBlockValidAsSource ports SpreadConfig.isOtherBlockValidAsSource.
	otherBlockValidAsSource func(world.Block) bool
}

// is reports the faces of b if it is the spreader's block.
func (s multifaceSpreader[T]) is(b world.Block) (MultiFace, bool) {
	m, ok := b.(T)
	if !ok {
		return 0, false
	}
	return m.multiFaces(), true
}

func (s multifaceSpreader[T]) hasFace(b world.Block, face cube.Face) bool {
	faces, ok := s.is(b)
	return ok && faces.Has(face)
}

func (s multifaceSpreader[T]) otherValid(b world.Block) bool {
	return s.otherBlockValidAsSource != nil && s.otherBlockValidAsSource(b)
}

func (s multifaceSpreader[T]) canSpreadFrom(b world.Block, face cube.Face) bool {
	return s.otherValid(b) || s.hasFace(b, face)
}

// defaultStateCanBeReplaced ports DefaultSpreaderConfig.stateCanBeReplaced.
func (s multifaceSpreader[T]) defaultStateCanBeReplaced(existing world.Block) bool {
	if _, ok := existing.(Air); ok {
		return true
	}
	if _, ok := s.is(existing); ok {
		return true
	}
	water, ok := existing.(Water)
	return ok && water.Depth == 8 && !water.Falling
}

// isValidStateForPlacement ports MultifaceBlock.isValidStateForPlacement.
func (s multifaceSpreader[T]) isValidStateForPlacement(tx *world.Tx, existing world.Block, pos cube.Pos, face cube.Face) bool {
	if s.hasFace(existing, face) {
		return false
	}
	return multifaceCanAttachTo(tx, pos, face)
}

// stateForPlacement ports MultifaceBlock.getStateForPlacement.
func (s multifaceSpreader[T]) stateForPlacement(tx *world.Tx, existing world.Block, pos cube.Pos, face cube.Face) (world.Block, bool) {
	if !s.isValidStateForPlacement(tx, existing, pos, face) {
		return nil, false
	}
	faces, _ := s.is(existing)
	return s.block.withMultiFaces(faces.With(face, true)), true
}

func (s multifaceSpreader[T]) canSpreadInto(tx *world.Tx, source cube.Pos, spread multifaceSpreadPos) bool {
	if spread.pos.OutOfBounds(tx.Range()) {
		return false
	}
	existing := tx.Block(spread.pos)
	replaceable := s.defaultStateCanBeReplaced(existing)
	if s.stateCanBeReplaced != nil {
		replaceable = s.stateCanBeReplaced(tx, source, spread.pos, spread.face, existing)
	}
	return replaceable && s.isValidStateForPlacement(tx, existing, spread.pos, spread.face)
}

// spreadTarget ports getSpreadFromFaceTowardDirection.
func (s multifaceSpreader[T]) spreadTarget(tx *world.Tx, state world.Block, pos cube.Pos, startingFace, spreadDirection cube.Face) (multifaceSpreadPos, bool) {
	if spreadDirection.Axis() == startingFace.Axis() {
		return multifaceSpreadPos{}, false
	}
	if !s.otherValid(state) && (!s.hasFace(state, startingFace) || s.hasFace(state, spreadDirection)) {
		return multifaceSpreadPos{}, false
	}
	for _, t := range s.types {
		spread := t.spreadPos(pos, spreadDirection, startingFace)
		if s.canSpreadInto(tx, pos, spread) {
			return spread, true
		}
	}
	return multifaceSpreadPos{}, false
}

// spreadToFace ports MultifaceSpreader.spreadToFace with SpreadConfig.placeBlock.
func (s multifaceSpreader[T]) spreadToFace(tx *world.Tx, spread multifaceSpreadPos) bool {
	b, ok := s.stateForPlacement(tx, tx.Block(spread.pos), spread.pos, spread.face)
	if !ok {
		return false
	}
	tx.SetBlock(spread.pos, b, nil)
	return true
}

func (s multifaceSpreader[T]) spreadFromFaceTowardDirection(tx *world.Tx, state world.Block, pos cube.Pos, fromFace, spreadDirection cube.Face) bool {
	spread, ok := s.spreadTarget(tx, state, pos, fromFace, spreadDirection)
	return ok && s.spreadToFace(tx, spread)
}

// spreadAll ports MultifaceSpreader.spreadAll and returns the number of faces
// placed.
func (s multifaceSpreader[T]) spreadAll(tx *world.Tx, state world.Block, pos cube.Pos) int {
	n := 0
	for _, face := range cube.Faces() {
		if !s.canSpreadFrom(state, face) {
			continue
		}
		for _, direction := range cube.Faces() {
			if s.spreadFromFaceTowardDirection(tx, state, pos, face, direction) {
				n++
			}
		}
	}
	return n
}

// canSpreadInAnyDirection ports MultifaceSpreader.canSpreadInAnyDirection.
func (s multifaceSpreader[T]) canSpreadInAnyDirection(tx *world.Tx, state world.Block, pos cube.Pos, startingFace cube.Face) bool {
	for _, direction := range cube.Faces() {
		if _, ok := s.spreadTarget(tx, state, pos, startingFace, direction); ok {
			return true
		}
	}
	return false
}

// spreadFromRandomFaceTowardRandomDirection ports the bone meal spread of
// glow lichen.
func (s multifaceSpreader[T]) spreadFromRandomFaceTowardRandomDirection(tx *world.Tx, state world.Block, pos cube.Pos, r *rand.Rand) bool {
	for _, face := range shuffledFaces(r) {
		if !s.canSpreadFrom(state, face) {
			continue
		}
		for _, direction := range shuffledFaces(r) {
			if s.spreadFromFaceTowardDirection(tx, state, pos, face, direction) {
				return true
			}
		}
	}
	return false
}

// shuffledFaces ports Direction.allShuffled. A nil r uses the global source.
func shuffledFaces(r *rand.Rand) []cube.Face {
	faces := slices.Clone(cube.Faces())
	swap := func(i, j int) { faces[i], faces[j] = faces[j], faces[i] }
	if r == nil {
		rand.Shuffle(len(faces), swap)
	} else {
		r.Shuffle(len(faces), swap)
	}
	return faces
}
