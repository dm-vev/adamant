package gamedata

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

type generationFaceProperty struct {
	name   string
	values []any
}

type generationFaceBlock struct {
	properties []generationFaceProperty
	masks      []uint64
	rigid      []uint8
	geometry   []uint16
}

type generationCollisionRect struct{ minU, minV, maxU, maxV float64 }
type generationCollisionGeometry struct {
	boundaries [6][]generationCollisionRect
	regions    [6][]generationCollisionRect
}

// GenerationShape describes fresh-generation source shapes (closed shulkers,
// no active moving pistons); it is not entity-dependent runtime collision parity.
// Face masks use DOWN, UP, NORTH, SOUTH, WEST, EAST order.
type GenerationShape struct {
	Attachment          uint8
	Sturdy              uint8
	Center              uint8
	Rigid               uint8 // SupportType.RIGID
	Occlusion           uint8 // full BlockState.getFaceOcclusionShape faces
	CollisionFull       bool
	CollisionEmpty      bool
	Replaceable         bool
	ConnectionException bool  // Block.isExceptionForConnection
	SignalSource        bool  // BlockState.isSignalSource
	RedstoneConductor   bool  // sampled with EmptyBlockGetter; inspect conductor predicate owner for contextual semantics
	FireCanBurn         bool  // FireBlock.canBurn for this state, including WATERLOGGED
	SolidRender         bool  // BlockState.isSolidRender, distinct from collision fullness
	OutlineFull         bool  // Block.isShapeFullBlock(BlockState.getShape), distinct from collision fullness
	LightEmission       uint8 // BlockState.getLightEmission
	LightDampening      uint8 // BlockState.getLightDampening
	LightEmptyShape     bool  // LightEngine.isEmptyShape: the shape takes no part in light occlusion
}

func generationBlockIndex(name string) (int, error) {
	namespace, block, qualified := strings.Cut(name, ":")
	if qualified {
		if namespace != "minecraft" {
			return 0, fmt.Errorf("generation faces: unsupported namespace %q", namespace)
		}
		name = block
	}
	index := sort.SearchStrings(solidBlockNames[:], name)
	if index == len(solidBlockNames) || solidBlockNames[index] != name {
		return 0, fmt.Errorf("generation faces: unknown block %q", name)
	}
	return index, nil
}

func generationStateIndex(index int, properties map[string]any) (int, error) {
	entry := &generationFaceBlocks[index]
	stateIndex := 0
	for _, property := range entry.properties {
		value, exists := properties[property.name]
		if !exists {
			value = defaultBlockProperties[index][property.name]
		}
		valueIndex := 0
		for valueIndex < len(property.values) && property.values[valueIndex] != value {
			valueIndex++
		}
		if valueIndex == len(property.values) {
			return 0, fmt.Errorf("generation faces: invalid %s.%s=%v", solidBlockNames[index], property.name, value)
		}
		stateIndex = stateIndex*len(property.values) + valueIndex
	}
	return stateIndex, nil
}

func generationShape(block, stateIndex int) GenerationShape {
	entry := &generationFaceBlocks[block]
	mask := entry.masks[stateIndex]
	return GenerationShape{
		Attachment: uint8(mask & 63), Sturdy: uint8(mask >> 6 & 63), Center: uint8(mask >> 12 & 63),
		Rigid:         entry.rigid[stateIndex],
		Occlusion:     uint8(mask >> 27 & 63),
		CollisionFull: mask&(1<<18) != 0, CollisionEmpty: mask&(1<<19) != 0, Replaceable: mask&(1<<20) != 0,
		ConnectionException: mask&(1<<21) != 0, SignalSource: mask&(1<<22) != 0,
		RedstoneConductor: mask&(1<<23) != 0, FireCanBurn: mask&(1<<24) != 0,
		SolidRender:   mask&(1<<25) != 0,
		OutlineFull:   mask&(1<<26) != 0,
		LightEmission: uint8(mask >> 33 & 15), LightDampening: uint8(mask >> 37 & 15),
		LightEmptyShape: mask&(1<<41) != 0,
	}
}

// BlockGenerationShape returns source state-sensitive support and collision properties.
func BlockGenerationShape(name string, properties map[string]any) (GenerationShape, error) {
	index, err := generationBlockIndex(name)
	if err != nil {
		return GenerationShape{}, err
	}
	stateIndex, err := generationStateIndex(index, properties)
	if err != nil {
		return GenerationShape{}, err
	}
	return generationShape(index, stateIndex), nil
}

