package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

// placeModernMultifaceGrowth mirrors MultifaceGrowthFeature's search and face-spreading order.
func placeModernMultifaceGrowth(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	blockName, ok := config["block"].(string)
	if !ok {
		panic("overworld: malformed Java multiface growth block")
	}
	blockName = normalizeJavaResource(blockName)
	_, originName := multifaceSourceStateAt(region, origin)
	if !multifaceAirOrWater(originName) {
		return false
	}
	searchRange := 10
	if value, exists := config["search_range"]; exists {
		searchRange = featureNumber(value)
	}
	if searchRange < 1 || searchRange > 64 {
		panic("overworld: invalid Java multiface growth search range")
	}
	canFloor, _ := config["can_place_on_floor"].(bool)
	canCeiling, _ := config["can_place_on_ceiling"].(bool)
	canWall, _ := config["can_place_on_wall"].(bool)
	chance := float32(0.5)
	if value, exists := config["chance_of_spreading"]; exists {
		raw, valid := value.(float64)
		if !valid || raw < 0 || raw > 1 {
			panic("overworld: invalid Java multiface growth spread chance")
		}
		chance = float32(raw)
	}
	support, ok := config["can_be_placed_on"]
	if !ok {
		panic("overworld: malformed Java multiface growth support set")
	}
	var validDirections [6]string
	directionCount := 0
	if canCeiling {
		validDirections[directionCount] = "up"
		directionCount++
	}
	if canFloor {
		validDirections[directionCount] = "down"
		directionCount++
	}
	if canWall {
		for _, direction := range [...]string{"north", "east", "south", "west"} {
			validDirections[directionCount] = direction
			directionCount++
		}
	}
	if directionCount == 0 {
		return false
	}

	var shuffledBuffer [6]string
	copy(shuffledBuffer[:], validDirections[:directionCount])
	directions := shuffledBuffer[:directionCount]
	multifaceShuffle(directions, random)
	if multifacePlaceAt(region, blockName, support, origin, random, directions, chance) {
		return true
	}
	for _, searchDirection := range directions {
		var placementBuffer [6]string
		placementDirections := placementBuffer[:0]
		for _, direction := range validDirections[:directionCount] {
			if direction != multifaceOpposite(searchDirection) {
				placementDirections = append(placementDirections, direction)
			}
		}
		multifaceShuffle(placementDirections, random)
		for range searchRange {
			// Vanilla resets its mutable position to origin on every search iteration.
			pos := multifaceOffset(origin, searchDirection)
			_, name := multifaceSourceStateAt(region, pos)
			if !multifaceAirOrWater(name) && normalizeJavaResource(name) != blockName {
				break
			}
			if multifacePlaceAt(region, blockName, support, pos, random, placementDirections, chance) {
				return true
			}
		}
	}
	return false
}

func multifacePlaceAt(region *featureRegion, blockName string, support any, pos featurePos, random *java26.Random, directions []string, chance float32) bool {
	oldID, oldName := multifaceSourceStateAt(region, pos)
	if !multifaceAirOrWater(oldName) && normalizeJavaResource(oldName) != blockName {
		return false
	}
	for _, direction := range directions {
		supportPos := multifaceOffset(pos, direction)
		_, supportName := multifaceSourceStateAt(region, supportPos)
		if !featureBlockHolderContains(support, supportName) {
			continue
		}
		stateID, valid := multifacePlacementState(region, blockName, pos, direction, oldID)
		if !valid {
			return false
		}
		// Source ignores the setter's return value, then marks and consumes spread RNG.
		region.setWithFlags(pos, world.ResolveStateID(region.c.BlockRegistry(), stateID), 3)
		region.markPostProcessing(cube.Pos{int(pos.x), int(pos.y), int(pos.z)})
		if random.NextFloat() < chance {
			multifaceSpreadFromFace(region, blockName, pos, stateID, direction, random)
		}
		return true
	}
	return false
}

func multifaceSpreadFromFace(region *featureRegion, blockName string, pos featurePos, sourceID uint32, startingFace string, random *java26.Random) {
	spreadDirections := [...]string{"down", "up", "north", "south", "west", "east"}
	multifaceShuffle(spreadDirections[:], random)
	for _, spreadDirection := range spreadDirections {
		hasSpreadFace, _ := javadata.BlockStateBool(sourceID, spreadDirection)
		if multifaceAxis(spreadDirection) == multifaceAxis(startingFace) || hasSpreadFace {
			continue
		}
		for spreadType := 0; spreadType < 3; spreadType++ {
			target, face := pos, spreadDirection
			switch spreadType {
			case 1:
				target = multifaceOffset(pos, spreadDirection)
				face = startingFace
			case 2:
				target = multifaceOffset(multifaceOffset(pos, spreadDirection), startingFace)
				face = multifaceOpposite(spreadDirection)
			}
			if multifaceCanSpreadInto(region, blockName, target, face) {
				if multifacePlaceFace(region, blockName, target, face) {
					return
				}
				break
			}
		}
	}
}

