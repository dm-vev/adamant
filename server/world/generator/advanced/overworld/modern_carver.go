package overworld

import (
	"fmt"
	"math"

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
	result := world.GenerationResult{}
	carved := make(map[carvedPos]struct{})
	for dz := int32(-8); dz <= 8; dz++ {
		for dx := int32(-8); dx <= 8; dx++ {
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
					f.carveCave(c, pos, cfg.config, sourceX, sourceZ, &r, minY, maxY, aquifer, carved, &result)
				case "canyon":
					f.carveCanyon(c, pos, cfg.config, sourceX, sourceZ, &r, minY, maxY, aquifer, carved, &result)
				default:
					panic("overworld: unsupported Java configured carver " + cfg.typeName)
				}
			}
		}
	}
	return result
}

type carvedPos struct{ x, y, z int32 }

func largeFeatureRandom(seed int64, chunkX, chunkZ int32) java26.Random {
	r := java26.NewLegacyRandom(seed)
	x, z := r.NextLong(), r.NextLong()
	return java26.NewLegacyRandom(int64(chunkX)*x ^ int64(chunkZ)*z ^ seed)
}

func (f *ModernFeatures) carveCave(c *chunk.Chunk, target world.ChunkPos, config map[string]any, sourceX, sourceZ int32, r *java26.Random, minY, maxY int32, aquifer ModernCarverAquifer, mask map[carvedPos]struct{}, result *world.GenerationResult) {
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
			carveEllipsoid(c, target, x+1, y, z, horizontalRadius, horizontalRadius*yScale, skip, minY, maxY, aquifer, mask, result)
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
			f.caveTunnel(c, target, tunnelRandom, x, y, z, float64(horizontal), float64(vertical), float32(thickness), horizontalRotation, verticalRotation, 0, distance, float64(startVertical), skip, minY, maxY, aquifer, mask, result)
		}
	}
}

func (f *ModernFeatures) caveTunnel(c *chunk.Chunk, target world.ChunkPos, seed int64, x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier float64, thickness, horizontalRotation, verticalRotation float32, step, distance int32, yScale float64, skip func(float64, float64, float64, int32) bool, minY, maxY int32, aquifer ModernCarverAquifer, mask map[carvedPos]struct{}, result *world.GenerationResult) {
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
			f.caveTunnel(c, target, r.NextLong(), x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier, r.NextFloat()*.5+.5, horizontalRotation-float32(math.Pi/2), verticalRotation/3, current, distance, 1, skip, minY, maxY, aquifer, mask, result)
			f.caveTunnel(c, target, r.NextLong(), x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier, r.NextFloat()*.5+.5, horizontalRotation+float32(math.Pi/2), verticalRotation/3, current, distance, 1, skip, minY, maxY, aquifer, mask, result)
			return
		}
		if r.NextInt(4) != 0 {
			if !carverCanReach(target, x, z, current, distance, float64(thickness)) {
				return
			}
			carveEllipsoid(c, target, x, y, z, hr*horizontalRadiusMultiplier, vr*verticalRadiusMultiplier, skip, minY, maxY, aquifer, mask, result)
		}
	}
}

func carverCanReach(target world.ChunkPos, x, z float64, current, total int32, thickness float64) bool {
	midX, midZ := float64((int32(target[0])<<4)+8), float64((int32(target[1])<<4)+8)
	dx, dz := x-midX, z-midZ
	remaining := float64(total - current)
	rr := thickness + 18
	return dx*dx+dz*dz-remaining*remaining <= rr*rr
}

func carveEllipsoid(c *chunk.Chunk, target world.ChunkPos, x, y, z, horizontalRadius, verticalRadius float64, skip func(float64, float64, float64, int32) bool, minY, maxY int32, aquifer ModernCarverAquifer, mask map[carvedPos]struct{}, result *world.GenerationResult) {
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
			hasGrass := false
			for wy := maxCarveY; wy > minCarveY; wy-- {
				yd := (float64(wy) - .5 - y) / verticalRadius
				if skip(xd, yd, zd, wy) {
					continue
				}
				p := carvedPos{wx, wy, wz}
				if _, seen := mask[p]; seen {
					continue
				}
				if !isCarverReplaceable(c, wx, wy, wz) {
					continue
				}
				current := carverNativeState(c, wx, wy, wz)
				currentJava, err := world.JavaState(c.BlockRegistry(), current)
				if err != nil {
					panic(err)
				}
				if currentJava.Name == "minecraft:grass_block" || currentJava.Name == "minecraft:mycelium" {
					hasGrass = true
				}
				mask[p] = struct{}{}
				material, schedule := aquifer.CarveSubstance(wx, wy, wz, 0)
				if material == nil {
					continue
				}
				state := resolveCarverMaterial(c, material)
				c.SetBlock(uint8(lx), int16(wy), uint8(lz), 0, state.Block)
				c.SetBlock(uint8(lx), int16(wy), uint8(lz), 1, state.Liquid)
				scheduleCarverFluidTick(c, result, wx, wy, wz, state, schedule, material)
				if hasGrass && wy > int32(c.Range()[0]) {
					belowY := wy - 1
					below := carverNativeState(c, wx, belowY, wz)
					belowJava, err := world.JavaState(c.BlockRegistry(), below)
					if err != nil {
						panic(err)
					}
					if belowJava.Name == "minecraft:dirt" {
						hasFluid := featureStateHasFluid(c, state)
						top, ok := aquifer.CarveTopMaterial(wx, belowY, wz, hasFluid)
						if ok && top != nil {
							topState := resolveCarverMaterial(c, top)
							c.SetBlock(uint8(lx), int16(belowY), uint8(lz), 0, topState.Block)
							c.SetBlock(uint8(lx), int16(belowY), uint8(lz), 1, topState.Liquid)
							scheduleCarverFluidTick(c, result, wx, belowY, wz, topState, featureStateHasFluid(c, topState), top)
						}
					}
				}
			}
		}
	}
}

