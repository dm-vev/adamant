package overworld

import java26 "github.com/df-mc/dragonfly/server/internal/java26"

func placeModernSingleBlockPillar(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	direction := int32(1)
	if config["direction"] == "down" {
		direction = -1
	} else if config["direction"] != "up" {
		panic("overworld: invalid single-block pillar direction")
	}
	chance := float32(1)
	if value, ok := config["chance_to_continue"]; ok {
		chance = float32(value.(float64))
	}
	predicate, _ := config["can_replace"].(map[string]any)
	p := origin
	// Java draws before testing build height, including the final outside cell.
	for (predicate == nil || featurePredicate(region, p, predicate)) && random.NextFloat() < chance && p.y >= region.MinY() && p.y <= region.MaxY() {
		state, actor, ok := region.SampleNativeState(config["block"], p, random)
		if !ok {
			panic("overworld: empty single-block pillar provider")
		}
		featureSetNativeBlockFlag2(region, p, state, actor)
		p.y += direction
	}
	p.y -= direction
	if cap, ok := config["cap_feature"]; ok {
		region.PlacePlacedFeature(cap, [3]int32{p.x, p.y, p.z}, random)
	}
	return true
}

func placeModernProjectedPatchySquare(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	size := sampleJavaInt(config["size"], random)
	bound := size*size + 1
	through := config["project_through"].(map[string]any)
	for dx := -size; dx <= size; dx++ {
		for dz := -size; dz <= size; dz++ {
			if random.NextInt(bound) >= bound-absI32(dx)*absI32(dz) {
				continue
			}
			p := featurePos{origin.x + dx, origin.y, origin.z + dz}
			drop := int32(featureNumber(config["max_projection_height"]))
			for featurePredicate(region, featurePos{p.x, p.y - 1, p.z}, through) {
				p.y--
				drop--
				if drop <= 0 {
					break
				}
			}
			if block, ok := sampleOptionalJavaBlockState(region, p, config["block"], random); ok {
				state, err := block.ResolveNative(region.c.BlockRegistry(), region.nativeState(p))
				if err != nil {
					panic(err)
				}
				featureSetNativeBlockFlag2(region, p, state, block.NativeBlock)
			}
		}
	}
	return true
}
