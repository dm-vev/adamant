package overworld

import (
	"fmt"
	"math"
	"math/bits"
	"sync"
	"sync/atomic"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/chunk"
)

// ModernCarverAquifer supplies Java aquifer and surface-material decisions during carving.
type ModernCarverAquifer interface {
	CarveSubstance(x, y, z int32, density float32) (material world.Block, scheduleFluidTick bool)
	CarveTopMaterial(x, y, z int32, hasFluid bool) (material world.Block, ok bool)
}

const (
	carverFlagComputed uint32 = 1 << iota
	carverFlagUncarvable
	carverFlagGrassOrMycelium
	carverFlagDirt
	carverFlagFluid
)

var defaultCarverFlags = sync.OnceValue(func() []atomic.Uint32 {
	return make([]atomic.Uint32, world.DefaultBlockRegistry.BlockCount())
})

func carverFlagsForDefaultRID(rid uint32) uint32 {
	table := defaultCarverFlags()
	if int(rid) >= len(table) {
		return 0
	}
	if flags := table[rid].Load(); flags != 0 {
		return flags
	}
	airRID := world.DefaultBlockRegistry.AirRuntimeID()
	canonical, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{Block: rid, Liquid: airRID})
	if err != nil {
		panic(err)
	}
	flags := carverFlagComputed
	if blockTagContains("uncarvable", normalizeResource(canonical.Name)) {
		flags |= carverFlagUncarvable
	}
	if canonical.Name == "minecraft:grass_block" || canonical.Name == "minecraft:mycelium" {
		flags |= carverFlagGrassOrMycelium
	}
	if canonical.Name == "minecraft:dirt" {
		flags |= carverFlagDirt
	}
	if name, _, ok := world.DefaultBlockRegistry.RuntimeIDToState(rid); ok {
		n := normalizeResource(name)
		if n == "water" || n == "lava" {
			flags |= carverFlagFluid
		}
	}
	table[rid].Store(flags)
	return flags
}

func (f *ModernFeatures) initBiomeCarvers() {
	f.biomeByIndexOnce.Do(func() {
		for i := range f.biomeByIndex {
			name := normalizeResource(vanilla.OverworldBiomeName(uint8(i)))
			b, ok := f.biomes[name]
			if !ok {
				continue
			}
			resolved := make([]resolvedCarver, 0, len(b.carvers))
			for _, cname := range b.carvers {
				cfg, ok := f.carvers[cname]
				if !ok {
					panic("overworld: missing configured carver " + cname)
				}
				prob, ok := cfg.config["probability"].(float64)
				if !ok {
					panic("overworld: malformed carver probability " + cname)
				}
				resolved = append(resolved, resolvedCarver{name: cname, typeName: cfg.typeName, config: cfg.config, probability: prob})
			}
			b.resolvedCarvers = resolved
			f.biomeByIndex[i] = b
		}
	})
}

// Carve applies each biome's configured Java carvers from the full 17x17 source-chunk neighborhood.
func (f *ModernFeatures) Carve(pos world.ChunkPos, c *chunk.Chunk, aquifer ModernCarverAquifer) world.GenerationResult {
	if aquifer == nil {
		panic("overworld: nil carver aquifer")
	}
	minY, maxY := int32(c.Range()[0])+1, int32(c.Range()[1])-7
	mask := acquireCarvingMask(minY, maxY)
	defer releaseCarvingMask(mask)
	result := world.GenerationResult{}
	var seedCache [8]struct {
		ready bool
		x, z  int64
	}
	src, hasFastSource := f.source.(modernTerrainSource)
	hasFastSource = hasFastSource && src.generator != nil
	if hasFastSource {
		f.initBiomeCarvers()
	}
	for dx := int32(-8); dx <= 8; dx++ {
		for dz := int32(-8); dz <= 8; dz++ {
			sourceX, sourceZ := int32(pos[0])+dx, int32(pos[1])+dz
			if hasFastSource {
				idx := src.generator.chunkBiomes.originBiome(world.ChunkPos{sourceX, sourceZ}, src.generator.noise)
				biome := &f.biomeByIndex[idx]
				for index := range biome.resolvedCarvers {
					rc := &biome.resolvedCarvers[index]
					f.applyCarverAt(c, pos, sourceX, sourceZ, index, rc.typeName, rc.config, rc.probability, minY, maxY, mask, &seedCache)
				}
				continue
			}
			biomeName := normalizeResource(f.source.NoiseBiomeAt(sourceX<<2, 0, sourceZ<<2))
			biome, ok := f.biomes[biomeName]
			if !ok {
				panic("overworld: noise biome source returned unknown carver biome " + biomeName)
			}
			for index, name := range biome.carvers {
				cfg, ok := f.carvers[name]
				if !ok {
					panic("overworld: missing configured carver " + name)
				}
				probability, ok := cfg.config["probability"].(float64)
				if !ok {
					panic("overworld: malformed carver probability " + name)
				}
				f.applyCarverAt(c, pos, sourceX, sourceZ, index, cfg.typeName, cfg.config, probability, minY, maxY, mask, &seedCache)
			}
		}
	}
	mask.apply(pos, c, aquifer, &result)
	return result
}

