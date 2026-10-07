package overworld

import (
	"strings"

	java26 "github.com/df-mc/dragonfly/server/internal/java26"
)

// placeModernSpeleothemExact ports SpeleothemFeature.place.
func placeModernSpeleothemExact(r *featureRegion, c map[string]any, origin featurePos, random *java26.Random) bool {
	base := speleothemBlockName(c["base_block"], "")
	pointed := speleothemBlockName(c["pointed_block"], "")
	replaceable := c["replaceable_blocks"]
	above := speleothemIsBase(r, c, featurePos{origin.x, origin.y + 1, origin.z})
	below := speleothemIsBase(r, c, featurePos{origin.x, origin.y - 1, origin.z})

	var dir int32
	switch {
	case above && below:
		if random.NextBoolean() {
			dir = -1
		} else {
			dir = 1
		}
	case above:
		dir = -1
	case below:
		dir = 1
	default:
		return false
	}

	root := featurePos{origin.x, origin.y - dir, origin.z}
	speleothemPlaceBase(r, root, base, replaceable)
	for _, d := range [...]struct {
		dx, dz int32
	}{
		{0, -1}, {1, 0}, {0, 1}, {-1, 0},
	} {
		if random.NextFloat() > speleothemChanceConfig(c, "chance_of_directional_spread", .7) {
			continue
		}
		next := featurePos{root.x + d.dx, root.y, root.z + d.dz}
		speleothemPlaceBase(r, next, base, replaceable)
		if random.NextFloat() > speleothemChanceConfig(c, "chance_of_spread_radius2", .5) {
			continue
		}
		next = speleothemRandomNeighbor(next, random)
		speleothemPlaceBase(r, next, base, replaceable)
		if random.NextFloat() > speleothemChanceConfig(c, "chance_of_spread_radius3", .5) {
			continue
		}
		next = speleothemRandomNeighbor(next, random)
		speleothemPlaceBase(r, next, base, replaceable)
	}

	height := int32(1)
	if random.NextFloat() < speleothemChanceConfig(c, "chance_of_taller_generation", .2) &&
		speleothemEmptyOrWater(r, speleothemNeighbor(origin, dir)) {
		height = 2
	}
	speleothemGrow(r, c, origin, dir, height, false, base, pointed)
	return true
}

func speleothemIsBase(r *featureRegion, c map[string]any, p featurePos) bool {
	name, _ := r.State(p)
	return normalizeJavaResource(name) == speleothemBlockName(c["base_block"], "") ||
		speleothemTag(c["replaceable_blocks"], name)
}

func speleothemPlaceBase(r *featureRegion, p featurePos, base string, replaceable any) {
	name, _ := r.State(p)
	if !speleothemTag(replaceable, name) {
		return
	}
	speleothemSetNamed(r, p, base)
}

func speleothemRandomNeighbor(p featurePos, random *java26.Random) featurePos {
	switch random.NextInt(6) {
	case 0:
		p.y--
	case 1:
		p.y++
	case 2:
		p.z--
	case 3:
		p.z++
	case 4:
		p.x--
	default:
		p.x++
	}
	return p
}

func speleothemNeighbor(p featurePos, dir int32) featurePos {
	p.y += dir
	return p
}

func speleothemChanceConfig(c map[string]any, key string, fallback float32) float32 {
	return float32(speleothemFloat(c[key], float64(fallback)))
}

// placeModernSpeleothemClusterExact ports SpeleothemClusterFeature.place.
func placeModernSpeleothemClusterExact(r *featureRegion, c map[string]any, origin featurePos, random *java26.Random) bool {
	if !speleothemEmptyOrWater(r, origin) {
		return false
	}
	height := speleothemIntSample(c["height"], random, 0)
	wetness := speleothemFloatSample(c["wetness"], random, 0)
	density := speleothemFloatSample(c["density"], random, 0)
	xr, zr := speleothemIntSample(c["radius"], random, 0), speleothemIntSample(c["radius"], random, 0)
	for dx := -xr; dx <= xr; dx++ {
		for dz := -zr; dz <= zr; dz++ {
			speleothemColumnPlace(r, c, random, featurePos{origin.x + dx, origin.y, origin.z + dz}, int(dx), int(dz), wetness, speleothemChance(c, int(xr), int(zr), int(dx), int(dz)), height, density)
		}
	}
	return true
}