// BlockGenerationShapeID returns the same shape metadata without materialising
// a property map from the canonical source state ID.
func BlockGenerationShapeID(id uint32) (GenerationShape, error) {
	block, ok := blockStateIndex(id)
	if !ok {
		return GenerationShape{}, fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	stateIndex, ok := generationStateIndexID(id, block)
	if !ok {
		return GenerationShape{}, fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	return generationShape(block, stateIndex), nil
}

func generationStateIndexID(id uint32, block int) (int, bool) {
	stateIndex := 0
	for _, property := range generationFaceBlocks[block].properties {
		domain, value, _, ok := blockStateProperty(id, property.name)
		if !ok {
			return 0, false
		}
		valueIndex := -1
		for i, candidate := range property.values {
			switch domain.kind {
			case 'b':
				if valueIndex < 0 && candidate.(bool) == (value != 0) {
					valueIndex = i
				}
			case 'i':
				if valueIndex < 0 && candidate.(int32) == int32(int64(domain.min)+int64(value)) {
					valueIndex = i
				}
			case 's':
				if valueIndex < 0 && candidate.(string) == domain.values[value] {
					valueIndex = i
				}
			}
		}
		if valueIndex < 0 {
			return 0, false
		}
		stateIndex = stateIndex*len(property.values) + valueIndex
	}
	return stateIndex, true
}

// GenerationCollisionFacesOcclude implements Shapes.mergedFaceOccludes for two
// fresh-generation source states. direction follows Minecraft Direction ordinal:
// DOWN, UP, NORTH, SOUTH, WEST, EAST. It combines the exact union of boundary
// rectangles, so complementary partial faces can fully occlude without either
// state having a full face. The successful path allocates no memory.
func GenerationCollisionFacesOcclude(source string, sourceProperties map[string]any, target string, targetProperties map[string]any, direction int) (bool, error) {
	if direction < 0 || direction >= 6 {
		return false, fmt.Errorf("generation faces: invalid direction %d", direction)
	}
	sourceIndex, err := generationBlockIndex(source)
	if err != nil {
		return false, err
	}
	targetIndex, err := generationBlockIndex(target)
	if err != nil {
		return false, err
	}
	sourceState, err := generationStateIndex(sourceIndex, sourceProperties)
	if err != nil {
		return false, err
	}
	targetState, err := generationStateIndex(targetIndex, targetProperties)
	if err != nil {
		return false, err
	}
	return generationCollisionFacesOcclude(sourceIndex, sourceState, targetIndex, targetState, direction), nil
}

// GenerationCollisionFacesOccludeIDs is the allocation-free canonical-state-ID
// variant of GenerationCollisionFacesOcclude.
func GenerationCollisionFacesOccludeIDs(sourceID, targetID uint32, direction int) (bool, error) {
	if direction < 0 || direction >= 6 {
		return false, fmt.Errorf("generation faces: invalid direction %d", direction)
	}
	sourceIndex, ok := blockStateIndex(sourceID)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", sourceID)
	}
	targetIndex, ok := blockStateIndex(targetID)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid target state ID %d", targetID)
	}
	sourceState, ok := generationStateIndexID(sourceID, sourceIndex)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", sourceID)
	}
	targetState, ok := generationStateIndexID(targetID, targetIndex)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid target state ID %d", targetID)
	}
	return generationCollisionFacesOcclude(sourceIndex, sourceState, targetIndex, targetState, direction), nil
}

func generationCollisionFacesOcclude(sourceIndex, sourceState, targetIndex, targetState, direction int) bool {
	opposite := direction ^ 1
	sourceGeometry := generationCollisionGeometries[generationFaceBlocks[sourceIndex].geometry[sourceState]]
	targetGeometry := generationCollisionGeometries[generationFaceBlocks[targetIndex].geometry[targetState]]
	return collisionRectanglesCover(sourceGeometry.boundaries[direction], targetGeometry.boundaries[opposite])
}

// GenerationCollisionBoundary is a read-only view of an exact source collision
// boundary. It allows native-only shapes to be compared without replacing the
// canonical side of a mixed boundary with its Bedrock model.
type GenerationCollisionBoundary struct {
	rectangles []generationCollisionRect
}

// Len returns the number of boundary rectangles.
func (b GenerationCollisionBoundary) Len() int { return len(b.rectangles) }

// Rectangle returns minU, minV, maxU, maxV. DOWN/UP use X,Z; NORTH/SOUTH
// use X,Y; WEST/EAST use Y,Z.
func (b GenerationCollisionBoundary) Rectangle(index int) [4]float64 {
	r := b.rectangles[index]
	return [4]float64{r.minU, r.minV, r.maxU, r.maxV}
}

