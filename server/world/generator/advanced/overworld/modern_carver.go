package overworld

import (
	"fmt"
	"math"
	"math/bits"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// ModernCarverAquifer supplies Java aquifer and surface-material decisions during carving.
type ModernCarverAquifer interface {
	CarveSubstance(x, y, z int32, density float32) (material world.Block, scheduleFluidTick bool)
	CarveTopMaterial(x, y, z int32, hasFluid bool) (material world.Block, ok bool)
}

// Carve applies each biome's configured Java carvers from the full 17x17 source-chunk neighborhood.
func (f *ModernFeatures) Carve(pos world.ChunkPos, c *chunk.Chunk, aquifer ModernCarverAquifer) world.GenerationResult {
	if aquifer == nil {
		panic("overworld: nil Java carver aquifer")
	}
	minY, maxY := int32(c.Range()[0])+1, int32(c.Range()[1])-7
	mask := newCarvingMask(minY, maxY)
	result := world.GenerationResult{}
	for dx := int32(-8); dx <= 8; dx++ {
		for dz := int32(-8); dz <= 8; dz++ {
			sourceX, sourceZ := int32(pos[0])+dx, int32(pos[1])+dz
			biomeName := normalizeJavaResource(f.source.JavaNoiseBiomeAt(sourceX<<2, 0, sourceZ<<2))
			biome, ok := f.biomes[biomeName]
			if !ok {
				panic("overworld: Java noise biome source returned unknown carver biome " + biomeName)
			}
			for index, name := range biome.carvers {
				cfg, ok := f.carvers[name]
				if !ok {
					panic("overworld: missing configured Java carver " + name)
				}
				r := largeFeatureRandom(f.seed+int64(index), sourceX, sourceZ)
				probability, ok := cfg.config["probability"].(float64)
				if !ok {
					panic("overworld: malformed Java carver probability " + name)
				}
				if float64(r.NextFloat()) > probability {
					continue
				}
				switch cfg.typeName {
				case "cave":
					f.carveCave(c, pos, cfg.config, sourceX, sourceZ, &r, minY, maxY, mask)
				case "canyon":
					f.carveCanyon(c, pos, cfg.config, sourceX, sourceZ, &r, minY, maxY, mask)
				default:
					panic("overworld: unsupported Java configured carver " + cfg.typeName)
				}
			}
		}
	}
	mask.apply(pos, c, aquifer, &result)
	return result
}

type carvingMask struct {
	minY   int32
	height int32
	bits   []uint64
}

func newCarvingMask(minY, maxY int32) *carvingMask {
	height := maxY - minY + 1
	bitCount := int(256 * height)
	return &carvingMask{minY: minY, height: height, bits: make([]uint64, (bitCount+63)/64)}
}

func (m *carvingMask) set(x, y, z int32) {
	index := int(y-m.minY) + int(z+(x<<4))*int(m.height)
	m.bits[index>>6] |= uint64(1) << uint(index&63)
}

func (m *carvingMask) nextSetBit(start int) int {
	wordIndex := start >> 6
	if wordIndex >= len(m.bits) {
		return -1
	}
	word := m.bits[wordIndex] & (^uint64(0) << uint(start&63))
	for {
		if word != 0 {
			return wordIndex*64 + bits.TrailingZeros64(word)
		}
		wordIndex++
		if wordIndex >= len(m.bits) {
			return -1
		}
		word = m.bits[wordIndex]
	}
}

func (m *carvingMask) nextClearBit(start int) int {
	wordIndex := start >> 6
	if wordIndex >= len(m.bits) {
		return len(m.bits) * 64
	}
	word := ^m.bits[wordIndex] & (^uint64(0) << uint(start&63))
	for {
		if word != 0 {
			return wordIndex*64 + bits.TrailingZeros64(word)
		}
		wordIndex++
		if wordIndex >= len(m.bits) {
			return len(m.bits) * 64
		}
		word = ^m.bits[wordIndex]
	}
}

func (m *carvingMask) apply(pos world.ChunkPos, c *chunk.Chunk, aquifer ModernCarverAquifer, result *world.GenerationResult) {
	for start := m.nextSetBit(0); start >= 0; {
		end := m.nextClearBit(start) - 1
		startColumn, endColumn := start/int(m.height), end/int(m.height)
		for column := startColumn; column <= endColumn; column++ {
			x, z := int32(column>>4&15), int32(column&15)
			base := column * int(m.height)
			bottom := int32(maxInt(start-base, 0)) + m.minY
			top := int32(minInt(end-base, int(m.height)-1)) + m.minY
			m.applySpan(pos, c, x, z, bottom, top, aquifer, result)
		}
		start = m.nextSetBit(end + 1)
	}
}

func (m *carvingMask) applySpan(pos world.ChunkPos, c *chunk.Chunk, x, z, bottomY, topY int32, aquifer ModernCarverAquifer, result *world.GenerationResult) {
	hasGrass := false
	worldX, worldZ := int32(pos[0])<<4+x, int32(pos[1])<<4+z
	for y := topY; y >= bottomY; y-- {
		current := carverNativeState(c, worldX, y, worldZ)
		currentJava, err := world.JavaState(c.BlockRegistry(), current)
		if err != nil {
			panic(err)
		}
		if javaBlockTagContains("uncarvable", normalizeJavaResource(currentJava.Name)) {
			continue
		}
		if currentJava.Name == "minecraft:grass_block" || currentJava.Name == "minecraft:mycelium" {
			hasGrass = true
		}
		material, schedule := aquifer.CarveSubstance(worldX, y, worldZ, 0)
		if material == nil {
			continue
		}
		state := resolveCarverMaterial(c, material)
		world.SetNativeState(c, uint8(x), int16(y), uint8(z), state)
		markCarverPostProcessing(result, c, worldX, y, worldZ, schedule && featureStateHasFluid(c, state))
		if hasGrass && y > int32(c.Range()[0]) {
			belowY := y - 1
			below := carverNativeState(c, worldX, belowY, worldZ)
			belowJava, err := world.JavaState(c.BlockRegistry(), below)
			if err != nil {
				panic(err)
			}
			if belowJava.Name == "minecraft:dirt" {
				hasFluid := featureStateHasFluid(c, state)
				top, ok := aquifer.CarveTopMaterial(worldX, belowY, worldZ, hasFluid)
				if ok && top != nil {
					topState := resolveCarverMaterial(c, top)
					world.SetNativeState(c, uint8(x), int16(belowY), uint8(z), topState)
					markCarverPostProcessing(result, c, worldX, belowY, worldZ, featureStateHasFluid(c, topState))
				}
			}
		}
	}
}

func largeFeatureRandom(seed int64, chunkX, chunkZ int32) java26.Random {
	r := java26.NewLegacyRandom(seed)
	x, z := r.NextLong(), r.NextLong()
	return java26.NewLegacyRandom(int64(chunkX)*x ^ int64(chunkZ)*z ^ seed)
}

func (f *ModernFeatures) carveCave(c *chunk.Chunk, target world.ChunkPos, config map[string]any, sourceX, sourceZ int32, r *java26.Random, minY, maxY int32, mask *carvingMask) {
	count := sampleJavaInt(config["count"], r)
	for range count {
		x := float64((sourceX << 4) + int32(r.NextInt(16)))
		y := float64(sampleJavaHeight(config["y"].(map[string]any), r, int32(c.Range()[0]), int32(c.Range()[1])+1, f.source.SeaLevel()))
		z := float64((sourceZ << 4) + int32(r.NextInt(16)))
		horizontal := sampleJavaFloat(config["horizontal_radius_multiplier"].(map[string]any), r)
		vertical := sampleJavaFloat(config["vertical_radius_multiplier"].(map[string]any), r)
		startVertical := float32(1)
		if raw, exists := config["start_vertical_radius_multiplier"]; exists {
			startVertical = sampleJavaFloat(raw.(map[string]any), r)
		}
		floor := float64(sampleJavaFloat(config["floor_level"].(map[string]any), r))
		skip := func(xd, yd, zd float64, _ int32) bool { return yd <= floor || xd*xd+yd*yd+zd*zd >= 1 }
		tunnels := 1
		if r.NextInt(4) == 0 {
			yScale := float64(sampleJavaFloat(config["room_vertical_radius_multiplier"].(map[string]any), r))
			thickness := float32(1 + r.NextFloat()*6)
			horizontalRadius := 1.5 + float64(thickness)
			carveEllipsoid(target, x+1, y, z, horizontalRadius, horizontalRadius*yScale, skip, minY, maxY, mask)
			tunnels += int(r.NextInt(4))
		}
		for range tunnels {
			horizontalRotation := r.NextFloat() * float32(2*math.Pi)
			verticalRotation := (r.NextFloat() - 0.5) / 4
			thickness := sampleJavaFloat(config["thickness"].(map[string]any), r)
			if config["weird_thickness_bias"] == true && r.NextInt(10) == 0 {
				thickness *= r.NextFloat()*r.NextFloat()*3 + 1
			}
			maxDistance := int32(112)
			distance := maxDistance - r.NextInt(maxDistance/4)
			tunnelRandom := r.NextLong()
			f.caveTunnel(c, target, tunnelRandom, x, y, z, float64(horizontal), float64(vertical), float32(thickness), horizontalRotation, verticalRotation, 0, distance, float64(startVertical), skip, minY, maxY, mask)
		}
	}
}

func (f *ModernFeatures) caveTunnel(c *chunk.Chunk, target world.ChunkPos, seed int64, x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier float64, thickness, horizontalRotation, verticalRotation float32, step, distance int32, yScale float64, skip func(float64, float64, float64, int32) bool, minY, maxY int32, mask *carvingMask) {
	r := java26.NewLegacyRandom(seed)
	split := r.NextInt(distance/2) + distance/4
	steep := r.NextInt(6) == 0
	var yRota, xRota float32
	for current := step; current < distance; current++ {
		hr := 1.5 + float64(java26.Sin(float64(float32(math.Pi)*float32(current)/float32(distance)))*thickness)
		vr := hr * yScale
		cosX := java26.Cos(float64(verticalRotation))
		x += float64(java26.Cos(float64(horizontalRotation)) * cosX)
		y += float64(java26.Sin(float64(verticalRotation)))
		z += float64(java26.Sin(float64(horizontalRotation)) * cosX)
		if steep {
			verticalRotation *= .92
		} else {
			verticalRotation *= .7
		}
		verticalRotation += xRota * .1
		horizontalRotation += yRota * .1
		xRota *= .9
		yRota *= .75
		xRota += (r.NextFloat() - r.NextFloat()) * r.NextFloat() * 2
		yRota += (r.NextFloat() - r.NextFloat()) * r.NextFloat() * 4
		if current == split && thickness > 1 {
			f.caveTunnel(c, target, r.NextLong(), x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier, r.NextFloat()*.5+.5, horizontalRotation-float32(math.Pi/2), verticalRotation/3, current, distance, 1, skip, minY, maxY, mask)
			f.caveTunnel(c, target, r.NextLong(), x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier, r.NextFloat()*.5+.5, horizontalRotation+float32(math.Pi/2), verticalRotation/3, current, distance, 1, skip, minY, maxY, mask)
			return
		}
		if r.NextInt(4) != 0 {
			if !carverCanReach(target, x, z, current, distance, thickness) {
				return
			}
			carveEllipsoid(target, x, y, z, hr*horizontalRadiusMultiplier, vr*verticalRadiusMultiplier, skip, minY, maxY, mask)
		}
	}
}

func carverCanReach(target world.ChunkPos, x, z float64, current, total int32, thickness float32) bool {
	midX, midZ := float64((int32(target[0])<<4)+8), float64((int32(target[1])<<4)+8)
	dx, dz := x-midX, z-midZ
	remaining := float64(total - current)
	rr := float64(thickness + 2.0 + 16.0)
	return dx*dx+dz*dz-remaining*remaining <= rr*rr
}

func carveEllipsoid(target world.ChunkPos, x, y, z, horizontalRadius, verticalRadius float64, skip func(float64, float64, float64, int32) bool, minY, maxY int32, mask *carvingMask) {
	centerX, centerZ := float64((int32(target[0])<<4)+8), float64((int32(target[1])<<4)+8)
	if math.Abs(x-centerX) > 16+horizontalRadius*2 || math.Abs(z-centerZ) > 16+horizontalRadius*2 {
		return
	}
	chunkMinX, chunkMinZ := int32(target[0])<<4, int32(target[1])<<4
	minX := maxInt(int(math.Floor(x-horizontalRadius))-int(chunkMinX)-1, 0)
	maxX := minInt(int(math.Floor(x+horizontalRadius))-int(chunkMinX), 15)
	minZ := maxInt(int(math.Floor(z-horizontalRadius))-int(chunkMinZ)-1, 0)
	maxZ := minInt(int(math.Floor(z+horizontalRadius))-int(chunkMinZ), 15)
	minCarveY := int32(maxInt(int(math.Floor(y-verticalRadius))-1, int(minY)))
	maxCarveY := int32(minInt(int(math.Floor(y+verticalRadius))+1, int(maxY)))
	for lx := minX; lx <= maxX; lx++ {
		wx := chunkMinX + int32(lx)
		xd := (float64(wx) + .5 - x) / horizontalRadius
		for lz := minZ; lz <= maxZ; lz++ {
			wz := chunkMinZ + int32(lz)
			zd := (float64(wz) + .5 - z) / horizontalRadius
			if xd*xd+zd*zd >= 1 {
				continue
			}
			for wy := maxCarveY; wy > minCarveY; wy-- {
				yd := (float64(wy) - .5 - y) / verticalRadius
				if !skip(xd, yd, zd, wy) {
					mask.set(int32(lx), wy, int32(lz))
				}
			}
		}
	}
}

func carverNativeState(c *chunk.Chunk, x, y, z int32) world.NativeState {
	return world.NativeStateAt(c, uint8(x&15), int16(y), uint8(z&15))
}

func resolveCarverMaterial(c *chunk.Chunk, material world.Block) world.NativeState {
	name, properties := material.EncodeBlock()
	rid, ok := c.BlockRegistry().StateToRuntimeID(name, properties)
	if !ok {
		panic(fmt.Sprintf("overworld: aquifer returned unregistered native carver material %s%v", name, properties))
	}
	return world.NativeState{Block: rid, Liquid: c.BlockRegistry().AirRuntimeID()}
}

func markCarverPostProcessing(result *world.GenerationResult, c *chunk.Chunk, x, y, z int32, mark bool) {
	if mark {
		result.MarkForPostProcessing(cube.Pos{int(x), int(y), int(z)}, c.Range())
	}
}

func javaIntProviderValue(value any) int32 {
	n, ok := value.(float64)
	if !ok || n < -2147483648 || n > 2147483647 || n != math.Trunc(n) {
		panic(fmt.Sprintf("overworld: malformed Java IntProvider integer %v", value))
	}
	return int32(n)
}

func javaIntProviderField(provider map[string]any, field string) int32 {
	value, exists := provider[field]
	if !exists {
		panic("overworld: Java IntProvider has no " + field)
	}
	return javaIntProviderValue(value)
}

// javaIntProviderBounds retains Source minInclusive/maxInclusive, including
// zero-weight entries and clamped providers' intersection with their source.
func javaIntProviderBounds(provider any) (int32, int32) {
	if _, ok := provider.(float64); ok {
		value := javaIntProviderValue(provider)
		return value, value
	}
	p, ok := provider.(map[string]any)
	if !ok {
		panic("overworld: malformed Java IntProvider")
	}
	typeName, ok := p["type"].(string)
	if !ok {
		panic("overworld: Java IntProvider has no type")
	}
	switch normalizeJavaResource(typeName) {
	case "constant":
		value := javaIntProviderField(p, "value")
		return value, value
	case "uniform", "biased_to_bottom", "very_biased_to_bottom", "clamped_normal":
		return javaIntProviderField(p, "min_inclusive"), javaIntProviderField(p, "max_inclusive")
	case "trapezoid":
		return javaIntProviderField(p, "min"), javaIntProviderField(p, "max")
	case "clamped":
		minimum, maximum := javaIntProviderBounds(p["source"])
		return max(minimum, javaIntProviderField(p, "min_inclusive")), min(maximum, javaIntProviderField(p, "max_inclusive"))
	case "weighted_list":
		distribution, ok := p["distribution"].([]any)
		if !ok || len(distribution) == 0 {
			panic("overworld: malformed weighted Java IntProvider")
		}
		minimum, maximum := int32(math.MaxInt32), int32(math.MinInt32)
		for _, raw := range distribution {
			entry, ok := raw.(map[string]any)
			if !ok {
				panic("overworld: malformed weighted Java IntProvider entry")
			}
			entryMin, entryMax := javaIntProviderBounds(entry["data"])
			minimum, maximum = min(minimum, entryMin), max(maximum, entryMax)
		}
		return minimum, maximum
	default:
		panic("overworld: unsupported Java IntProvider " + normalizeJavaResource(typeName))
	}
}

func sampleJavaIntOrUniform(provider any, minimum, maximum int, random *java26.Random) int32 {
	if provider != nil {
		return sampleJavaInt(provider, random)
	}
	if minimum == maximum {
		return int32(minimum)
	}
	return int32(minimum) + random.NextInt(int32(maximum-minimum+1))
}

func sampleJavaInt(provider any, r *java26.Random) int32 {
	if _, ok := provider.(float64); ok {
		return javaIntProviderValue(provider)
	}
	p, ok := provider.(map[string]any)
	if !ok {
		panic("overworld: malformed Java IntProvider")
	}
	typeName, ok := p["type"].(string)
	if !ok {
		panic("overworld: Java IntProvider has no type")
	}
	switch normalizeJavaResource(typeName) {
	case "constant":
		return javaIntProviderField(p, "value")
	case "uniform", "biased_to_bottom", "very_biased_to_bottom":
		minimum := javaIntProviderField(p, "min_inclusive")
		maximum := javaIntProviderField(p, "max_inclusive")
		if maximum < minimum {
			panic("overworld: invalid Java IntProvider bounds")
		}
		width := maximum - minimum
		switch normalizeJavaResource(typeName) {
		case "uniform":
			return minimum + r.NextInt(width+1)
		case "biased_to_bottom":
			return minimum + r.NextInt(r.NextInt(width+1)+1)
		default:
			return minimum + r.NextInt(r.NextInt(r.NextInt(width+1)+1)+1)
		}
	case "trapezoid":
		minimum := javaIntProviderField(p, "min")
		maximum := javaIntProviderField(p, "max")
		plateau := javaIntProviderField(p, "plateau")
		if maximum < minimum || plateau > maximum-minimum {
			panic("overworld: invalid Java trapezoid IntProvider")
		}
		if plateau == 0 && maximum == -minimum {
			return r.NextInt(maximum+1) - r.NextInt(maximum+1)
		}
		width := maximum - minimum
		if plateau == width {
			return minimum + r.NextInt(width+1)
		}
		plateauStart := (width - plateau) / 2
		plateauEnd := width - plateauStart
		return minimum + r.NextInt(plateauEnd+1) + r.NextInt(plateauStart+1)
	case "weighted_list":
		distribution, ok := p["distribution"].([]any)
		if !ok || len(distribution) == 0 {
			panic("overworld: malformed weighted Java IntProvider")
		}
		total := int32(0)
		for _, raw := range distribution {
			entry, ok := raw.(map[string]any)
			if !ok {
				panic("overworld: malformed weighted Java IntProvider entry")
			}
			weight := javaIntProviderValue(entry["weight"])
			if weight < 0 || total > int32(2147483647)-weight {
				panic("overworld: invalid weighted Java IntProvider weight")
			}
			total += weight
		}
		if total == 0 {
			panic("overworld: weighted Java IntProvider has no positive weight")
		}
		choice := r.NextInt(total)
		for _, raw := range distribution {
			entry := raw.(map[string]any)
			weight := javaIntProviderValue(entry["weight"])
			if choice < weight {
				value, exists := entry["data"]
				if !exists {
					panic("overworld: weighted Java IntProvider entry has no data")
				}
				return sampleJavaInt(value, r)
			}
			choice -= weight
		}
		panic("overworld: weighted Java IntProvider selection escaped its total")
	case "clamped":
		source, exists := p["source"]
		if !exists {
			panic("overworld: clamped Java IntProvider has no source")
		}
		minimum := javaIntProviderField(p, "min_inclusive")
		maximum := javaIntProviderField(p, "max_inclusive")
		if maximum < minimum {
			panic("overworld: invalid clamped Java IntProvider bounds")
		}
		value := sampleJavaInt(source, r)
		if value < minimum {
			return minimum
		}
		if value > maximum {
			return maximum
		}
		return value
	case "clamped_normal":
		mean, meanOK := p["mean"].(float64)
		deviation, deviationOK := p["deviation"].(float64)
		if !meanOK || !deviationOK {
			panic("overworld: malformed clamped_normal Java IntProvider")
		}
		minimum := javaIntProviderField(p, "min_inclusive")
		maximum := javaIntProviderField(p, "max_inclusive")
		if maximum < minimum {
			panic("overworld: invalid clamped_normal Java IntProvider bounds")
		}
		value := float32(mean) + float32(r.NextGaussian())*float32(deviation)
		if value < float32(minimum) {
			value = float32(minimum)
		}
		if value > float32(maximum) {
			value = float32(maximum)
		}
		return int32(value)
	default:
		panic("overworld: unsupported Java IntProvider " + normalizeJavaResource(typeName))
	}
}

func sampleJavaFloat(provider map[string]any, r *java26.Random) float32 {
	typeName, _ := provider["type"].(string)
	if value, ok := provider["value"].(float64); ok {
		return float32(value)
	}
	min, ok1 := provider["min_inclusive"].(float64)
	max, ok2 := provider["max_exclusive"].(float64)
	if !ok2 {
		max, _ = provider["max"].(float64)
		ok2 = true
	}
	if !ok1 {
		min, _ = provider["min"].(float64)
		ok1 = true
	}
	if !ok1 || !ok2 {
		panic("overworld: malformed Java FloatProvider")
	}
	switch normalizeJavaResource(typeName) {
	case "uniform":
		return float32(min) + (float32(max)-float32(min))*r.NextFloat()
	case "trapezoid":
		plateau, _ := provider["plateau"].(float64)
		span := float32(max) - float32(min)
		lower := (span - float32(plateau)) / 2
		upper := span - lower
		return float32(min) + r.NextFloat()*upper + r.NextFloat()*lower
	case "clamped_normal":
		mean, meanOK := provider["mean"].(float64)
		deviation, deviationOK := provider["deviation"].(float64)
		if !meanOK || !deviationOK {
			panic("overworld: malformed clamped_normal Java FloatProvider")
		}
		value := float32(mean) + float32(r.NextGaussian())*float32(deviation)
		if value < float32(min) {
			return float32(min)
		}
		if value > float32(max) {
			return float32(max)
		}
		return value
	case "constant":
		return float32(min)
	default:
		panic("overworld: unsupported Java FloatProvider " + normalizeJavaResource(typeName))
	}
}

func sampleJavaFloatValue(value any, r *java26.Random) float32 {
	if n, ok := value.(float64); ok {
		return float32(n)
	}
	provider, ok := value.(map[string]any)
	if !ok {
		panic("overworld: malformed Java FloatProvider")
	}
	return sampleJavaFloat(provider, r)
}

func (f *ModernFeatures) carveCanyon(c *chunk.Chunk, target world.ChunkPos, config map[string]any, sourceX, sourceZ int32, r *java26.Random, minY, maxY int32, mask *carvingMask) {
	shape := config["shape"].(map[string]any)
	x := float64((sourceX << 4) + int32(r.NextInt(16)))
	y := float64(sampleJavaHeight(config["y"].(map[string]any), r, int32(c.Range()[0]), int32(c.Range()[1])+1, f.source.SeaLevel()))
	z := float64((sourceZ << 4) + int32(r.NextInt(16)))
	rotation := r.NextFloat() * float32(2*math.Pi)
	vertical := sampleJavaFloat(config["vertical_rotation"].(map[string]any), r)
	yScale := float64(sampleJavaFloatValue(shape["y_scale"], r))
	thickness := sampleJavaFloat(shape["thickness"].(map[string]any), r)
	distance := int32(112 * sampleJavaFloat(shape["distance_factor"].(map[string]any), r))
	f.canyonTunnel(c, target, r.NextLong(), x, y, z, thickness, rotation, vertical, 0, distance, yScale, shape, minY, maxY, mask)
}

func (f *ModernFeatures) canyonTunnel(c *chunk.Chunk, target world.ChunkPos, seed int64, x, y, z float64, thickness, rotation, vertical float32, step, distance int32, yScale float64, shape map[string]any, minY, maxY int32, mask *carvingMask) {
	if distance <= 0 {
		return
	}
	r := java26.NewLegacyRandom(seed)
	width := make([]float32, c.Range()[1]-c.Range()[0]+1)
	factor := float32(1)
	smooth := int32(featureNumber(shape["width_smoothness"]))
	if smooth <= 0 {
		panic("overworld: invalid canyon width smoothness")
	}
	for i := range width {
		if i == 0 || r.NextInt(smooth) == 0 {
			factor = 1 + r.NextFloat()*r.NextFloat()
		}
		width[i] = factor * factor
	}
	var yRota, xRota float32
	for current := step; current < distance; current++ {
		hr := 1.5 + float64(java26.Sin(float64(float32(current)*float32(math.Pi)/float32(distance)))*thickness)
		vr := hr * yScale
		hr *= float64(sampleJavaFloat(shape["horizontal_radius_factor"].(map[string]any), &r))
		center := float32(1) - float32(math.Abs(float64(float32(.5)-float32(current)/float32(distance))))*2
		vDefault, _ := shape["vertical_radius_default_factor"].(float64)
		vCenter, _ := shape["vertical_radius_center_factor"].(float64)
		factor := float32(vDefault) + float32(vCenter)*center
		vr = float64(factor) * vr * float64(float32(.75)+float32(.25)*r.NextFloat())
		cosV := java26.Cos(float64(vertical))
		sinV := java26.Sin(float64(vertical))
		x += float64(java26.Cos(float64(rotation)) * cosV)
		y += float64(sinV)
		z += float64(java26.Sin(float64(rotation)) * cosV)
		vertical *= .7
		vertical += xRota * .05
		rotation += yRota * .05
		xRota *= .8
		yRota *= .5
		xRota += (r.NextFloat() - r.NextFloat()) * r.NextFloat() * 2
		yRota += (r.NextFloat() - r.NextFloat()) * r.NextFloat() * 4
		if r.NextInt(4) != 0 {
			if !carverCanReach(target, x, z, current, distance, thickness) {
				return
			}
			skip := func(xd, yd, zd float64, wy int32) bool {
				idx := wy - int32(c.Range()[0]) - 1
				if idx < 0 || idx >= int32(len(width)) {
					return true
				}
				return (xd*xd+zd*zd)*float64(width[idx])+yd*yd/6 >= 1
			}
			carveEllipsoid(target, x, y, z, hr, vr, skip, minY, maxY, mask)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