type speleothemColumn struct{ ceiling, floor *int32 }

func speleothemColumnPlace(r *featureRegion, c map[string]any, random *java26.Random, p featurePos, dx, dz int, wetness float32, chance float64, height int32, density float32) {
	col, ok := speleothemScan(r, p, int32(speleothemInt(c["floor_to_ceiling_search_range"], 0)))
	if !ok || col.ceiling == nil && col.floor == nil {
		return
	}
	ceiling, floor := col.ceiling, col.floor
	if random.NextFloat() < wetness && floor != nil && speleothemCanPool(r, c, featurePos{p.x, *floor, p.z}) {
		y := *floor - 1
		speleothemSetNamed(r, featurePos{p.x, *floor, p.z}, "water")
		floor = &y
	}
	stalactite := int32(0)
	wantStalactite := random.NextDouble() < chance
	if ceiling != nil && wantStalactite && !speleothemNameIs(r, featurePos{p.x, *ceiling, p.z}, "lava") {
		speleothemReplaceBase(r, c, featurePos{p.x, *ceiling, p.z}, speleothemIntSample(c["speleothem_block_layer_thickness"], random, 0), 1)
		max := height
		if floor != nil && *ceiling-*floor < max {
			max = *ceiling - *floor
		}
		stalactite = speleothemHeight(random, c, dx, dz, density, max)
	}
	stalagmite := int32(0)
	wantStalagmite := random.NextDouble() < chance
	if floor != nil && wantStalagmite && !speleothemNameIs(r, featurePos{p.x, *floor, p.z}, "lava") {
		speleothemReplaceBase(r, c, featurePos{p.x, *floor, p.z}, speleothemIntSample(c["speleothem_block_layer_thickness"], random, 0), -1)
		if ceiling != nil {
			diff := int32(speleothemInt(c["max_stalagmite_stalactite_height_diff"], 0))
			stalagmite = stalactite + random.NextInt(2*diff+1) - diff
			if stalagmite < 0 {
				stalagmite = 0
			}
		} else {
			stalagmite = speleothemHeight(random, c, dx, dz, density, height)
		}
	}
	actualStalactite, actualStalagmite := stalactite, stalagmite
	if ceiling != nil && floor != nil && *ceiling-stalactite <= *floor+stalagmite {
		low := *ceiling - stalactite
		if low < *floor+1 {
			low = *floor + 1
		}
		high := *floor + stalagmite
		if high > *ceiling-1 {
			high = *ceiling - 1
		}
		bottom := low + random.NextInt(high-low+2)
		actualStalactite, actualStalagmite = *ceiling-bottom, bottom-1-*floor
	}
	merge := random.NextBoolean() && actualStalactite > 0 && actualStalagmite > 0 && ceiling != nil && floor != nil && actualStalactite+actualStalagmite == *ceiling-*floor-1
	base, point := speleothemBlockName(c["base_block"], "dripstone_block"), speleothemBlockName(c["pointed_block"], "pointed_dripstone")
	if ceiling != nil {
		speleothemGrow(r, c, featurePos{p.x, *ceiling - 1, p.z}, -1, actualStalactite, merge, base, point)
	}
	if floor != nil {
		speleothemGrow(r, c, featurePos{p.x, *floor + 1, p.z}, 1, actualStalagmite, merge, base, point)
	}
}