// GenerationCollisionBoundaryID returns the boundary used by
// Shapes.mergedFaceOccludes, not the potentially interior getFaceShape region.
func GenerationCollisionBoundaryID(id uint32, direction int) (GenerationCollisionBoundary, error) {
	if direction < 0 || direction >= 6 {
		return GenerationCollisionBoundary{}, fmt.Errorf("generation faces: invalid direction %d", direction)
	}
	block, ok := blockStateIndex(id)
	if !ok {
		return GenerationCollisionBoundary{}, fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	state, ok := generationStateIndexID(id, block)
	if !ok {
		return GenerationCollisionBoundary{}, fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	geometry := &generationCollisionGeometries[generationFaceBlocks[block].geometry[state]]
	return GenerationCollisionBoundary{rectangles: geometry.boundaries[direction]}, nil
}

// GenerationCollisionFaceCoversRectangleID reports whether the exact Java
// collision getFaceShape(face) covers the requested planar rectangle. The
// rectangle is extruded through the unit normal axis; face coordinates follow
// Java's face plane: DOWN/UP X,Z; NORTH/SOUTH X,Y; WEST/EAST Y,Z.
func GenerationCollisionFaceCoversRectangleID(state uint32, face int, minU, minV, maxU, maxV float64) (bool, error) {
	if face < 0 || face >= 6 {
		return false, fmt.Errorf("generation faces: invalid direction %d", face)
	}
	if math.IsNaN(minU) || math.IsNaN(minV) || math.IsNaN(maxU) || math.IsNaN(maxV) ||
		math.IsInf(minU, 0) || math.IsInf(minV, 0) || math.IsInf(maxU, 0) || math.IsInf(maxV, 0) ||
		minU < 0 || minV < 0 || maxU > 1 || maxV > 1 || minU >= maxU || minV >= maxV {
		return false, fmt.Errorf("generation faces: invalid rectangle bounds")
	}
	block, ok := blockStateIndex(state)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", state)
	}
	stateIndex, ok := generationStateIndexID(state, block)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", state)
	}
	geometry := generationCollisionGeometries[generationFaceBlocks[block].geometry[stateIndex]]
	return collisionRectanglesCoverBounds(geometry.regions[face], minU, minV, maxU, maxV), nil
}

// GenerationCollisionFaceNonEmptyID matches collisionShape.getFaceShape(face).isEmpty().
func GenerationCollisionFaceNonEmptyID(state uint32, face int) (bool, error) {
	if face < 0 || face >= 6 {
		return false, fmt.Errorf("generation faces: invalid direction %d", face)
	}
	block, ok := blockStateIndex(state)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", state)
	}
	stateIndex, ok := generationStateIndexID(state, block)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", state)
	}
	geometry := generationCollisionGeometries[generationFaceBlocks[block].geometry[stateIndex]]
	return len(geometry.regions[face]) != 0, nil
}

// BlockExceptionForConnectionID matches Block.isExceptionForConnection.
func BlockExceptionForConnectionID(id uint32) (bool, error) {
	block, ok := blockStateIndex(id)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	stateIndex, ok := generationStateIndexID(id, block)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	return generationFaceBlocks[block].masks[stateIndex]&(1<<21) != 0, nil
}

