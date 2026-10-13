package world

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// JavaGenerationShapeAccess is the WorldGenRegion view used by source shape
// callbacks. It deliberately does not emulate a live World or neighbour updates.
// Implementations retain canonical states, loaded actors and proto-chunk tick
// reservations, including those outside the chunk currently being published.
// JavaRandom is the region stream, not the structure/feature placement stream.
// SetJavaBlockState observes the source write radius and update flags.
// Light returns source raw brightness with zero sky darkening.
// JavaSignal's face points out of the signaling block towards its receiver.
// JavaHiveHasOccupants reads the currently loaded generation hive actor.
// JavaFluidTickDelay accepts minecraft:water or minecraft:lava.
// Scheduled fluid type names may also be their flowing variants.
type JavaGenerationShapeAccess interface {
	JavaBlockStateID(cube.Pos) (uint32, error)
	Range() cube.Range
	JavaRandom() *java26.Random
	Light(cube.Pos) uint8
	JavaFluidTickDelay(name string) int32
	ScheduleJavaTick(pos cube.Pos, kind chunk.ScheduledTickKind, name string, delay int32)
	HasJavaBlockTick(pos cube.Pos, name string) bool
	JavaSignal(pos cube.Pos, face cube.Face) int
	JavaHiveHasOccupants(pos cube.Pos) bool
	SetJavaBlockState(pos cube.Pos, state uint32, flags int) bool
}

// JavaGenerationBlockCanSurvive evaluates a proposed source state against the
// current generation view without placing it or running neighbour updates.
func JavaGenerationBlockCanSurvive(access JavaGenerationShapeAccess, state uint32, pos cube.Pos) bool {
	context := javaShapeContext{generation: access, pos: pos}
	return context.canSurvive(state, pos)
}

// PlaceJavaGenerationSimpleBlock applies SimpleBlockFeature after its state
// provider has sampled a state. Mossy carpet consumes the region random stream.
func PlaceJavaGenerationSimpleBlock(access JavaGenerationShapeAccess, state uint32, pos cube.Pos, scheduleTick bool) bool {
	context := javaShapeContext{generation: access, pos: pos}
	owner, err := javadata.BlockCanSurviveOwnerID(state)
	if err != nil {
		panic(err)
	}
	owner = owner[strings.LastIndexByte(owner, '.')+1:]
	if !context.ownerSurvival(owner, state, pos) {
		return false
	}
	// These are the pinned source canSurvive owners of DoublePlantBlock and
	// its subclasses; no second block-name classification table is needed.
	switch owner {
	case "MossyCarpetBlock":
		placeJavaGenerationMossyCarpet(&context, pos)
	case "DoublePlantBlock", "PitcherCropBlock", "SmallDripleafBlock", "TallSeagrassBlock":
		abovePos := pos.Side(cube.FaceUp)
		above := context.at(abovePos)
		if !javaIsAir(above) && (context.fluid(state) != context.fluid(above) || !context.replaceable(above)) {
			return false
		}
		lower := javaWith(state, "half", "lower")
		upper := javaWith(state, "half", "upper")
		if _, hasWaterlogged := javadata.BlockStateBool(state, "waterlogged"); hasWaterlogged {
			for _, placement := range [...]struct {
				pos   cube.Pos
				state uint32
			}{{pos, lower}, {abovePos, upper}} {
				fluid := context.fluid(context.at(placement.pos))
				placement.state = javaWith(placement.state, "waterlogged", fluid.kind == javaFluidWater)
				access.SetJavaBlockState(placement.pos, placement.state, 2)
			}
		} else {
			access.SetJavaBlockState(pos, lower, 2)
			access.SetJavaBlockState(abovePos, upper, 2)
		}
	default:
		access.SetJavaBlockState(pos, state, 2)
	}

	if scheduleTick {
		current := context.at(pos)
		access.ScheduleJavaTick(pos, chunk.BlockTick, "minecraft:"+javaName(current), 1)
	}
	return true
}

func placeJavaGenerationMossyCarpet(context *javaShapeContext, pos cube.Pos) {
	base := javaDefault("pale_moss_carpet")
	context.pos = pos
	base = javaPlant06UpdateMossyCarpetSides(context, base, true)
	context.generation.SetJavaBlockState(pos, base, 2)

	abovePos := pos.Side(cube.FaceUp)
	abovePrevious := context.at(abovePos)
	isMossyCarpet := javaName(abovePrevious) == "pale_moss_carpet"
	if isMossyCarpet {
		if javaBool(abovePrevious, "bottom") {
			return
		}
	} else if !context.replaceable(abovePrevious) {
		return
	}

	context.pos = abovePos
	topper := javaWith(javaDefault("pale_moss_carpet"), "bottom", false)
	topper = javaPlant06UpdateMossyCarpetSides(context, topper, true)
	for _, property := range [...]string{"north", "east", "south", "west"} {
		if javaString(topper, property) != "none" && !context.generation.JavaRandom().NextBoolean() {
			topper = javaWith(topper, property, "none")
		}
	}
	if !javaPlant06MossyCarpetHasFaces(topper) || topper == abovePrevious {
		return
	}
	context.generation.SetJavaBlockState(abovePos, topper, 2)
	context.pos = pos
	base = javaPlant06UpdateMossyCarpetSides(context, base, true)
	context.generation.SetJavaBlockState(pos, base, 2)
}