func speleothemScan(r *featureRegion, o featurePos, reach int32) (speleothemColumn, bool) {
	if !speleothemEmptyOrWater(r, o) {
		return speleothemColumn{}, false
	}
	col := speleothemColumn{}
	// Column.scan searches the ceiling ray first, then the floor ray.
	for _, dir := range [...]int32{1, -1} {
		p := o
		// Column.scan evaluates the origin on each ray before stepping and checks
		// the edge after at most searchRange-1 moves.
		for i := int32(1); i < reach && speleothemEmptyOrWater(r, p); i++ {
			p.y += dir
		}
		if p.y >= r.MinY() && p.y <= r.MaxY() {
			name, _ := r.State(p)
			if !speleothemEmptyWaterName(name) {
				y := p.y
				if dir > 0 {
					col.ceiling = &y
				} else {
					col.floor = &y
				}
			}
		}
	}
	return col, true
}
func speleothemEmptyOrWater(r *featureRegion, p featurePos) bool {
	n, _ := r.State(p)
	return speleothemEmptyWaterName(n)
}
func speleothemEmptyWaterName(n string) bool {
	switch normalizeJavaResource(n) {
	case "air", "cave_air", "void_air", "water":
		return true
	}
	return false
}
func speleothemNameIs(r *featureRegion, p featurePos, name string) bool {
	n, _ := r.State(p)
	return normalizeJavaResource(n) == name
}
func speleothemBlockName(v any, d string) string {
	if s, ok := v.(string); ok {
		return normalizeJavaResource(s)
	}
	return d
}
func speleothemInt(v any, d int) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return d
}
func speleothemFloat(v any, d float64) float64 {
	if n, ok := v.(float64); ok {
		return n
	}
	return d
}
func speleothemIntSample(v any, r *java26.Random, d int32) int32 {
	if n, ok := v.(float64); ok {
		return int32(n)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return d
	}
	switch normalizeJavaResource(speleothemBlockName(m["type"], "")) {
	case "constant":
		return int32(speleothemInt(m["value"], int(d)))
	case "uniform":
		lo := int32(speleothemInt(m["min_inclusive"], int(d)))
		hi := int32(speleothemInt(m["max_inclusive"], int(d)))
		return lo + r.NextInt(hi-lo+1)
	default:
		panic("overworld: unsupported speleothem integer provider")
	}
}
func speleothemFloatSample(v any, r *java26.Random, d float32) float32 {
	if n, ok := v.(float64); ok {
		return float32(n)
	}
	m, ok := v.(map[string]any)
	if !ok {
		return d
	}
	switch normalizeJavaResource(speleothemBlockName(m["type"], "")) {
	case "constant":
		return float32(speleothemFloat(m["value"], float64(d)))
	case "uniform":
		lo := float32(speleothemFloat(m["min_inclusive"], float64(d)))
		hi := float32(speleothemFloat(m["max_exclusive"], float64(d)))
		return lo + (hi-lo)*r.NextFloat()
	case "clamped_normal":
		mean := float32(speleothemFloat(m["mean"], float64(d)))
		dev := float32(speleothemFloat(m["deviation"], 0))
		v := mean + float32(r.NextGaussian())*dev
		lo := float32(speleothemFloat(m["min"], float64(d)))
		hi := float32(speleothemFloat(m["max"], float64(d)))
		if v < lo {
			v = lo
		}
		if v > hi {
			v = hi
		}
		return v
	default:
		panic("overworld: unsupported speleothem float provider")
	}
}
func speleothemChance(c map[string]any, xr, zr, dx, dz int) float64 {
	edge := xr - absInt(dx)
	if z := zr - absInt(dz); z < edge {
		edge = z
	}
	return speleothemMap(float64(edge), 0, float64(speleothemInt(c["max_distance_from_edge_affecting_chance_of_speleothem"], 1)), speleothemFloat(c["chance_of_speleothem_at_max_distance_from_center"], .1), 1)
}
func speleothemMap(v, lo, hi, a, b float64) float64 {
	if lo == hi {
		return a
	}
	t := (v - lo) / (hi - lo)
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return a + (b-a)*t
}
func speleothemHeight(r *java26.Random, c map[string]any, dx, dz int, density float32, max int32) int32 {
	if r.NextFloat() > density {
		return 0
	}
	distance := absInt(dx) + absInt(dz)
	mean := float32(speleothemMap(float64(distance), 0, float64(speleothemInt(c["max_distance_from_center_affecting_height_bias"], 1)), float64(max)/2, 0))
	dev := float32(speleothemInt(c["height_deviation"], 1))
	v := mean + float32(r.NextGaussian())*dev
	if v < 0 {
		v = 0
	}
	if v > float32(max) {
		v = float32(max)
	}
	return int32(v)
}
func speleothemCanPool(r *featureRegion, c map[string]any, p featurePos) bool {
	n, _ := r.State(p)
	if normalizeJavaResource(n) == "water" || normalizeJavaResource(n) == speleothemBlockName(c["base_block"], "dripstone_block") || normalizeJavaResource(n) == speleothemBlockName(c["pointed_block"], "pointed_dripstone") || speleothemWater(r, featurePos{p.x, p.y + 1, p.z}) {
		return false
	}
	for _, d := range [...][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
		if !speleothemAdjacent(r, featurePos{p.x + d[0], p.y, p.z + d[1]}) {
			return false
		}
	}
	return speleothemAdjacent(r, featurePos{p.x, p.y - 1, p.z})
}
func speleothemWater(r *featureRegion, p featurePos) bool {
	state := r.nativeState(p)
	for _, runtimeID := range [...]uint32{state.Block, state.Liquid} {
		name, _, ok := r.c.BlockRegistry().RuntimeIDToState(runtimeID)
		if !ok {
			panic("overworld: invalid native speleothem fluid state")
		}
		if normalizeJavaResource(name) == "water" {
			return true
		}
	}
	return false
}
func speleothemSetNamed(r *featureRegion, p featurePos, name string) {
	rid, ok := featureStateRID(r.c, map[string]any{"id": "minecraft:" + name})
	if !ok {
		panic("overworld: unresolved Java speleothem state minecraft:" + name)
	}
	r.Set(p, rid)
}
func speleothemAdjacent(r *featureRegion, p featurePos) bool {
	name, _ := r.State(p)
	return javaBlockTagContains("base_stone_overworld", normalizeJavaResource(name)) || speleothemWater(r, p)
}
func speleothemReplaceBase(r *featureRegion, c map[string]any, p featurePos, count, dy int32) {
	baseName := "minecraft:" + speleothemBlockName(c["base_block"], "dripstone_block")
	base, ok := featureStateRID(r.c, map[string]any{"id": baseName})
	if !ok {
		panic("overworld: unresolved Java speleothem base block " + baseName)
	}
	for i := int32(0); i < count; i++ {
		name, _ := r.State(p)
		if !speleothemTag(c["replaceable_blocks"], name) {
			return
		}
		r.Set(p, base)
		p.y += dy
	}
}
func speleothemGrow(r *featureRegion, c map[string]any, start featurePos, dir, height int32, merge bool, base, point string) {
	support := featurePos{start.x, start.y - dir, start.z}
	n, _ := r.State(support)
	if !speleothemTag(c["replaceable_blocks"], n) && normalizeJavaResource(n) != base {
		return
	}
	for _, thickness := range speleothemThicknesses(height, merge) {
		props := map[string]any{"vertical_direction": map[bool]string{true: "up", false: "down"}[dir > 0], "thickness": thickness, "waterlogged": speleothemWater(r, start)}
		state, ok := featureStateRID(r.c, map[string]any{"id": "minecraft:" + point, "properties": props})
		if !ok {
			panic("overworld: unresolved Java pointed-dripstone state")
		}
		r.Set(start, state)
		start.y += dir
	}
}
func speleothemThicknesses(n int32, merge bool) []string {
	out := make([]string, 0, n)
	if n >= 3 {
		out = append(out, "base")
		for i := int32(0); i < n-3; i++ {
			out = append(out, "middle")
		}
	}
	if n >= 2 {
		out = append(out, "frustum")
	}
	if n >= 1 {
		if merge {
			out = append(out, "tip_merge")
		} else {
			out = append(out, "tip")
		}
	}
	return out
}
func speleothemTag(raw any, name string) bool {
	s, ok := raw.(string)
	if !ok {
		return false
	}
	if strings.HasPrefix(s, "#") {
		return javaBlockTagContains(normalizeJavaResource(strings.TrimPrefix(s, "#")), normalizeJavaResource(name))
	}
	return normalizeJavaResource(s) == normalizeJavaResource(name)
}
