package overworld

import (
	"math"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// placeModernLargeDripstone ports LargeDripstoneFeature.place and its nested
// LargeDripstone/WindOffsetter logic. All accesses go through featureRegion so
// neighboring chunks remain visible to the feature overlay.
func placeModernLargeDripstone(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	if !largeDripstoneEmptyOrWater(region, origin) {
		return false
	}

	searchRange := int32(largeDripstoneInt(config["floor_to_ceiling_search_range"], 30))
	column, ok := largeDripstoneScan(region, origin, searchRange, config["replaceable_blocks"])
	if !ok {
		return false
	}
	columnHeight := column.ceiling - column.floor - 1
	if columnHeight < 4 {
		return false
	}

	radiusMin, radiusMax := intProviderBounds(config["column_radius"])
	maxRadius := int32(float32(columnHeight) * largeDripstoneFloat(config["max_column_radius_to_cave_height_ratio"], .1))
	if maxRadius < radiusMin { maxRadius = radiusMin }
	if maxRadius > radiusMax { maxRadius = radiusMax }
	radius := radiusMin + random.NextInt(maxRadius-radiusMin+1)

	stalactite := largeDripstoneMake(featurePos{origin.x, column.ceiling - 1, origin.z}, false, radius, config["stalactite_bluntness"], config["height_scale"], random)
	stalagmite := largeDripstoneMake(featurePos{origin.x, column.floor + 1, origin.z}, true, radius, config["stalagmite_bluntness"], config["height_scale"], random)
	wind := largeDripstoneWind{}
	minWindRadius := int32(largeDripstoneInt(config["min_radius_for_wind"], 0))
	minWindBluntness := largeDripstoneFloat(config["min_bluntness_for_wind"], 0)
	if largeDripstoneSuitableForWind(stalactite, minWindRadius, float64(minWindBluntness)) && largeDripstoneSuitableForWind(stalagmite, minWindRadius, float64(minWindBluntness)) {
		speed := largeDripstoneSampleFloat(config["wind_speed"], 0, random)
		direction := largeDripstoneRandomFloat(random, 0, float32(math.Pi))
		wind = largeDripstoneWind{originY: origin.y, x: float64(vanilla.Cos(float64(direction)) * speed), z: float64(vanilla.Sin(float64(direction)) * speed), maxOffset: 16-radius}
	}

	stalactiteEmbedded := largeDripstoneEmbed(region, &stalactite, wind)
	stalagmiteEmbedded := largeDripstoneEmbed(region, &stalagmite, wind)
	if stalactiteEmbedded {
		largeDripstonePlace(region, stalactite, wind, random)
	}
	if stalagmiteEmbedded {
		largeDripstonePlace(region, stalagmite, wind, random)
	}
	return true
}

type largeDripstoneColumn struct { floor, ceiling int32 }

func largeDripstoneScan(region *featureRegion, origin featurePos, reach int32, replaceable any) (largeDripstoneColumn, bool) {
	if !largeDripstoneEmptyOrWater(region, origin) { return largeDripstoneColumn{}, false }
	scan := func(direction int32) (int32, bool) {
		p := origin
		for i := int32(1); i < reach && largeDripstoneEmptyOrWater(region, p); i++ {
			p.y += direction
			if p.y < region.MinY() || p.y > region.MaxY() { return 0, false }
		}
		if p.y < region.MinY() || p.y > region.MaxY() { return 0, false }
		return p.y, largeDripstoneBaseOrLava(largeDripstoneName(region, p), replaceable)
	}
	ceiling, hasCeiling := scan(1)
	floor, hasFloor := scan(-1)
	return largeDripstoneColumn{floor, ceiling}, hasFloor && hasCeiling
}

func largeDripstoneMake(root featurePos, pointingUp bool, radius int32, bluntness, scale any, random *vanilla.Random) largeDripstoneShape {
	return largeDripstoneShape{root: root, pointingUp: pointingUp, radius: radius, bluntness: float64(largeDripstoneSampleFloat(bluntness, .1, random)), scale: float64(largeDripstoneSampleFloat(scale, 1, random))}
}

type largeDripstoneShape struct {
	root featurePos
	pointingUp bool
	radius int32
	bluntness, scale float64
}

func (s largeDripstoneShape) heightAtRadius(distance float64) int32 {
	if distance < s.bluntness { distance = s.bluntness }
	r := distance / float64(s.radius) * .384
	part1 := .75 * math.Pow(r, 1.3333333333333333)
	part2 := math.Pow(r, .6666666666666666)
	part3 := .3333333333333333 * math.Log(r)
	height := s.scale * (part1 - part2 - part3)
	if height < 0 { height = 0 }
	return int32(height / .384 * float64(s.radius))
}

type largeDripstoneWind struct { originY int32; x, z float64; maxOffset int32; noWind bool }

func largeDripstoneWindOffset(p featurePos, wind largeDripstoneWind) featurePos {
	if wind.noWind { return p }
	dy := float64(wind.originY - p.y)
	dx, dz := int32(math.Floor(wind.x*dy)), int32(math.Floor(wind.z*dy))
	if dx < -wind.maxOffset { dx = -wind.maxOffset }; if dx > wind.maxOffset { dx = wind.maxOffset }
	if dz < -wind.maxOffset { dz = -wind.maxOffset }; if dz > wind.maxOffset { dz = wind.maxOffset }
	return featurePos{p.x+dx, p.y, p.z+dz}
}

func largeDripstoneEmbed(region *featureRegion, shape *largeDripstoneShape, wind largeDripstoneWind) bool {
	for shape.radius > 1 {
		root := shape.root
		tries := shape.heightAtRadius(0)
		if tries > 10 { tries = 10 }
		for i := int32(0); i < tries; i++ {
			name, _ := region.State(root)
			if normalizeResource(name) == "lava" { return false }
			if largeDripstoneCircleSolid(region, largeDripstoneWindOffset(root, wind), shape.radius) {
				shape.root = root
				return true
			}
			if shape.pointingUp { root.y-- } else { root.y++ }
		}
		shape.radius /= 2
	}
	return false
}

func largeDripstoneCircleSolid(region *featureRegion, center featurePos, radius int32) bool {
	if largeDripstoneEmptyWaterOrLava(region, center) { return false }
	step := float32(6) / float32(radius)
	for angle := float32(0); angle < float32(2*math.Pi); angle += step {
		dx := int32(vanilla.Cos(float64(angle)) * float32(radius))
		dz := int32(vanilla.Sin(float64(angle)) * float32(radius))
		if largeDripstoneEmptyWaterOrLava(region, featurePos{center.x+dx, center.y, center.z+dz}) { return false }
	}
	return true
}

func largeDripstonePlace(region *featureRegion, shape largeDripstoneShape, wind largeDripstoneWind, random *vanilla.Random) {
	dripstone := featureRequireState(region, "minecraft:dripstone_block")
	for dx := -shape.radius; dx <= shape.radius; dx++ {
		for dz := -shape.radius; dz <= shape.radius; dz++ {
			currentRadius := float32(math.Sqrt(float64(dx*dx + dz*dz)))
			if currentRadius > float32(shape.radius) { continue }
			height := shape.heightAtRadius(float64(currentRadius))
			if height <= 0 { continue }
			if random.NextFloat() < .2 { height = int32(float32(height) * largeDripstoneRandomFloat(random, .8, 1)) }
			pos := featurePos{shape.root.x+dx, shape.root.y, shape.root.z+dz}
			hasBeenOutOfStone := false
			maxY := int32(math.MaxInt32)
			if shape.pointingUp { maxY = region.Height(pos.x, pos.z, false) }
			for i := int32(0); i < height && pos.y < maxY; i++ {
				adjusted := largeDripstoneWindOffset(pos, wind)
				if largeDripstoneEmptyWaterOrLava(region, adjusted) {
					hasBeenOutOfStone = true
					region.setWithFlags(adjusted, dripstone, 2)
				} else if hasBeenOutOfStone && blockTagContains("base_stone_overworld", largeDripstoneName(region, adjusted)) {
					break
				}
				if shape.pointingUp { pos.y++ } else { pos.y-- }
			}
		}
	}
}

func largeDripstoneName(region *featureRegion, p featurePos) string { name, _ := region.State(p); return normalizeResource(name) }

func largeDripstoneEmptyOrWater(region *featureRegion, p featurePos) bool { return largeDripstoneEmptyOrWaterName(largeDripstoneName(region, p)) }
func largeDripstoneEmptyWaterOrLava(region *featureRegion, p featurePos) bool {
	name := largeDripstoneName(region, p)
	return largeDripstoneEmptyOrWaterName(name) || name == "lava"
}
func largeDripstoneEmptyOrWaterName(name string) bool {
	switch normalizeResource(name) { case "air", "cave_air", "void_air", "water": return true }
	return false
}
func largeDripstoneBaseOrLava(name string, replaceable any) bool {
	name = normalizeResource(name)
	return name == "dripstone_block" || name == "lava" || featureBlockHolderContains(replaceable, name)
}

func largeDripstoneSuitableForWind(shape largeDripstoneShape, minRadius int32, minBluntness float64) bool {
	return shape.radius >= minRadius && shape.bluntness >= minBluntness
}
func largeDripstoneInt(v any, fallback int) int { if n, ok := v.(float64); ok { return int(n) }; return fallback }
func largeDripstoneFloat(v any, fallback float32) float32 { if n, ok := v.(float64); ok { return float32(n) }; return fallback }
func largeDripstoneSampleFloat(v any, fallback float32, random *vanilla.Random) float32 {
	if n, ok := v.(float64); ok { return float32(n) }
	provider, ok := v.(map[string]any); if !ok { return fallback }
	switch normalizeResource(largeDripstoneString(provider["type"], "")) {
	case "constant": return largeDripstoneFloat(provider["value"], fallback)
	case "uniform": return largeDripstoneRandomFloat(random, largeDripstoneFloat(provider["min_inclusive"], fallback), largeDripstoneFloat(provider["max_exclusive"], fallback))
	case "clamped_normal":
		mean := largeDripstoneFloat(provider["mean"], fallback); deviation := largeDripstoneFloat(provider["deviation"], 0)
		value := mean + float32(random.NextGaussian())*deviation
		minimum := largeDripstoneFloat(provider["min"], fallback); maximum := largeDripstoneFloat(provider["max"], fallback)
		if value < minimum { value = minimum }; if value > maximum { value = maximum }; return value
	default: panic("overworld: unsupported large dripstone float provider")
	}
}
func largeDripstoneRandomFloat(random *vanilla.Random, min, max float32) float32 { return min + (max-min)*random.NextFloat() }
func largeDripstoneString(value any, fallback string) string {
	if text, ok := value.(string); ok { return text }
	return fallback
}
