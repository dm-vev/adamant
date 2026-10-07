package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

// placeModernMultifaceGrowth mirrors MultifaceGrowthFeature's search and face-spreading order.
func placeModernMultifaceGrowth(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	blockName, ok := config["block"].(string)
	if !ok {
		panic("overworld: malformed Java multiface growth block")
	}
	blockName = normalizeJavaResource(blockName)
	originName, _ := region.State(origin)
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
	validDirections := make([]string, 0, 6)
	if canCeiling {
		validDirections = append(validDirections, "up")
	}
	if canFloor {
		validDirections = append(validDirections, "down")
	}
	if canWall {
		validDirections = append(validDirections, "north", "east", "south", "west")
	}
	if len(validDirections) == 0 {
		return false
	}

	directions := multifaceShuffle(validDirections, random)
	if multifacePlaceAt(region, blockName, support, origin, random, directions, chance) {
		return true
	}
	for _, searchDirection := range directions {
		placementDirections := make([]string, 0, len(directions)-1)
		for _, direction := range validDirections {
			if direction != multifaceOpposite(searchDirection) {
				placementDirections = append(placementDirections, direction)
			}
		}
		placementDirections = multifaceShuffle(placementDirections, random)
		for range searchRange {
			// Vanilla resets its mutable position to origin on every search iteration.
			pos := multifaceOffset(origin, searchDirection)
			name, _ := region.State(pos)
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
	oldName, oldProperties := region.State(pos)
	if !multifaceAirOrWater(oldName) && normalizeJavaResource(oldName) != blockName {
		return false
	}
	for _, direction := range directions {
		supportPos := multifaceOffset(pos, direction)
		supportName, _ := region.State(supportPos)
		if !multifaceSupportMatchesName(support, supportName) {
			continue
		}
		if !multifaceCanAttach(region, supportPos, multifaceOpposite(direction)) {
			return false
		}
		properties := make(map[string]any, len(oldProperties)+1)
		if normalizeJavaResource(oldName) == blockName {
			for key, value := range oldProperties {
				properties[key] = value
			}
		} else if normalizeJavaResource(oldName) == "water" && multifaceWaterSource(oldProperties) {
			properties["waterlogged"] = true
		}
		if existing, _ := properties[direction].(bool); existing {
			continue
		}
		if existing, _ := properties[direction].(string); existing == "true" {
			continue
		}
		properties[direction] = true
		state, found := featureStateRID(region.c, map[string]any{"id": "minecraft:" + blockName, "properties": properties})
		if !found || !region.Set(pos, state) {
			continue
		}
		if random.NextFloat() < chance {
			multifaceSpreadFromFace(region, blockName, pos, direction, random)
		}
		return true
	}
	return false
}

func multifaceSpreadFromFace(region *featureRegion, blockName string, pos featurePos, startingFace string, random *java26.Random) {
	for _, spreadDirection := range multifaceShuffle([]string{"down", "up", "north", "south", "west", "east"}, random) {
		if multifaceAxis(spreadDirection) == multifaceAxis(startingFace) {
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
			if multifaceCanSpreadInto(region, blockName, target, face) && multifacePlaceFace(region, blockName, target, face) {
				return
			}
		}
	}
}

func multifaceCanSpreadInto(region *featureRegion, blockName string, pos featurePos, face string) bool {
	name, properties := region.State(pos)
	if normalizeJavaResource(name) == "water" && !multifaceWaterSource(properties) {
		return false
	}
	if !multifaceAirOrWater(name) && normalizeJavaResource(name) != blockName {
		return false
	}
	if normalizeJavaResource(name) == blockName {
		if value, ok := properties[face].(bool); ok && value {
			return false
		}
		if value, ok := properties[face].(string); ok && value == "true" {
			return false
		}
	}
	supportPos := multifaceOffset(pos, face)
	return multifaceCanAttach(region, supportPos, multifaceOpposite(face))
}

func multifacePlaceFace(region *featureRegion, blockName string, pos featurePos, face string) bool {
	oldName, properties := region.State(pos)
	if normalizeJavaResource(oldName) != blockName {
		properties = map[string]any{}
	}
	if normalizeJavaResource(oldName) == "water" && multifaceWaterSource(properties) {
		properties["waterlogged"] = true
	}
	properties[face] = true
	state, ok := featureStateRID(region.c, map[string]any{"id": "minecraft:" + blockName, "properties": properties})
	return ok && region.Set(pos, state)
}

func multifaceCanAttach(region *featureRegion, supportPos featurePos, supportFace string) bool {
	block := region.Block(supportPos)
	if block == nil {
		return false
	}
	pos := cube.Pos{int(supportPos.x), int(supportPos.y), int(supportPos.z)}
	return block.Model().FaceSolid(pos, multifaceCubeFace(supportFace), featureRegionBlockSource{region})
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

func multifaceSupportMatchesName(raw any, name string) bool {
	name = normalizeJavaResource(name)
	switch value := raw.(type) {
	case string:
		return name == normalizeJavaResource(value)
	case []any:
		for _, item := range value {
			if id, ok := item.(string); ok && name == normalizeJavaResource(id) {
				return true
			}
		}
	case map[string]any:
		if tag, ok := value["tag"].(string); ok {
			return javaBlockTagContains(normalizeJavaResource(tag), name)
		}
		if id, ok := value["id"].(string); ok {
			return name == normalizeJavaResource(id)
		}
	}
	return false
}

func multifaceAirOrWater(name string) bool {
	switch normalizeJavaResource(name) {
	case "air", "cave_air", "void_air", "water":
		return true
	default:
		return false
	}
}

func multifaceWaterSource(properties map[string]any) bool {
	switch level := properties["level"].(type) {
	case string:
		return level == "" || level == "0"
	case int:
		return level == 0
	case int8:
		return level == 0
	case int16:
		return level == 0
	case int32:
		return level == 0
	case int64:
		return level == 0
	default:
		return true
	}
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

func multifaceShuffle(in []string, random *java26.Random) []string {
	out := append([]string(nil), in...)
	for i := len(out) - 1; i > 0; i-- {
		j := int(random.NextInt(int32(i + 1)))
		out[i], out[j] = out[j], out[i]
	}
	return out
}
