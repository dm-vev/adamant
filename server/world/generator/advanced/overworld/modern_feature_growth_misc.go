package overworld

import (
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

func placeModernGrowthMisc(region *featureRegion, kind string, config map[string]any, origin featurePos, random *java26.Random) (bool, bool) {
	switch kind {
	case "coral_tree":
		pos := origin
		for i, n := int32(0), random.NextInt(3)+1; i < n; i++ {
			if !region.PlacePlacedFeature(config["feature"], [3]int32{pos.x, pos.y, pos.z}, random) {
				return true, true
			}
			pos.y++
		}
		top := pos
		n := random.NextInt(3) + 2
		dirs := []featurePos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}
		for i := len(dirs); i > 1; i-- {
			j := int(random.NextInt(int32(i)))
			dirs[i-1], dirs[j] = dirs[j], dirs[i-1]
		}
		for _, d := range dirs[:n] {
			pos = featurePos{top.x + d.x, top.y, top.z + d.z}
			height := random.NextInt(5) + 2
			length := int32(0)
			for j := int32(0); j < height && region.PlacePlacedFeature(config["feature"], [3]int32{pos.x, pos.y, pos.z}, random); j++ {
				length++
				pos.y++
				if j == 0 || length >= 2 && random.NextFloat() < .25 {
					pos.x += d.x
					pos.z += d.z
					length = 0
				}
			}
		}
		return true, true
	case "coral_claw":
		if !region.PlacePlacedFeature(config["feature"], [3]int32{origin.x, origin.y, origin.z}, random) {
			return true, false
		}
		dirs := []featurePos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}
		claw := dirs[random.NextInt(4)]
		n := int(random.NextInt(2) + 2)
		poss := []featurePos{claw, {-claw.z, 0, claw.x}, {claw.z, 0, -claw.x}}
		for i := len(poss); i > 1; i-- {
			j := int(random.NextInt(int32(i)))
			poss[i-1], poss[j] = poss[j], poss[i-1]
		}
		for _, branch := range poss[:n] {
			pos := origin
			side := random.NextInt(2) + 1
			pos.x += branch.x
			pos.z += branch.z
			var segment featurePos
			var inward int32
			if branch == claw {
				segment = claw
				inward = random.NextInt(3) + 2
			} else {
				pos.y++
				choices := []featurePos{branch, {0, 1, 0}}
				segment = choices[random.NextInt(2)]
				inward = random.NextInt(3) + 3
			}
			for i := int32(0); i < side && region.PlacePlacedFeature(config["feature"], [3]int32{pos.x, pos.y, pos.z}, random); i++ {
				pos.x += segment.x
				pos.y += segment.y
				pos.z += segment.z
			}
			pos.x -= segment.x
			pos.y -= segment.y
			pos.z -= segment.z
			pos.y++
			for i := int32(0); i < inward; i++ {
				pos.x += claw.x
				pos.z += claw.z
				if !region.PlacePlacedFeature(config["feature"], [3]int32{pos.x, pos.y, pos.z}, random) {
					break
				}
				if random.NextFloat() < .25 {
					pos.y++
				}
			}
		}
		return true, true
	}
	return false, false
}
