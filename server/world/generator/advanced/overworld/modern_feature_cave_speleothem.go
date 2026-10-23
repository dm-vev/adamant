package overworld

import (
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

type speleothemColumn struct {
	ceiling, floor       int32
	hasCeiling, hasFloor bool
}

// placeModernSpeleothemExact ports SpeleothemFeature.place.
func placeModernSpeleothemExact(r *featureRegion, c map[string]any, origin featurePos, random *vanilla.Random) bool {
	base := speleothemBlockName(c["base_block"], "")
	point := speleothemBlockName(c["pointed_block"], "")
	replaceable := c["replaceable_blocks"]
	above := speleothemIsBase(r, base, replaceable, featurePos{origin.x, origin.y + 1, origin.z})
	below := speleothemIsBase(r, base, replaceable, featurePos{origin.x, origin.y - 1, origin.z})
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
	for _, d := range [...]struct{ dx, dz int32 }{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
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
	if random.NextFloat() < speleothemChanceConfig(c, "chance_of_taller_generation", .2) && speleothemEmptyOrWater(r, featurePos{origin.x, origin.y + dir, origin.z}) {
		height = 2
	}
	speleothemGrow(r, c, origin, dir, height, false, base, point)
	return true
}

func speleothemIsBase(r *featureRegion, base string, replaceable any, p featurePos) bool {
	name := speleothemStateName(r, p)
	return normalizeResource(name) == base || featureBlockHolderContains(replaceable, name)
}
func speleothemPlaceBase(r *featureRegion, p featurePos, base string, replaceable any) {
	name := speleothemStateName(r, p)
	if featureBlockHolderContains(replaceable, name) {
		r.setWithFlags(p, speleothemDefaultState(r, base), 2)
	}
}
func speleothemRandomNeighbor(p featurePos, random *vanilla.Random) featurePos {
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
func speleothemChanceConfig(c map[string]any, key string, fallback float32) float32 {
	return float32(speleothemFloat(c[key], float64(fallback)))
}

// placeModernSpeleothemClusterExact ports SpeleothemClusterFeature.place.
func placeModernSpeleothemClusterExact(r *featureRegion, c map[string]any, origin featurePos, random *vanilla.Random) bool {
	if !speleothemEmptyOrWater(r, origin) {
		return false
	}
	height := sampleInt(c["height"], random)
	wetness := sampleFloatValue(c["wetness"], random)
	density := sampleFloatValue(c["density"], random)
	xr, zr := sampleInt(c["radius"], random), sampleInt(c["radius"], random)
	for dx := -xr; dx <= xr; dx++ {
		for dz := -zr; dz <= zr; dz++ {
			speleothemColumnPlace(r, c, random, featurePos{origin.x + dx, origin.y, origin.z + dz}, dx, dz, wetness, speleothemChance(c, xr, zr, dx, dz), height, density)
		}
	}
	return true
}

func speleothemColumnPlace(r *featureRegion, c map[string]any, random *vanilla.Random, p featurePos, dx, dz int32, wetness float32, chance float64, height int32, density float32) {
	column, ok := speleothemScan(r, p, int32(featureNumber(c["floor_to_ceiling_search_range"])))
	if !ok || !column.hasCeiling && !column.hasFloor {
		return
	}
	ceiling, floor := column.ceiling, column.floor
	hasCeiling, hasFloor := column.hasCeiling, column.hasFloor
	if random.NextFloat() < wetness && hasFloor && speleothemCanPool(r, c, featurePos{p.x, floor, p.z}) {
		speleothemSetNamed(r, featurePos{p.x, floor, p.z}, "minecraft:water")
		floor--
	}
	stalactite := int32(0)
	wantStalactite := random.NextDouble() < chance
	if hasCeiling && wantStalactite && !speleothemNameIs(r, featurePos{p.x, ceiling, p.z}, "minecraft:lava") {
		speleothemReplaceBase(r, c, featurePos{p.x, ceiling, p.z}, sampleInt(c["speleothem_block_layer_thickness"], random), 1)
		maxHeight := height
		if hasFloor && ceiling-floor < maxHeight {
			maxHeight = ceiling - floor
		}
		stalactite = speleothemHeight(random, c, dx, dz, density, maxHeight)
	}
	stalagmite := int32(0)
	wantStalagmite := random.NextDouble() < chance
	if hasFloor && wantStalagmite && !speleothemNameIs(r, featurePos{p.x, floor, p.z}, "minecraft:lava") {
		speleothemReplaceBase(r, c, featurePos{p.x, floor, p.z}, sampleInt(c["speleothem_block_layer_thickness"], random), -1)
		if hasCeiling {
			diff := int32(featureNumber(c["max_stalagmite_stalactite_height_diff"]))
			stalagmite = stalactite + random.NextInt(2*diff+1) - diff
			if stalagmite < 0 {
				stalagmite = 0
			}
		} else {
			stalagmite = speleothemHeight(random, c, dx, dz, density, height)
		}
	}
	actualStalactite, actualStalagmite := stalactite, stalagmite
	if hasCeiling && hasFloor && ceiling-stalactite <= floor+stalagmite {
		low := max(ceiling-stalactite, floor+1)
		high := min(floor+stalagmite, ceiling-1)
		bottom := low + random.NextInt(high-low+2)
		actualStalactite, actualStalagmite = ceiling-bottom, bottom-1-floor
	}
	merge := random.NextBoolean() && actualStalactite > 0 && actualStalagmite > 0 && hasCeiling && hasFloor && actualStalactite+actualStalagmite == ceiling-floor-1
	base := speleothemBlockName(c["base_block"], "minecraft:dripstone_block")
	point := speleothemBlockName(c["pointed_block"], "minecraft:pointed_dripstone")
	if hasCeiling {
		speleothemGrow(r, c, featurePos{p.x, ceiling - 1, p.z}, -1, actualStalactite, merge, base, point)
	}
	if hasFloor {
		speleothemGrow(r, c, featurePos{p.x, floor + 1, p.z}, 1, actualStalagmite, merge, base, point)
	}
}

func speleothemScan(r *featureRegion, origin featurePos, reach int32) (speleothemColumn, bool) {
	if !speleothemEmptyOrWater(r, origin) {
		return speleothemColumn{}, false
	}
	column := speleothemColumn{}
	for _, dir := range [...]int32{1, -1} {
		y := origin.y
		for i := int32(1); i < reach && speleothemEmptyOrWater(r, featurePos{origin.x, y, origin.z}); i++ {
			y += dir
		}
		if y >= r.MinY() && y <= r.MaxY() {
			name := speleothemStateName(r, featurePos{origin.x, y, origin.z})
			if !speleothemEmptyWaterName(name) {
				if dir > 0 {
					column.ceiling, column.hasCeiling = y, true
				} else {
					column.floor, column.hasFloor = y, true
				}
			}
		}
	}
	return column, true
}
func speleothemEmptyOrWater(r *featureRegion, p featurePos) bool {
	if p.y < r.MinY() || p.y > r.MaxY() {
		return true
	}
	name := speleothemStateName(r, p)
	return speleothemEmptyWaterName(name)
}
func speleothemEmptyWaterName(name string) bool {
	switch normalizeResource(name) {
	case "air", "cave_air", "void_air", "water":
		return true
	}
	return false
}
func speleothemNameIs(r *featureRegion, p featurePos, name string) bool {
	got := speleothemStateName(r, p)
	return normalizeResource(got) == normalizeResource(name)
}
func speleothemBlockName(v any, fallback string) string {
	if name, ok := v.(string); ok {
		return normalizeResource(name)
	}
	if state, ok := v.(map[string]any); ok {
		for _, key := range [...]string{"id", "Name", "name"} {
			if name, ok := state[key].(string); ok {
				return normalizeResource(name)
			}
		}
	}
	return normalizeResource(fallback)
}
func speleothemFloat(v any, fallback float64) float64 {
	if value, ok := v.(float64); ok {
		return value
	}
	return fallback
}
func speleothemChance(c map[string]any, xr, zr, dx, dz int32) float64 {
	edge := min(xr-speleothemAbs(dx), zr-speleothemAbs(dz))
	maxDistance := float32(featureNumber(c["max_distance_from_edge_affecting_chance_of_speleothem"]))
	t := float32(edge) / maxDistance
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	chance := float32(speleothemFloat(c["chance_of_speleothem_at_max_distance_from_center"], .1))
	return float64(chance + (1-chance)*t)
}
func speleothemHeight(random *vanilla.Random, c map[string]any, dx, dz int32, density float32, maxHeight int32) int32 {
	if random.NextFloat() > density {
		return 0
	}
	distance := float64(speleothemAbs(dx) + speleothemAbs(dz))
	maxDistance := float64(featureNumber(c["max_distance_from_center_affecting_height_bias"]))
	halfHeight := float64(maxHeight) / 2
	mean := float32(halfHeight + min(float64(1), distance/maxDistance)*(-halfHeight))
	deviation := float32(featureNumber(c["height_deviation"]))
	value := mean + float32(random.NextGaussian())*deviation
	if value < 0 {
		value = 0
	}
	if value > float32(maxHeight) {
		value = float32(maxHeight)
	}
	return int32(value)
}
func speleothemCanPool(r *featureRegion, c map[string]any, p featurePos) bool {
	name := normalizeResource(speleothemStateName(r, p))
	if name == "water" || name == speleothemBlockName(c["base_block"], "minecraft:dripstone_block") || name == speleothemBlockName(c["pointed_block"], "minecraft:pointed_dripstone") || speleothemWater(r, featurePos{p.x, p.y + 1, p.z}) {
		return false
	}
	for _, d := range [...]featurePos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
		if !speleothemAdjacent(r, featurePos{p.x + d.x, p.y, p.z + d.z}) {
			return false
		}
	}
	return speleothemAdjacent(r, featurePos{p.x, p.y - 1, p.z})
}
func speleothemWater(r *featureRegion, p featurePos) bool {
	if p.y < r.MinY() || p.y > r.MaxY() {
		return false
	}
	return world.BlockStateHasWaterFluid(r.nativeState(p).CanonicalID(r.c.BlockRegistry()))
}
func speleothemAdjacent(r *featureRegion, p featurePos) bool {
	name := speleothemStateName(r, p)
	return blockTagContains("base_stone_overworld", normalizeResource(name)) || speleothemWater(r, p)
}
func speleothemReplaceBase(r *featureRegion, c map[string]any, p featurePos, count, dy int32) {
	base := speleothemDefaultState(r, speleothemBlockName(c["base_block"], "minecraft:dripstone_block"))
	for i := int32(0); i < count; i++ {
		name := speleothemStateName(r, p)
		if !featureBlockHolderContains(c["replaceable_blocks"], name) {
			return
		}
		r.setWithFlags(p, base, 2)
		p.y += dy
	}
}
func speleothemGrow(r *featureRegion, c map[string]any, start featurePos, dir, height int32, merge bool, base, point string) {
	support := featurePos{start.x, start.y - dir, start.z}
	name := speleothemStateName(r, support)
	if !featureBlockHolderContains(c["replaceable_blocks"], name) && normalizeResource(name) != normalizeResource(base) {
		return
	}
	if height <= 0 {
		return
	}
	pointID, err := gamedata.BlockStateID(normalizeResource(point), nil)
	if err != nil {
		panic(err)
	}
	direction := "down"
	if dir > 0 {
		direction = "up"
	}
	pointID, err = gamedata.BlockStateWithProperty(pointID, "vertical_direction", direction)
	if err != nil {
		panic(err)
	}
	for i := int32(0); i < height; i++ {
		thickness := "tip"
		if i == height-1 && merge {
			thickness = "tip_merge"
		} else if height >= 3 {
			if i == 0 {
				thickness = "base"
			} else if i < height-2 {
				thickness = "middle"
			} else if i == height-2 {
				thickness = "frustum"
			}
		} else if i == height-2 && height >= 2 {
			thickness = "frustum"
		}
		id, e := gamedata.BlockStateWithProperty(pointID, "thickness", thickness)
		if e != nil {
			panic(e)
		}
		id, e = gamedata.BlockStateWithProperty(id, "waterlogged", speleothemWater(r, start))
		if e != nil {
			panic(e)
		}
		r.setWithFlags(start, world.ResolveStateID(r.c.BlockRegistry(), id), 2)
		start.y += dir
	}
}
func speleothemAbs(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}
func speleothemSetNamed(r *featureRegion, p featurePos, name string) {
	r.setWithFlags(p, speleothemDefaultState(r, name), 2)
}
func speleothemDefaultState(r *featureRegion, name string) world.NativeState {
	id, err := gamedata.BlockStateID(name, nil)
	if err != nil {
		panic(err)
	}
	return world.ResolveStateID(r.c.BlockRegistry(), id)
}
func speleothemStateName(r *featureRegion, p featurePos) string {
	name, ok := gamedata.BlockStateName(r.nativeState(p).CanonicalID(r.c.BlockRegistry()))
	if !ok {
		panic("overworld: invalid speleothem state")
	}
	return name
}
