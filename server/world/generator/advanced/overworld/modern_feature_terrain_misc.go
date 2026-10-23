package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// placeModernTerrainMisc implements vanilla's small position-oriented terrain features.
func placeModernTerrainMisc(region *featureRegion, kind string, config map[string]any, origin featurePos, random *vanilla.Random) (handled, placed bool) {
	switch kind {
	case "block_blob":
		return true, placeModernBlockBlob(region, config, origin, random)
	case "block_pile":
		return true, placeModernBlockPile(region, config, origin, random)
	case "fill_layer":
		return true, placeModernFillLayer(region, config, origin)
	case "replace_single_block":
		return true, placeModernReplaceSingleBlock(region, config, origin, random)
	default:
		return false, false
	}
}

func featureRuleMatches(p featurePos, rule map[string]any, stateID uint32, random *vanilla.Random) bool {
	kind := normalizeResource(rule["predicate_type"].(string))
	switch kind {
	case "always_true":
		return true
	case "block_match":
		name, _ := gamedata.BlockStateName(stateID)
		return normalizeResource(rule["block"].(string)) == normalizeResource(name)
	case "tag_match":
		return featureBlockTagContainsID(rule["tag"].(string), stateID)
	case "height_match":
		return p.y >= int32(featureNumber(rule["min_inclusive"])) && p.y <= int32(featureNumber(rule["max_inclusive"]))
	case "blockstate_match", "random_blockstate_match":
		name, ok := rule["block_state"].(string)
		var properties map[string]any
		if !ok {
			state := rule["block_state"].(map[string]any)
			name = state["id"].(string)
			properties, _ = state["properties"].(map[string]any)
		}
		expected, err := gamedata.BlockStateID(name, properties)
		if err != nil {
			panic(err)
		}
		if stateID != expected {
			return false
		}
		return kind != "random_blockstate_match" || random.NextFloat() < float32(rule["probability"].(float64))
	case "random_block_match":
		name, _ := gamedata.BlockStateName(stateID)
		if normalizeResource(rule["block"].(string)) != normalizeResource(name) {
			return false
		}
		return random.NextFloat() < float32(rule["probability"].(float64))
	case "all_of":
		for _, raw := range rule["rules"].([]any) {
			if !featureRuleMatches(p, raw.(map[string]any), stateID, random) {
				return false
			}
		}
		return true
	case "any_of":
		for _, raw := range rule["rules"].([]any) {
			if featureRuleMatches(p, raw.(map[string]any), stateID, random) {
				return true
			}
		}
		return false
	case "not":
		return !featureRuleMatches(p, rule["rule"].(map[string]any), stateID, random)
	default:
		panic("overworld: unsupported block rule test " + kind)
	}
}

func placeModernBlockBlob(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	predicate, ok := config["can_place_on"].(map[string]any)
	if !ok {
		panic("overworld: malformed block blob predicate")
	}
	for origin.y > region.MinY()+3 && !featurePredicate(region, featurePos{origin.x, origin.y - 1, origin.z}, predicate) {
		origin.y--
	}
	if origin.y <= region.MinY()+3 {
		return false
	}
	placement, ok := featureStateBlock(config["state"])
	if !ok {
		panic("overworld: malformed block blob state")
	}
	for range 3 {
		rx, ry, rz := int32(random.NextInt(2)), int32(random.NextInt(2)), int32(random.NextInt(2))
		threshold := float32(rx+ry+rz)*0.333 + 0.5
		for z := origin.z - rz; z <= origin.z+rz; z++ {
			for y := origin.y - ry; y <= origin.y+ry; y++ {
				for x := origin.x - rx; x <= origin.x+rx; x++ {
					dx, dy, dz := float64(x-origin.x), float64(y-origin.y), float64(z-origin.z)
					if dx*dx+dy*dy+dz*dz <= float64(threshold*threshold) {
						p := featurePos{x, y, z}
						state, err := placement.ResolveNative(region.c.BlockRegistry(), region.nativeState(p))
						if err != nil {
							panic(err)
						}
						region.SetNativeBlock(p, state, placement.NativeBlock)
					}
				}
			}
		}
		origin.x += -1 + int32(random.NextInt(2))
		origin.y -= int32(random.NextInt(2))
		origin.z += -1 + int32(random.NextInt(2))
	}
	return true
}

