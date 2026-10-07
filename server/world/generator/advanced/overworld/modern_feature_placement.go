package overworld

import (
	"fmt"
	"strings"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

// placeModifiedPositions follows FeaturePlacer's depth-first work stack. A
// modifier finishes emitting before its children run, but sibling positions do
// not advance together: feature execution consumes the same random stream.
func (f *ModernFeatures) placeModifiedPositions(region *featureRegion, modifiers []map[string]any, origin featurePos, random *java26.Random, placedName string, place func(featurePos) bool) bool {
	if len(modifiers) == 0 {
		return place(origin)
	}
	type frame struct {
		position featurePos
		modifier int
	}
	var pendingBuffer [32]frame
	pending := append(pendingBuffer[:0], frame{origin, 0})
	var outputBuffer [32]featurePos
	output := outputBuffer[:0]
	placed := false
	for len(pending) != 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		output = f.modifyPlacement(region, current.position, modifiers[current.modifier], random, placedName, output[:0])
		next := current.modifier + 1
		if next == len(modifiers) {
			for _, p := range output {
				placed = place(p) || placed
			}
		} else {
			for i := len(output) - 1; i >= 0; i-- {
				pending = append(pending, frame{output[i], next})
			}
		}
	}
	return placed
}

func (f *ModernFeatures) placeWithModifiers(region *featureRegion, feature any, modifiers []map[string]any, origin featurePos, random *java26.Random, placedName string) bool {
	return f.placeModifiedPositions(region, modifiers, origin, random, placedName, func(p featurePos) bool { return region.PlaceValue(feature, p, random) })
}

func inlinePlacementModifiers(value any) []map[string]any {
	if value == nil {
		return nil
	}
	raw, ok := value.([]any)
	if !ok {
		panic("overworld: malformed inline Java placed-feature modifiers")
	}
	modifiers := make([]map[string]any, len(raw))
	for i, value := range raw {
		modifier, ok := value.(map[string]any)
		if !ok {
			panic("overworld: malformed inline Java placed-feature modifier")
		}
		modifiers[i] = modifier
	}
	return modifiers
}