func carverNativeState(c *chunk.Chunk, x, y, z int32) world.NativeState {
	return world.NativeState{Block: c.Block(uint8(x&15), int16(y), uint8(z&15), 0), Liquid: c.Block(uint8(x&15), int16(y), uint8(z&15), 1)}
}

func resolveCarverMaterial(c *chunk.Chunk, material world.Block) world.NativeState {
	name, properties := material.EncodeBlock()
	rid, ok := c.BlockRegistry().StateToRuntimeID(name, properties)
	if !ok {
		panic(fmt.Sprintf("overworld: aquifer returned unregistered native carver material %s%v", name, properties))
	}
	return world.NativeState{Block: rid, Liquid: c.BlockRegistry().AirRuntimeID()}
}

func scheduleCarverFluidTick(c *chunk.Chunk, result *world.GenerationResult, x, y, z int32, state world.NativeState, schedule bool, material world.Block) {
	if !schedule {
		return
	}
	name, _ := material.EncodeBlock()
	tick := int64(5)
	if name == "minecraft:lava" {
		tick = 30
	}
	block := state.Block
	if block == c.BlockRegistry().AirRuntimeID() {
		block = state.Liquid
	}
	result.ScheduledBlocks = append(result.ScheduledBlocks, chunk.ScheduledBlockUpdate{Pos: cube.Pos{int(x), int(y), int(z)}, Block: block, Tick: tick})
}

func isCarverReplaceable(c *chunk.Chunk, x, y, z int32) bool {
	state := carverNativeState(c, x, y, z)
	javaState, err := world.JavaState(c.BlockRegistry(), state)
	if err != nil {
		panic(err)
	}
	return !javaBlockTagContains("uncarvable", normalizeJavaResource(javaState.Name))
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
		if maximum < minimum || plateau < 0 || plateau > maximum-minimum {
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
		if plateau < 0 {
			panic("overworld: invalid trapezoid provider")
		}
		return float32(min) + (float32(max)-float32(min))*(r.NextFloat()+r.NextFloat()+float32(plateau))/(2+float32(plateau))
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

func (f *ModernFeatures) carveCanyon(c *chunk.Chunk, target world.ChunkPos, config map[string]any, sourceX, sourceZ int32, r *java26.Random, minY, maxY int32, aquifer ModernCarverAquifer, mask map[carvedPos]struct{}, result *world.GenerationResult) {
	shape := config["shape"].(map[string]any)
	x := float64((sourceX << 4) + int32(r.NextInt(16)))
	y := float64(sampleJavaHeight(config["y"].(map[string]any), r, int32(c.Range()[0]), int32(c.Range()[1])+1, f.source.SeaLevel()))
	z := float64((sourceZ << 4) + int32(r.NextInt(16)))
	rotation := r.NextFloat() * float32(2*math.Pi)
	vertical := sampleJavaFloat(config["vertical_rotation"].(map[string]any), r)
	yScale := float64(sampleJavaFloatValue(shape["y_scale"], r))
	thickness := sampleJavaFloat(shape["thickness"].(map[string]any), r)
	distance := int32(112 * sampleJavaFloat(shape["distance_factor"].(map[string]any), r))
	f.canyonTunnel(c, target, r.NextLong(), x, y, z, thickness, rotation, vertical, 0, distance, yScale, shape, minY, maxY, aquifer, mask, result)
}

func (f *ModernFeatures) canyonTunnel(c *chunk.Chunk, target world.ChunkPos, seed int64, x, y, z float64, thickness, rotation, vertical float32, step, distance int32, yScale float64, shape map[string]any, minY, maxY int32, aquifer ModernCarverAquifer, mask map[carvedPos]struct{}, result *world.GenerationResult) {
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
			if !carverCanReach(target, x, z, current, distance, float64(thickness)) {
				return
			}
			skip := func(xd, yd, zd float64, wy int32) bool {
				idx := wy - int32(c.Range()[0]) - 1
				if idx < 0 || idx >= int32(len(width)) {
					return true
				}
				return (xd*xd+zd*zd)*float64(width[idx])+yd*yd/6 >= 1
			}
			carveEllipsoid(c, target, x, y, z, hr, vr, skip, minY, maxY, aquifer, mask, result)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