func multifaceCanSpreadInto(region *featureRegion, blockName string, pos featurePos, face string) bool {
	id, name := multifaceSourceStateAt(region, pos)
	if normalizeJavaResource(name) == "water" && !multifaceWaterSource(id) {
		return false
	}
	if !multifaceAirOrWater(name) && normalizeJavaResource(name) != blockName {
		return false
	}
	_, valid := multifacePlacementState(region, blockName, pos, face, id)
	return valid
}

func multifacePlaceFace(region *featureRegion, blockName string, pos featurePos, face string) bool {
	id, _ := multifaceSourceStateAt(region, pos)
	stateID, valid := multifacePlacementState(region, blockName, pos, face, id)
	if !valid {
		return false
	}
	region.markPostProcessing(cube.Pos{int(pos.x), int(pos.y), int(pos.z)})
	return region.setWithFlags(pos, world.ResolveStateID(region.c.BlockRegistry(), stateID), 2)
}

func multifaceCanAttach(region *featureRegion, supportPos featurePos, supportFace string) bool {
	id := region.nativeState(supportPos).CanonicalJavaID(region.c.BlockRegistry())
	shape, err := javadata.BlockGenerationShapeID(id)
	if err != nil {
		panic(err)
	}
	return shape.Attachment&(1<<uint(multifaceCubeFace(supportFace))) != 0
}

func multifaceCubeFace(direction string) cube.Face {
	switch direction {
	case "down":
		return cube.FaceDown
	case "up":
		return cube.FaceUp
	case "north":
		return cube.FaceNorth
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	case "east":
		return cube.FaceEast
	default:
		panic("overworld: invalid Java multiface direction " + direction)
	}
}

func multifaceSourceStateAt(region *featureRegion, pos featurePos) (uint32, string) {
	id := region.nativeState(pos).CanonicalJavaID(region.c.BlockRegistry())
	name, valid := javadata.BlockStateName(id)
	if !valid {
		panic("overworld: invalid Java multiface state")
	}
	return id, name
}

func multifacePlacementState(region *featureRegion, blockName string, pos featurePos, face string, oldID uint32) (uint32, bool) {
	oldName, _ := javadata.BlockStateName(oldID)
	id := oldID
	if normalizeJavaResource(oldName) != blockName {
		var err error
		id, err = javadata.BlockStateID(blockName, nil)
		if err != nil {
			panic(err)
		}
		if normalizeJavaResource(oldName) == "water" && multifaceWaterSource(oldID) {
			id, err = javadata.BlockStateWithProperty(id, "waterlogged", true)
			if err != nil {
				panic(err)
			}
		}
	}
	if hasFace, supported := javadata.BlockStateBool(id, face); !supported || hasFace {
		return 0, false
	}
	supportPos := multifaceOffset(pos, face)
	if !multifaceCanAttach(region, supportPos, multifaceOpposite(face)) {
		return 0, false
	}
	id, err := javadata.BlockStateWithProperty(id, face, true)
	if err != nil {
		panic(err)
	}
	return id, true
}

func multifaceAirOrWater(name string) bool {
	switch normalizeJavaResource(name) {
	case "air", "cave_air", "void_air", "water":
		return true
	default:
		return false
	}
}

func multifaceWaterSource(id uint32) bool {
	level, valid := javadata.BlockStateInt(id, "level")
	return valid && level == 0
}

func multifaceAxis(direction string) string {
	switch direction {
	case "up", "down":
		return "y"
	case "east", "west":
		return "x"
	default:
		return "z"
	}
}

func multifaceOpposite(direction string) string {
	switch direction {
	case "up":
		return "down"
	case "down":
		return "up"
	case "north":
		return "south"
	case "south":
		return "north"
	case "west":
		return "east"
	case "east":
		return "west"
	default:
		panic("overworld: invalid Java multiface direction " + direction)
	}
}

func multifaceOffset(pos featurePos, direction string) featurePos {
	switch direction {
	case "up":
		pos.y++
	case "down":
		pos.y--
	case "north":
		pos.z--
	case "south":
		pos.z++
	case "west":
		pos.x--
	case "east":
		pos.x++
	default:
		panic("overworld: invalid Java multiface direction " + direction)
	}
	return pos
}

func multifaceShuffle(in []string, random *java26.Random) {
	for i := len(in) - 1; i > 0; i-- {
		j := int(random.NextInt(int32(i + 1)))
		in[i], in[j] = in[j], in[i]
	}
}
