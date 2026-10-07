package overworld

import (
	"maps"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
)

// placeModernTerrainMisc implements vanilla's small position-oriented terrain features.
func placeModernTerrainMisc(region *featureRegion, kind string, config map[string]any, origin featurePos, random *java26.Random) (handled, placed bool) {
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

func replaceSingleRuleMatches(region *featureRegion, p featurePos, rule map[string]any, blockName string, random *java26.Random) bool {
	kind := normalizeJavaResource(rule["predicate_type"].(string))
	switch kind {
	case "always_true":
		return true
	case "block_match", "tag_match", "height_match":
		return orePredicateMatches(p, rule, blockName)
	case "blockstate_match", "random_blockstate_match":
		expected, ok := featureStateBlock(rule["block_state"])
		if !ok {
			panic("overworld: malformed Java block-state rule test")
		}
		if normalizeJavaResource(expected.State.Name) != normalizeJavaResource(blockName) {
			return false
		}
		_, properties := region.State(p)
		if !maps.Equal(javadata.BlockPropertiesWithDefaults(expected.State.Name, expected.State.Properties), javadata.BlockPropertiesWithDefaults(blockName, properties)) {
			return false
		}
		if kind == "random_blockstate_match" {
			return random.NextFloat() < float32(rule["probability"].(float64))
		}
		return true
	case "random_block_match":
		if normalizeJavaResource(rule["block"].(string)) != normalizeJavaResource(blockName) {
			return false
		}
		return random.NextFloat() < float32(rule["probability"].(float64))
	case "all_of":
		for _, raw := range rule["rules"].([]any) {
			if !replaceSingleRuleMatches(region, p, raw.(map[string]any), blockName, random) {
				return false
			}
		}
		return true
	case "any_of":
		for _, raw := range rule["rules"].([]any) {
			if replaceSingleRuleMatches(region, p, raw.(map[string]any), blockName, random) {
				return true
			}
		}
		return false
	case "not":
		return !replaceSingleRuleMatches(region, p, rule["rule"].(map[string]any), blockName, random)
	default:
		panic("overworld: unsupported Java replace-single-block rule test " + kind)
	}
}

func placeModernBlockBlob(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	predicate, ok := config["can_place_on"].(map[string]any)
	if !ok {
		panic("overworld: malformed Java block blob predicate")
	}
	for origin.y > region.MinY()+3 && !featurePredicate(region, featurePos{origin.x, origin.y - 1, origin.z}, predicate) {
		origin.y--
	}
	if origin.y <= region.MinY()+3 {
		return false
	}
	placement, ok := featureStateBlock(config["state"])
	if !ok {
		panic("overworld: malformed Java block blob state")
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

func placeModernBlockPile(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
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

func blockPileTryPlace(region *featureRegion, p featurePos, provider any, random *java26.Random) {
	if !featureRegionAir(region, p) {
		return
	}
	below := featurePos{p.x, p.y - 1, p.z}
	name, properties := region.State(below)
	if normalizeJavaResource(name) == "dirt_path" {
		if !random.NextBoolean() {
			return
		}
	} else {
		shape, err := javadata.BlockGenerationShape(name, properties)
		if err != nil {
			panic(err)
		}
		if shape.Sturdy&(1<<uint(cube.FaceUp)) == 0 {
			return
		}
	}
	state, actor, ok := region.SampleNativeState(provider, p, random)
	if ok {
		region.SetNativeBlock(p, state, actor)
	}
}

func placeModernFillLayer(region *featureRegion, config map[string]any, origin featurePos) bool {
	height := int32(featureNumber(config["height"]))
	placement, ok := featureStateBlock(config["state"])
	if !ok {
		panic("overworld: malformed Java fill-layer state")
	}
	for x := int32(0); x < 16; x++ {
		for z := int32(0); z < 16; z++ {
			p := featurePos{origin.x + x, region.MinY() + height, origin.z + z}
			if featureRegionAir(region, p) {
				state, err := placement.ResolveNative(region.c.BlockRegistry(), region.nativeState(p))
				if err != nil {
					panic(err)
				}
				region.SetNativeBlock(p, state, placement.NativeBlock)
			}
		}
	}
	return true
}

func placeModernReplaceSingleBlock(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	targets, ok := config["targets"].([]any)
	if !ok {
		panic("overworld: malformed Java replace-single-block targets")
	}
	for _, raw := range targets {
		replacement, ok := raw.(map[string]any)
		if !ok {
			panic("overworld: malformed Java block replacement")
		}
		predicate, ok := replacement["target"].(map[string]any)
		if !ok {
			panic("overworld: malformed Java block replacement target")
		}
		name, _ := region.State(origin)
		if replaceSingleRuleMatches(region, origin, predicate, name, random) {
			placement, ok := featureStateBlock(replacement["state"])
			if !ok {
				panic("overworld: malformed Java replacement state")
			}
			state, err := placement.ResolveNative(region.c.BlockRegistry(), region.nativeState(origin))
			if err != nil {
				panic(err)
			}
			region.SetNativeBlock(origin, state, placement.NativeBlock)
			break
		}
	}
	return true
}