func (f *ModernFeatures) modifyPlacement(region *featureRegion, p featurePos, modifier map[string]any, r *java26.Random, placedName string, out []featurePos) []featurePos {
	typeName, ok := modifier["type"].(string)
	if !ok {
		panic("overworld: Java placement modifier without type")
	}
	switch normalizeJavaResource(typeName) {
	case "count":
		for range sampleJavaInt(modifier["count"], r) {
			out = append(out, p)
		}
		return out
	case "count_on_every_layer":
		return countEveryLayerPositions(region, p, modifier["count"], r, out)
	case "noise_based_count":
		for range noiseBasedCount(p, modifier) {
			out = append(out, p)
		}
		return out
	case "noise_threshold_count":
		for range noiseThresholdCount(p, modifier) {
			out = append(out, p)
		}
		return out
	case "in_square":
		p.x += r.NextInt(16)
		p.z += r.NextInt(16)
	case "heightmap":
		name, ok := modifier["heightmap"].(string)
		if !ok {
			panic("overworld: malformed heightmap placement")
		}
		p.y = region.placementHeight(p.x, p.z, name)
		if p.y <= region.MinY() {
			return out
		}
	case "height_range":
		provider, ok := modifier["height"].(map[string]any)
		if !ok {
			panic("overworld: malformed height_range placement")
		}
		p.y = sampleJavaHeight(provider, r, region.MinY(), region.MaxY()+1, region.f.source.SeaLevel())
	case "environment_scan":
		input := [1]featurePos{p}
		return append(out, environmentScanPlacement(region, input[:], modifier, region.MinY(), region.MaxY()+1)...)
	case "rarity_filter":
		chance := featureNumber(modifier["chance"])
		if chance <= 0 {
			panic("overworld: invalid Java rarity_filter chance")
		}
		if r.NextFloat() >= float32(1)/float32(chance) {
			return out
		}
	case "random_chance":
		chance, ok := modifier["chance"].(float64)
		if !ok || chance < 0 || chance > 1 {
			panic("overworld: invalid Java random_chance probability")
		}
		if r.NextFloat() >= float32(chance) {
			return out
		}
	case "biome":
		if placedName == "" {
			panic("overworld: biome placement requires a top-level placed feature")
		}
		biome := normalizeJavaResource(f.source.JavaBiomeAt(p.x, p.y, p.z))
		if !f.biomeHasFeature(biome, placedName) {
			return out
		}
	case "offset":
		p.x += sampleJavaInt(modifier["x"], r)
		p.y += sampleJavaInt(modifier["y"], r)
		p.z += sampleJavaInt(modifier["z"], r)
	case "surface_water_depth_filter":
		floor := region.placementHeight(p.x, p.z, "OCEAN_FLOOR")
		surface := region.placementHeight(p.x, p.z, "WORLD_SURFACE")
		if surface-floor > int32(featureNumber(modifier["max_water_depth"])) {
			return out
		}
	case "surface_relative_threshold_filter":
		heightmap, ok := modifier["heightmap"].(string)
		if !ok {
			panic("overworld: malformed surface_relative_threshold_filter")
		}
		min, max := int64(-1<<31), int64(1<<31-1)
		if n, ok := modifier["min_inclusive"].(float64); ok {
			min = int64(int32(n))
		}
		if n, ok := modifier["max_inclusive"].(float64); ok {
			max = int64(int32(n))
		}
		height := int64(region.placementHeight(p.x, p.z, heightmap))
		if int64(p.y) < height+min || int64(p.y) > height+max {
			return out
		}
	case "block_predicate_filter":
		predicate, ok := modifier["predicate"].(map[string]any)
		if !ok {
			panic("overworld: malformed block_predicate_filter")
		}
		if !featurePredicate(region, p, predicate) {
			return out
		}
	case "randomly_selected":
		placements, ok := modifier["placements"].([]any)
		if !ok || len(placements) == 0 {
			panic("overworld: malformed randomly_selected placement")
		}
		selected, ok := placements[r.NextInt(int32(len(placements)))].(map[string]any)
		if !ok {
			panic("overworld: malformed selected placement modifier")
		}
		return f.modifyPlacement(region, p, selected, r, placedName, out)
	case "fixed_placement":
		positions, ok := modifier["positions"].([]any)
		if !ok {
			panic("overworld: malformed fixed_placement positions")
		}
		for _, raw := range positions {
			coordinates, ok := raw.([]any)
			if !ok || len(coordinates) != 3 {
				panic("overworld: malformed fixed_placement position")
			}
			position := featurePos{int32(coordinates[0].(float64)), int32(coordinates[1].(float64)), int32(coordinates[2].(float64))}
			if position.x>>4 == p.x>>4 && position.z>>4 == p.z>>4 {
				out = append(out, position)
			}
		}
		return out
	case "cuboid":
		height := sampleJavaInt(modifier["y_size"], r)
		width, length := sampleJavaInt(modifier["xz_size"], r), sampleJavaInt(modifier["xz_size"], r)
		edges, interior := true, true
		if value, ok := modifier["include_edges"].(bool); ok {
			edges = value
		}
		if value, ok := modifier["include_interior"].(bool); ok {
			interior = value
		}
		for x := range width + 1 {
			for y := range height + 1 {
				for z := range length + 1 {
					xEdge, yEdge, zEdge := x == 0 || x == width, y == 0 || y == height, z == 0 || z == length
					if (edges || !xEdge || !yEdge) && (edges || !zEdge || !yEdge) && (edges || !xEdge || !zEdge) && (interior || xEdge || yEdge || zEdge) {
						out = append(out, featurePos{p.x + x, p.y + y, p.z + z})
					}
				}
			}
		}
		return out
	default:
		panic(fmt.Sprintf("overworld: unsupported Java placement modifier %s", strings.TrimPrefix(typeName, "minecraft:")))
	}
	return append(out, p)
}
