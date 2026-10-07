package overworld

// featureWouldSurvive evaluates the states referenced by builtin would_survive
// predicates using Java's support tags and fluid rules, not native collision guesses.
func featureWouldSurvive(region *featureRegion, p featurePos, value any) bool {
	state, ok := featureStateBlock(value)
	if !ok {
		panic("overworld: malformed would_survive state")
	}
	name := normalizeJavaResource(state.State.Name)
	below := featurePos{p.x, p.y - 1, p.z}
	support, _ := region.State(below)
	support = normalizeJavaResource(support)
	switch name {
	case "oak_sapling", "spruce_sapling", "birch_sapling", "jungle_sapling", "acacia_sapling", "dark_oak_sapling", "cherry_sapling", "pale_oak_sapling", "poplar_sapling", "firefly_bush":
		return javaBlockTagContains("supports_vegetation", support)
	case "mangrove_propagule":
		if state.State.Properties["hanging"] == true {
			above, _ := region.State(featurePos{p.x, p.y + 1, p.z})
			return javaBlockTagContains("supports_hanging_mangrove_propagule", normalizeJavaResource(above))
		}
		return javaBlockTagContains("supports_mangrove_propagule", support)
	case "cactus":
		for _, delta := range [4]featurePos{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			neighbor := featurePos{p.x + delta.x, p.y, p.z + delta.z}
			block, properties := region.State(neighbor)
			if featureJavaSolid(block) || featureFluidTagContains("lava", featureFluidName(region, neighbor, properties)) {
				return false
			}
		}
		if support != "cactus" && !javaBlockTagContains("supports_cactus", support) {
			return false
		}
		above, _ := region.State(featurePos{p.x, p.y + 1, p.z})
		switch normalizeJavaResource(above) {
		case "water", "lava", "bubble_column":
			return false
		}
		return true
	case "sugar_cane":
		if support == "sugar_cane" {
			return true
		}
		if !javaBlockTagContains("supports_sugar_cane", support) {
			return false
		}
		for _, delta := range [4]featurePos{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			neighbor := featurePos{below.x + delta.x, below.y, below.z + delta.z}
			block, properties := region.State(neighbor)
			if featureFluidTagContains("supports_sugar_cane_adjacently", featureFluidName(region, neighbor, properties)) || javaBlockTagContains("supports_sugar_cane_adjacently", normalizeJavaResource(block)) {
				return true
			}
		}
		return false
	default:
		panic("overworld: unsupported would_survive state " + name)
	}
}