func placeModernBlockPile(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	if origin.y < region.MinY()+5 {
		return false
	}
	rx, rz := int32(2+random.NextInt(2)), int32(2+random.NextInt(2))
	for z := origin.z - rz; z <= origin.z+rz; z++ {
		for y := origin.y; y <= origin.y+1; y++ {
			for x := origin.x - rx; x <= origin.x+rx; x++ {
				p := featurePos{x, y, z}
				dx, dz := origin.x-x, origin.z-z
				if float32(dx*dx+dz*dz) <= random.NextFloat()*10-random.NextFloat()*6 {
					blockPileTryPlace(region, p, config["state_provider"], random)
				} else if float64(random.NextFloat()) < 0.031 {
					blockPileTryPlace(region, p, config["state_provider"], random)
				}
			}
		}
	}
	return true
}

func blockPileTryPlace(region *featureRegion, p featurePos, provider any, random *vanilla.Random) {
	if !featureRegionAir(region, p) {
		return
	}
	below := featurePos{p.x, p.y - 1, p.z}
	belowID := region.nativeState(below).CanonicalID(region.c.BlockRegistry())
	belowName, ok := gamedata.BlockStateName(belowID)
	if !ok {
		panic("overworld: block pile encountered an invalid support state ID")
	}
	if normalizeResource(belowName) == "dirt_path" {
		if !random.NextBoolean() {
			return
		}
	} else {
		shape, err := gamedata.BlockGenerationShapeID(belowID)
		if err != nil {
			panic(err)
		}
		if shape.Sturdy&(1<<uint(cube.FaceUp)) == 0 {
			return
		}
	}
	state, actor, ok := region.SampleNativeState(provider, p, random)
	if ok && region.setWithFlags(p, state, 260) && actor != nil {
		region.recordTemplateActor(structure.Block{Pos: [3]int32{p.x, p.y, p.z}, NativeBlock: actor})
	}
}

func placeModernFillLayer(region *featureRegion, config map[string]any, origin featurePos) bool {
	height := int32(featureNumber(config["height"]))
	placement, ok := featureStateBlock(config["state"])
	if !ok {
		panic("overworld: malformed fill-layer state")
	}
	state, err := placement.ResolveNative(region.c.BlockRegistry())
	if err != nil {
		panic(err)
	}
	for x := range int32(16) {
		for z := range int32(16) {
			p := featurePos{origin.x + x, region.MinY() + height, origin.z + z}
			if featureRegionAir(region, p) {
				featureSetNativeBlockFlag2(region, p, state, placement.NativeBlock)
			}
		}
	}
	return true
}

func placeModernReplaceSingleBlock(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	targets, ok := config["targets"].([]any)
	if !ok {
		panic("overworld: malformed replace-single-block targets")
	}
	for _, raw := range targets {
		replacement, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed block replacement")
		}
		predicate, ok := replacement["target"].(map[string]any)
		if !ok {
			panic("overworld: malformed block replacement target")
		}
		stateID := region.nativeState(origin).CanonicalID(region.c.BlockRegistry())
		if featureRuleMatches(origin, predicate, stateID, random) {
			placement, ok := featureStateBlock(replacement["state"])
			if !ok {
				panic("overworld: malformed replacement state")
			}
			state, err := placement.ResolveNative(region.c.BlockRegistry(), region.nativeState(origin))
			if err != nil {
				panic(err)
			}
			featureSetNativeBlockFlag2(region, origin, state, placement.NativeBlock)
			break
		}
	}
	return true
}