// JavaGenerationFireState applies BaseFireBlock.getState against live
// generation states, including Source flammability and sturdy-face metadata.
func JavaGenerationFireState(access JavaGenerationShapeAccess, pos cube.Pos) uint32 {
	context := javaShapeContext{generation: access, pos: pos}
	below := context.at(pos.Side(cube.FaceDown))
	if context.tag(below, "minecraft:soul_fire_base_blocks") {
		return javaDefault("soul_fire")
	}
	state := javaDefault("fire")
	support := javaGenerationShape(below)
	if support.FireCanBurn || support.Sturdy&(1<<uint(cube.FaceUp)) != 0 {
		return state
	}
	for _, face := range [...]cube.Face{cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
		property := [...]string{"down", "up", "north", "south", "west", "east"}[face]
		state = javaWith(state, property, javaGenerationShape(context.at(pos.Side(face))).FireCanBurn)
	}
	return state
}

// UpdateJavaTemplateShapes applies StructureTemplate's immediate shape phase.
// Call it for one knownShape=false placement, after successful block writes,
// liquid refill and actor loading, but before entities or data markers. Positions
// include accepted unchanged writes and retain placement order and duplicates.
// Generation's updateNeighborsAt is a no-op; it is not a runtime neighbour wave.
func UpdateJavaTemplateShapes(access JavaGenerationShapeAccess, positions []cube.Pos, flags int) {
	if len(positions) == 0 {
		return
	}
	low, high := positions[0], positions[0]
	for _, pos := range positions[1:] {
		for axis := range 3 {
			low[axis] = min(low[axis], pos[axis])
			high[axis] = max(high[axis], pos[axis])
		}
	}
	size := [3]int{high[0] - low[0] + 1, high[1] - low[1] + 1, high[2] - low[2] + 1}
	updateJavaGenerationShapeAtEdge(access, low, size, positions, flags, true)
}

// UpdateJavaGenerationShapeAtEdge applies StructureTemplate.updateShapeAtEdge
// to an explicitly bounded discrete shape. filledPositions are its occupied
// voxels; size may include empty cells that still constrain the source shape.
func UpdateJavaGenerationShapeAtEdge(access JavaGenerationShapeAccess, low cube.Pos, size [3]int, filledPositions []cube.Pos, flags int) {
	updateJavaGenerationShapeAtEdge(access, low, size, filledPositions, flags, false)
}

func updateJavaGenerationShapeAtEdge(access JavaGenerationShapeAccess, low cube.Pos, size [3]int, positions []cube.Pos, flags int, updateInterior bool) {
	if len(positions) == 0 {
		return
	}
	volume := size[0] * size[1] * size[2]
	occupied := make([]uint64, (volume+63)/64)
	for _, pos := range positions {
		index := ((pos[0]-low[0])*size[1]+pos[1]-low[1])*size[2] + pos[2] - low[2]
		occupied[index>>6] |= uint64(1) << uint(index&63)
	}
	context := javaShapeContext{generation: access}
	// DiscreteVoxelShape.forAllFaces visits NONE, FORWARD, BACKWARD.
	// Each scan uses the inverse axis cycle: (x,y,z), (z,x,y), (y,z,x).
	for _, axes := range [...][3]int{{0, 1, 2}, {2, 0, 1}, {1, 2, 0}} {
		aAxis, bAxis, cAxis := axes[0], axes[1], axes[2]
		negative := [3]cube.Face{cube.FaceWest, cube.FaceDown, cube.FaceNorth}[cAxis]
		positive := negative.Opposite()
		var local cube.Pos
		for a := range size[aAxis] {
			local[aAxis] = a
			for b := range size[bAxis] {
				local[bAxis] = b
				lastFull := false
				for c := range size[cAxis] + 1 {
					local[cAxis] = c
					full := false
					if c < size[cAxis] {
						index := (local[0]*size[1]+local[1])*size[2] + local[2]
						full = occupied[index>>6]&(uint64(1)<<uint(index&63)) != 0
					}
					if !lastFull && full {
						javaTemplateShapeEdge(&context, local.Add(low), negative, flags&^1)
					} else if lastFull && !full {
						pos := local.Add(low)
						pos[cAxis]--
						javaTemplateShapeEdge(&context, pos, positive, flags&^1)
					}
					lastFull = full
				}
			}
		}
	}
	if updateInterior {
		for _, pos := range positions {
			context.pos = pos
			state := context.at(pos)
			updated := context.updateFromNeighbourShapes(state)
			if updated != state {
				access.SetJavaBlockState(pos, updated, flags&^1|16)
			}
		}
	}
}

func javaTemplateShapeEdge(context *javaShapeContext, pos cube.Pos, face cube.Face, flags int) {
	neighborPos := pos.Side(face)
	state, neighbor := context.at(pos), context.at(neighborPos)
	context.pos = pos
	updated := context.updateShape(state, face, neighbor)
	if state != updated {
		context.generation.SetJavaBlockState(pos, updated, flags)
	}
	context.pos = neighborPos
	updatedNeighbor := context.updateShape(neighbor, face.Opposite(), updated)
	if neighbor != updatedNeighbor {
		context.generation.SetJavaBlockState(neighborPos, updatedNeighbor, flags)
	}
}