func (f *ModernFeatures) applyCarverAt(c *chunk.Chunk, pos world.ChunkPos, sourceX, sourceZ int32, index int, typeName string, config map[string]any, probability float64, minY, maxY int32, mask *carvingMask, seedCache *[8]struct {
	ready bool
	x, z  int64
}) {
	var r vanilla.Random
	seed := f.seed + int64(index)
	if index < len(seedCache) {
		entry := &seedCache[index]
		if !entry.ready {
			tmp := vanilla.NewLegacyRandom(seed)
			entry.x, entry.z = tmp.NextLong(), tmp.NextLong()
			entry.ready = true
		}
		r = vanilla.NewLegacyRandom(int64(sourceX)*entry.x ^ int64(sourceZ)*entry.z ^ seed)
	} else {
		r = largeFeatureRandom(seed, sourceX, sourceZ)
	}
	if float64(r.NextFloat()) > probability {
		return
	}
	switch typeName {
	case "cave":
		f.carveCave(c, pos, config, sourceX, sourceZ, &r, minY, maxY, mask)
	case "canyon":
		f.carveCanyon(c, pos, config, sourceX, sourceZ, &r, minY, maxY, mask)
	default:
		panic("overworld: unsupported configured carver " + typeName)
	}
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

var carvingMaskPool = sync.Pool{New: func() any { return &carvingMask{} }}

func acquireCarvingMask(minY, maxY int32) *carvingMask {
	m := carvingMaskPool.Get().(*carvingMask)
	height := maxY - minY + 1
	words := (int(256*height) + 63) / 64
	if cap(m.bits) < words {
		m.bits = make([]uint64, words)
	} else {
		m.bits = m.bits[:words]
		clear(m.bits)
	}
	m.minY = minY
	m.height = height
	return m
}

func releaseCarvingMask(m *carvingMask) {
	carvingMaskPool.Put(m)
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
	nativeAquifer, useFast := aquifer.(modernCarverFluid)
	useFast = useFast && c.BlockRegistry() == world.DefaultBlockRegistry
	for start := m.nextSetBit(0); start >= 0; {
		end := m.nextClearBit(start) - 1
		startColumn, endColumn := start/int(m.height), end/int(m.height)
		for column := startColumn; column <= endColumn; column++ {
			x, z := int32(column>>4&15), int32(column&15)
			base := column * int(m.height)
			bottom := int32(maxInt(start-base, 0)) + m.minY
			top := int32(minInt(end-base, int(m.height)-1)) + m.minY
			if useFast {
				m.applySpanNative(pos, c, x, z, bottom, top, nativeAquifer, result)
			} else {
				m.applySpan(pos, c, x, z, bottom, top, aquifer, result)
			}
		}
		start = m.nextSetBit(end + 1)
	}
}

func (m *carvingMask) applySpanNative(pos world.ChunkPos, c *chunk.Chunk, x, z, bottomY, topY int32, aquifer modernCarverFluid, result *world.GenerationResult) {
	hasGrass := false
	worldX, worldZ := int32(pos[0])<<4+x, int32(pos[1])<<4+z
	lx, lz := uint8(x), uint8(z)
	airRID := world.DefaultBlockRegistry.AirRuntimeID()
	chunkMinY := int32(c.Range()[0])
	r := c.Range()
	for y := topY; y >= bottomY; y-- {
		blk, liq, stateID := c.Native(lx, int16(y), lz)
		if liq == airRID && stateID == 0 {
			flags := carverFlagsForDefaultRID(blk)
			if flags&carverFlagUncarvable != 0 {
				continue
			}
			if flags&carverFlagGrassOrMycelium != 0 {
				hasGrass = true
			}
		} else {
			currentCanonical, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{Block: blk, Liquid: liq, StateID: stateID})
			if err != nil {
				panic(err)
			}
			if blockTagContains("uncarvable", normalizeResource(currentCanonical.Name)) {
				continue
			}
			if currentCanonical.Name == "minecraft:grass_block" || currentCanonical.Name == "minecraft:mycelium" {
				hasGrass = true
			}
		}
		rid, isFluid, schedule, ok := aquifer.carveNativeSubstance(worldX, y, worldZ, 0)
		if !ok {
			continue
		}
		if liq == airRID && stateID == 0 {
			c.SetBlock(lx, int16(y), lz, 0, rid)
		} else {
			world.SetNativeState(c, lx, int16(y), lz, world.NativeState{Block: rid, Liquid: airRID})
		}
		if schedule && isFluid {
			result.MarkForPostProcessing(cube.Pos{int(worldX), int(y), int(worldZ)}, r)
		}
		if hasGrass && y > chunkMinY {
			belowY := y - 1
			bBlk, bLiq, bStateID := c.Native(lx, int16(belowY), lz)
			isDirt := false
			if bLiq == airRID && bStateID == 0 {
				isDirt = carverFlagsForDefaultRID(bBlk)&carverFlagDirt != 0
			} else {
				belowCanonical, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeState{Block: bBlk, Liquid: bLiq, StateID: bStateID})
				if err != nil {
					panic(err)
				}
				isDirt = belowCanonical.Name == "minecraft:dirt"
			}
			if isDirt {
				if topRID, ok := aquifer.carveNativeTopMaterial(worldX, belowY, worldZ, isFluid); ok {
					if bLiq == airRID && bStateID == 0 {
						c.SetBlock(lx, int16(belowY), lz, 0, topRID)
					} else {
						world.SetNativeState(c, lx, int16(belowY), lz, world.NativeState{Block: topRID, Liquid: airRID})
					}
					if carverFlagsForDefaultRID(topRID)&carverFlagFluid != 0 {
						result.MarkForPostProcessing(cube.Pos{int(worldX), int(belowY), int(worldZ)}, r)
					}
				}
			}
		}
	}
}