// BlockSignalSourceID matches BlockState.isSignalSource().
func BlockSignalSourceID(id uint32) (bool, error) {
	block, ok := blockStateIndex(id)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	stateIndex, ok := generationStateIndexID(id, block)
	if !ok {
		return false, fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	return generationFaceBlocks[block].masks[stateIndex]&(1<<22) != 0, nil
}

// RedstoneConductorDefaultPredicateOwner names the Java class that configures
// the context-dependent default state.isCollisionShapeFullBlock predicate.
const RedstoneConductorDefaultPredicateOwner = "net.minecraft.world.level.block.state.BlockBehaviour"

// BlockRedstoneConductorID returns the Java conductor predicate sampled with
// EmptyBlockGetter/BlockPos.ZERO. Use its owner metadata to distinguish the
// contextual default predicate from configured static predicates.
func BlockRedstoneConductorID(id uint32) (bool, error) {
	shape, err := BlockGenerationShapeID(id)
	if err != nil {
		return false, err
	}
	return shape.RedstoneConductor, nil
}

func BlockRedstoneConductorPredicateOwnerID(id uint32) (string, error) {
	block, ok := blockStateIndex(id)
	if !ok {
		return "", fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	return blockConductorPredicateOwners[block], nil
}

// BlockCanBurnID matches FireBlock.canBurn(state), including its waterlogged
// source-state check and pinned FireBlock ignition-odds registry.
func BlockCanBurnID(id uint32) (bool, error) {
	shape, err := BlockGenerationShapeID(id)
	if err != nil {
		return false, err
	}
	return shape.FireCanBurn, nil
}

// BlockInstrumentID returns BlockState.instrument().getSerializedName().
func BlockInstrumentID(id uint32) (string, error) {
	block, ok := blockStateIndex(id)
	if !ok {
		return "", fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	return blockInstruments[block], nil
}

func collisionRectanglesCoverBounds(rectangles []generationCollisionRect, minU, minV, maxU, maxV float64) bool {
	x := minU
	for x < maxU {
		nextX := maxU
		for _, rect := range rectangles {
			if rect.minU > x && rect.minU < nextX {
				nextX = rect.minU
			}
			if rect.maxU > x && rect.maxU < nextX {
				nextX = rect.maxU
			}
		}
		if nextX <= x {
			return false
		}
		y := minV
		for y < maxV {
			nextY := y
			for _, rect := range rectangles {
				if rect.minU <= x && rect.maxU >= nextX && rect.minV <= y && rect.maxV > nextY {
					nextY = rect.maxV
				}
			}
			if nextY <= y {
				return false
			}
			if nextY > maxV {
				nextY = maxV
			}
			y = nextY
		}
		x = nextX
	}
	return true
}

func collisionRectanglesCover(first, second []generationCollisionRect) bool {
	x := 0.0
	for x < 1 {
		nextX := 1.0
		for _, rect := range first {
			if rect.minU > x && rect.minU < nextX {
				nextX = rect.minU
			}
			if rect.maxU > x && rect.maxU < nextX {
				nextX = rect.maxU
			}
		}
		for _, rect := range second {
			if rect.minU > x && rect.minU < nextX {
				nextX = rect.minU
			}
			if rect.maxU > x && rect.maxU < nextX {
				nextX = rect.maxU
			}
		}
		if nextX <= x {
			return false
		}
		y := 0.0
		for y < 1 {
			maxY := y
			for _, rect := range first {
				if rect.minU <= x && rect.maxU >= nextX && rect.minV <= y && rect.maxV > maxY {
					maxY = rect.maxV
				}
			}
			for _, rect := range second {
				if rect.minU <= x && rect.maxU >= nextX && rect.minV <= y && rect.maxV > maxY {
					maxY = rect.maxV
				}
			}
			if maxY <= y {
				return false
			}
			y = maxY
		}
		x = nextX
	}
	return true
}

// BlockUpdateShapeOwner returns the Java declaring class that implements this
// block's updateShape dispatch. The exact
// net.minecraft.world.level.block.state.BlockBehaviour owner identifies the
// inherited identity handler; every other value is a concrete override.
func BlockUpdateShapeOwner(name string) (string, error) {
	index, err := generationBlockIndex(name)
	if err != nil {
		return "", err
	}
	return blockUpdateShapeOwners[index], nil
}

// BlockUpdateShapeOwnerID returns the updateShape declaring class for a
// canonical source state without decoding its block name or properties.
func BlockUpdateShapeOwnerID(id uint32) (string, error) {
	block, ok := blockStateIndex(id)
	if !ok {
		return "", fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	return blockUpdateShapeOwners[block], nil
}

// BlockCanSurviveOwnerID returns the actual declaring class of canSurvive for
// the block in a canonical source state ID.
func BlockCanSurviveOwnerID(id uint32) (string, error) {
	block, ok := blockStateIndex(id)
	if !ok {
		return "", fmt.Errorf("generation faces: invalid source state ID %d", id)
	}
	return blockCanSurviveOwners[block], nil
}

// lightOcclusionState names a state with partial light occlusion faces and
// the indexes of their bitmaps in lightOcclusionBitmaps, zero for none.
type lightOcclusionState struct {
	name, properties string
	faces            [6]uint16
}

var (
	lightOcclusionOnce sync.Once
	lightOcclusionByID map[uint32][6]uint16
)

// BlockLightOcclusionFace returns the coverage on the 16x16 pixel grid of a
// face of the occlusion shape of a state, as BlockState.getFaceOcclusionShape,
// for faces that are neither empty nor full. Rows are V and columns U of
// the face plane: Y and Z for faces along X, X and Z along Y, X and Y along
// Z. ok is false for faces that are not partial.
func BlockLightOcclusionFace(id uint32, face int) (bits [4]uint64, ok bool) {
	lightOcclusionOnce.Do(func() {
		lightOcclusionByID = make(map[uint32][6]uint16, len(lightOcclusionStates))
		for _, state := range lightOcclusionStates {
			properties := map[string]any{}
			if state.properties != "" {
				for _, pair := range strings.Split(state.properties, ",") {
					// Values carry the kind of their property: s:, b: or i:.
					name, value, _ := strings.Cut(pair, "=")
					if len(value) > 2 && value[1] == ':' {
						value = value[2:]
					}
					properties[name] = value
				}
			}
			stateID, err := BlockStateID(state.name, properties)
			if err != nil {
				panic(fmt.Sprintf("light occlusion state %s[%s]: %v", state.name, state.properties, err))
			}
			lightOcclusionByID[stateID] = state.faces
		}
	})
	faces, ok := lightOcclusionByID[id]
	if !ok || face < 0 || face >= 6 || faces[face] == 0 {
		return bits, false
	}
	return lightOcclusionBitmaps[faces[face]], true
}