func (m *carvingMask) applySpan(pos world.ChunkPos, c *chunk.Chunk, x, z, bottomY, topY int32, aquifer ModernCarverAquifer, result *world.GenerationResult) {
	hasGrass := false
	worldX, worldZ := int32(pos[0])<<4+x, int32(pos[1])<<4+z
	for y := topY; y >= bottomY; y-- {
		current := carverNativeState(c, worldX, y, worldZ)
		currentCanonical, err := world.CanonicalState(c.BlockRegistry(), current)
		if err != nil {
			panic(err)
		}
		if blockTagContains("uncarvable", normalizeResource(currentCanonical.Name)) {
			continue
		}
		if currentCanonical.Name == "minecraft:grass_block" || currentCanonical.Name == "minecraft:mycelium" {
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
			belowCanonical, err := world.CanonicalState(c.BlockRegistry(), below)
			if err != nil {
				panic(err)
			}
			if belowCanonical.Name == "minecraft:dirt" {
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

func largeFeatureRandom(seed int64, chunkX, chunkZ int32) vanilla.Random {
	r := vanilla.NewLegacyRandom(seed)
	x, z := r.NextLong(), r.NextLong()
	return vanilla.NewLegacyRandom(int64(chunkX)*x ^ int64(chunkZ)*z ^ seed)
}

func (f *ModernFeatures) carveCave(c *chunk.Chunk, target world.ChunkPos, config map[string]any, sourceX, sourceZ int32, r *vanilla.Random, minY, maxY int32, mask *carvingMask) {
	count := sampleInt(config["count"], r)
	if count <= 0 {
		return
	}
	yCfg := config["y"].(map[string]any)
	horizCfg := config["horizontal_radius_multiplier"].(map[string]any)
	vertCfg := config["vertical_radius_multiplier"].(map[string]any)
	startVertRaw, hasStartVert := config["start_vertical_radius_multiplier"]
	var startVertCfg map[string]any
	if hasStartVert {
		startVertCfg = startVertRaw.(map[string]any)
	}
	floorCfg := config["floor_level"].(map[string]any)
	roomVertCfg := config["room_vertical_radius_multiplier"].(map[string]any)
	thickCfg := config["thickness"].(map[string]any)
	weirdBias := config["weird_thickness_bias"] == true
	cMinY, cMaxYPlus1 := int32(c.Range()[0]), int32(c.Range()[1])+1
	seaLevel := f.source.SeaLevel()
	for range count {
		x := float64((sourceX << 4) + int32(r.NextInt(16)))
		y := float64(sampleHeight(yCfg, r, cMinY, cMaxYPlus1, seaLevel))
		z := float64((sourceZ << 4) + int32(r.NextInt(16)))
		horizontal := sampleFloat(horizCfg, r)
		vertical := sampleFloat(vertCfg, r)
		startVertical := float32(1)
		if hasStartVert {
			startVertical = sampleFloat(startVertCfg, r)
		}
		floor := float64(sampleFloat(floorCfg, r))
		tunnels := 1
		if r.NextInt(4) == 0 {
			yScale := float64(sampleFloat(roomVertCfg, r))
			thickness := float32(1 + r.NextFloat()*6)
			horizontalRadius := 1.5 + float64(thickness)
			carveCaveEllipsoid(target, x+1, y, z, horizontalRadius, horizontalRadius*yScale, floor, minY, maxY, mask)
			tunnels += int(r.NextInt(4))
		}
		for range tunnels {
			horizontalRotation := r.NextFloat() * float32(2*math.Pi)
			verticalRotation := (r.NextFloat() - 0.5) / 4
			thickness := sampleFloat(thickCfg, r)
			if weirdBias && r.NextInt(10) == 0 {
				thickness *= r.NextFloat()*r.NextFloat()*3 + 1
			}
			maxDistance := int32(112)
			distance := maxDistance - r.NextInt(maxDistance/4)
			tunnelRandom := r.NextLong()
			f.caveTunnel(target, tunnelRandom, x, y, z, float64(horizontal), float64(vertical), float32(thickness), horizontalRotation, verticalRotation, 0, distance, float64(startVertical), floor, minY, maxY, mask)
		}
	}
}

func (f *ModernFeatures) caveTunnel(target world.ChunkPos, seed int64, x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier float64, thickness, horizontalRotation, verticalRotation float32, step, distance int32, yScale, floor float64, minY, maxY int32, mask *carvingMask) {
	r := vanilla.NewLegacyRandom(seed)
	split := r.NextInt(distance/2) + distance/4
	steep := r.NextInt(6) == 0
	var yRota, xRota float32
	for current := step; current < distance; current++ {
		hr := 1.5 + float64(vanilla.Sin(float64(float32(math.Pi)*float32(current)/float32(distance)))*thickness)
		vr := hr * yScale
		cosX := vanilla.Cos(float64(verticalRotation))
		x += float64(vanilla.Cos(float64(horizontalRotation)) * cosX)
		y += float64(vanilla.Sin(float64(verticalRotation)))
		z += float64(vanilla.Sin(float64(horizontalRotation)) * cosX)
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
			f.caveTunnel(target, r.NextLong(), x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier, r.NextFloat()*.5+.5, horizontalRotation-float32(math.Pi/2), verticalRotation/3, current, distance, 1, floor, minY, maxY, mask)
			f.caveTunnel(target, r.NextLong(), x, y, z, horizontalRadiusMultiplier, verticalRadiusMultiplier, r.NextFloat()*.5+.5, horizontalRotation+float32(math.Pi/2), verticalRotation/3, current, distance, 1, floor, minY, maxY, mask)
			return
		}
		if r.NextInt(4) != 0 {
			if !carverCanReach(target, x, z, current, distance, thickness) {
				return
			}
			carveCaveEllipsoid(target, x, y, z, hr*horizontalRadiusMultiplier, vr*verticalRadiusMultiplier, floor, minY, maxY, mask)
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

func carveCaveEllipsoid(target world.ChunkPos, x, y, z, horizontalRadius, verticalRadius, floor float64, minY, maxY int32, mask *carvingMask) {
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
	mHeight, mMinY := int(mask.height), int(mask.minY)
	mBits := mask.bits
	for lx := minX; lx <= maxX; lx++ {
		wx := chunkMinX + int32(lx)
		xd := (float64(wx) + .5 - x) / horizontalRadius
		xd2 := xd * xd
		if xd2 >= 1 {
			continue
		}
		for lz := minZ; lz <= maxZ; lz++ {
			wz := chunkMinZ + int32(lz)
			zd := (float64(wz) + .5 - z) / horizontalRadius
			zd2 := zd * zd
			if xd2+zd2 >= 1 {
				continue
			}
			colBase := (lz+(lx<<4))*mHeight - mMinY
			for wy := maxCarveY; wy > minCarveY; wy-- {
				yd := (float64(wy) - .5 - y) / verticalRadius
				if yd <= floor || xd2+yd*yd+zd2 >= 1 {
					continue
				}
				idx := colBase + int(wy)
				mBits[idx>>6] |= uint64(1) << uint(idx&63)
			}
		}
	}
}

func carveCanyonEllipsoid(target world.ChunkPos, x, y, z, horizontalRadius, verticalRadius float64, cMinY int32, width []float32, minY, maxY int32, mask *carvingMask) {
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
	if maxCarveY > cMinY+int32(len(width)) {
		maxCarveY = cMinY + int32(len(width))
	}
	mHeight, mMinY := int(mask.height), int(mask.minY)
	mBits := mask.bits
	for lx := minX; lx <= maxX; lx++ {
		wx := chunkMinX + int32(lx)
		xd := (float64(wx) + .5 - x) / horizontalRadius
		xd2 := xd * xd
		if xd2 >= 1 {
			continue
		}
		for lz := minZ; lz <= maxZ; lz++ {
			wz := chunkMinZ + int32(lz)
			zd := (float64(wz) + .5 - z) / horizontalRadius
			xz2 := xd2 + zd*zd
			if xz2 >= 1 {
				continue
			}
			colBase := (lz+(lx<<4))*mHeight - mMinY
			for wy := maxCarveY; wy > minCarveY; wy-- {
				idxW := wy - cMinY - 1
				if idxW < 0 {
					break
				}
				yd := (float64(wy) - .5 - y) / verticalRadius
				if xz2*float64(width[idxW])+yd*yd/6 >= 1 {
					continue
				}
				idx := colBase + int(wy)
				mBits[idx>>6] |= uint64(1) << uint(idx&63)
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

func intProviderValue(value any) int32 {
	n, ok := value.(float64)
	if !ok || n < -2147483648 || n > 2147483647 || n != math.Trunc(n) {
		panic(fmt.Sprintf("overworld: malformed IntProvider integer %v", value))
	}
	return int32(n)
}

func intProviderField(provider map[string]any, field string) int32 {
	value, exists := provider[field]
	if !exists {
		panic("overworld: IntProvider has no " + field)
	}
	return intProviderValue(value)
}

// intProviderBounds retains Source minInclusive/maxInclusive, including
// zero-weight entries and clamped providers' intersection with their source.
func intProviderBounds(provider any) (int32, int32) {
	if _, ok := provider.(float64); ok {
		value := intProviderValue(provider)
		return value, value
	}
	p, ok := provider.(map[string]any)
	if !ok {
		panic("overworld: malformed IntProvider")
	}
	typeName, ok := p["type"].(string)
	if !ok {
		panic("overworld: IntProvider has no type")
	}
	switch normalizeResource(typeName) {
	case "constant":
		value := intProviderField(p, "value")
		return value, value
	case "uniform", "biased_to_bottom", "very_biased_to_bottom", "clamped_normal":
		return intProviderField(p, "min_inclusive"), intProviderField(p, "max_inclusive")
	case "trapezoid":
		return intProviderField(p, "min"), intProviderField(p, "max")
	case "clamped":
		minimum, maximum := intProviderBounds(p["source"])
		return max(minimum, intProviderField(p, "min_inclusive")), min(maximum, intProviderField(p, "max_inclusive"))
	case "weighted_list":
		distribution, ok := p["distribution"].([]any)
		if !ok || len(distribution) == 0 {
			panic("overworld: malformed weighted IntProvider")
		}
		minimum, maximum := int32(math.MaxInt32), int32(math.MinInt32)
		for _, raw := range distribution {
			entry, ok := raw.(map[string]any)
			if !ok {
				panic("overworld: malformed weighted IntProvider entry")
			}
			entryMin, entryMax := intProviderBounds(entry["data"])
			minimum, maximum = min(minimum, entryMin), max(maximum, entryMax)
		}
		return minimum, maximum
	default:
		panic("overworld: unsupported IntProvider " + normalizeResource(typeName))
	}
}

func sampleIntOrUniform(provider any, minimum, maximum int, random *vanilla.Random) int32 {
	if provider != nil {
		return sampleInt(provider, random)
	}
	if minimum == maximum {
		return int32(minimum)
	}
	return int32(minimum) + random.NextInt(int32(maximum-minimum+1))
}

func sampleInt(provider any, r *vanilla.Random) int32 {
	if _, ok := provider.(float64); ok {
		return intProviderValue(provider)
	}
	p, ok := provider.(map[string]any)
	if !ok {
		panic("overworld: malformed IntProvider")
	}
	typeName, ok := p["type"].(string)
	if !ok {
		panic("overworld: IntProvider has no type")
	}
	switch normalizeResource(typeName) {
	case "constant":
		return intProviderField(p, "value")
	case "uniform", "biased_to_bottom", "very_biased_to_bottom":
		minimum := intProviderField(p, "min_inclusive")
		maximum := intProviderField(p, "max_inclusive")
		if maximum < minimum {
			panic("overworld: invalid IntProvider bounds")
		}
		width := maximum - minimum
		switch normalizeResource(typeName) {
		case "uniform":
			return minimum + r.NextInt(width+1)
		case "biased_to_bottom":
			return minimum + r.NextInt(r.NextInt(width+1)+1)
		default:
			return minimum + r.NextInt(r.NextInt(r.NextInt(width+1)+1)+1)
		}
	case "trapezoid":
		minimum := intProviderField(p, "min")
		maximum := intProviderField(p, "max")
		plateau := intProviderField(p, "plateau")
		if maximum < minimum || plateau > maximum-minimum {
			panic("overworld: invalid trapezoid IntProvider")
		}
		if plateau == 0 && maximum == -minimum {
			return r.NextInt(maximum+1) - r.NextInt(maximum+1) //nolint:staticcheck // SA4000: two draws of the random source, as Java's TrapezoidInt.sample does.
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
			panic("overworld: malformed weighted IntProvider")
		}
		total := int32(0)
		for _, raw := range distribution {
			entry, ok := raw.(map[string]any)
			if !ok {
				panic("overworld: malformed weighted IntProvider entry")
			}
			weight := intProviderValue(entry["weight"])
			if weight < 0 || total > int32(2147483647)-weight {
				panic("overworld: invalid weighted IntProvider weight")
			}
			total += weight
		}
		if total == 0 {
			panic("overworld: weighted IntProvider has no positive weight")
		}
		choice := r.NextInt(total)
		for _, raw := range distribution {
			entry := raw.(map[string]any)
			weight := intProviderValue(entry["weight"])
			if choice < weight {
				value, exists := entry["data"]
				if !exists {
					panic("overworld: weighted IntProvider entry has no data")
				}
				return sampleInt(value, r)
			}
			choice -= weight
		}
		panic("overworld: weighted IntProvider selection escaped its total")
	case "clamped":
		source, exists := p["source"]
		if !exists {
			panic("overworld: clamped IntProvider has no source")
		}
		minimum := intProviderField(p, "min_inclusive")
		maximum := intProviderField(p, "max_inclusive")
		if maximum < minimum {
			panic("overworld: invalid clamped IntProvider bounds")
		}
		value := sampleInt(source, r)
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
			panic("overworld: malformed clamped_normal IntProvider")
		}
		minimum := intProviderField(p, "min_inclusive")
		maximum := intProviderField(p, "max_inclusive")
		if maximum < minimum {
			panic("overworld: invalid clamped_normal IntProvider bounds")
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
		panic("overworld: unsupported IntProvider " + normalizeResource(typeName))
	}
}

func sampleFloat(provider map[string]any, r *vanilla.Random) float32 {
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
		panic("overworld: malformed FloatProvider")
	}
	switch normalizeResource(typeName) {
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
			panic("overworld: malformed clamped_normal FloatProvider")
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
		panic("overworld: unsupported FloatProvider " + normalizeResource(typeName))
	}
}

func sampleFloatValue(value any, r *vanilla.Random) float32 {
	if n, ok := value.(float64); ok {
		return float32(n)
	}
	provider, ok := value.(map[string]any)
	if !ok {
		panic("overworld: malformed FloatProvider")
	}
	return sampleFloat(provider, r)
}

func (f *ModernFeatures) carveCanyon(c *chunk.Chunk, target world.ChunkPos, config map[string]any, sourceX, sourceZ int32, r *vanilla.Random, minY, maxY int32, mask *carvingMask) {
	shape := config["shape"].(map[string]any)
	x := float64((sourceX << 4) + int32(r.NextInt(16)))
	y := float64(sampleHeight(config["y"].(map[string]any), r, int32(c.Range()[0]), int32(c.Range()[1])+1, f.source.SeaLevel()))
	z := float64((sourceZ << 4) + int32(r.NextInt(16)))
	rotation := r.NextFloat() * float32(2*math.Pi)
	vertical := sampleFloat(config["vertical_rotation"].(map[string]any), r)
	yScale := float64(sampleFloatValue(shape["y_scale"], r))
	thickness := sampleFloat(shape["thickness"].(map[string]any), r)
	distance := int32(112 * sampleFloat(shape["distance_factor"].(map[string]any), r))
	f.canyonTunnel(c, target, r.NextLong(), x, y, z, thickness, rotation, vertical, 0, distance, yScale, shape, minY, maxY, mask)
}

func (f *ModernFeatures) canyonTunnel(c *chunk.Chunk, target world.ChunkPos, seed int64, x, y, z float64, thickness, rotation, vertical float32, step, distance int32, yScale float64, shape map[string]any, minY, maxY int32, mask *carvingMask) {
	if distance <= 0 {
		return
	}
	r := vanilla.NewLegacyRandom(seed)
	cMinY := int32(c.Range()[0])
	height := int(c.Range()[1] - c.Range()[0] + 1)
	var widthBuf [512]float32
	var width []float32
	if height <= len(widthBuf) {
		width = widthBuf[:height]
	} else {
		width = make([]float32, height)
	}
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
	horizFactorCfg := shape["horizontal_radius_factor"].(map[string]any)
	vDefault, _ := shape["vertical_radius_default_factor"].(float64)
	vCenter, _ := shape["vertical_radius_center_factor"].(float64)
	vDef32, vCen32 := float32(vDefault), float32(vCenter)
	var yRota, xRota float32
	for current := step; current < distance; current++ {
		hr := 1.5 + float64(vanilla.Sin(float64(float32(current)*float32(math.Pi)/float32(distance)))*thickness)
		vr := hr * yScale
		hr *= float64(sampleFloat(horizFactorCfg, &r))
		center := float32(1) - float32(math.Abs(float64(float32(.5)-float32(current)/float32(distance))))*2
		factor := vDef32 + vCen32*center
		vr = float64(factor) * vr * float64(float32(.75)+float32(.25)*r.NextFloat())
		cosV := vanilla.Cos(float64(vertical))
		sinV := vanilla.Sin(float64(vertical))
		x += float64(vanilla.Cos(float64(rotation)) * cosV)
		y += float64(sinV)
		z += float64(vanilla.Sin(float64(rotation)) * cosV)
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
			carveCanyonEllipsoid(target, x, y, z, hr, vr, cMinY, width, minY, maxY, mask)
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
