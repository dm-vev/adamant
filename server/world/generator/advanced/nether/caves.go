package nether

import (
	"math"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const netherCaveMaskWords = 128 * 16 * 16 / 64

var netherCaveUncarvableBlocks = func() map[string]struct{} {
	members, err := gamedata.BlockTagMembers("minecraft:uncarvable")
	if err != nil {
		panic(err)
	}
	blocks := make(map[string]struct{}, len(members))
	for _, name := range members {
		blocks[name] = struct{}{}
	}
	return blocks
}()

// netherCaves is the configured minecraft:nether_cave CaveWorldCarver.
// Its only mutable state is the per-invocation carving mask.
type netherCaves struct {
	seed int64
	g    *Nether
}

func newNetherCaves(seed int64, g *Nether) *netherCaves {
	return &netherCaves{seed: seed, g: g}
}

func (g *Nether) carveCaves(chunkX, chunkZ int, c *chunk.Chunk) {
	g.caves.generate(chunkX, chunkZ, c)
}

func (c *netherCaves) generate(chunkX, chunkZ int, target *chunk.Chunk) {
	var mask [netherCaveMaskWords]uint64
	// NoiseBasedChunkGenerator visits the full 17x17 source window, X then Z.
	for dx := -8; dx <= 8; dx++ {
		for dz := -8; dz <= 8; dz++ {
			sourceX, sourceZ := chunkX+dx, chunkZ+dz
			random := largeFeatureRandom(c.seed, sourceX, sourceZ)
			if random.NextFloat() > 0.2 {
				continue
			}
			c.carveSource(chunkX, chunkZ, sourceX, sourceZ, target, &random, &mask)
		}
	}
}

func largeFeatureRandom(seed int64, chunkX, chunkZ int) vanilla.Random {
	random := vanilla.NewLegacyRandom(seed)
	xScale, zScale := random.NextLong(), random.NextLong()
	return vanilla.NewLegacyRandom(int64(chunkX)*xScale ^ int64(chunkZ)*zScale ^ seed)
}

func (c *netherCaves) carveSource(targetX, targetZ, sourceX, sourceZ int, target *chunk.Chunk, random *vanilla.Random, mask *[netherCaveMaskWords]uint64) {
	// VeryBiasedToBottomInt.of(0, 9): three nested inclusive draws.
	count := random.NextInt(random.NextInt(random.NextInt(10)+1) + 1)
	for range count {
		x := float64(sourceX*16 + int(random.NextInt(16)))
		y := float64(random.NextInt(127))
		z := float64(sourceZ*16 + int(random.NextInt(16)))
		horizontalMultiplier := float64(1)
		verticalMultiplier := float64(1)
		startVerticalMultiplier := float64(5)
		floorLevel := float64(float32(-0.7))
		tunnels := 1
		if random.NextInt(4) == 0 {
			roomVerticalMultiplier := float64(0.5)
			thickness := float32(1) + random.NextFloat()*float32(6)
			horizontalRadius := 1.5 + float64(vanilla.Sin(float64(float32(math.Pi/2)))*thickness)
			c.carveEllipsoid(targetX, targetZ, target, x+1, y, z, horizontalRadius, horizontalRadius*roomVerticalMultiplier, floorLevel, mask)
			tunnels += int(random.NextInt(4))
		}
		for range tunnels {
			horizontal := random.NextFloat() * float32(2*math.Pi)
			vertical := (random.NextFloat() - float32(0.5)) / float32(4)
			thickness := sampleNetherCaveThickness(random)
			distance := 112 - int(random.NextInt(28))
			tunnelSeed := random.NextLong()
			c.carveTunnelSeed(targetX, targetZ, target, tunnelSeed, x, y, z, thickness, horizontal, vertical, 0, distance, startVerticalMultiplier, horizontalMultiplier, verticalMultiplier, floorLevel, mask)
		}
	}
}

// TrapezoidFloat.of(0, 6, 2), with Java float staging.
func sampleNetherCaveThickness(random *vanilla.Random) float32 {
	a, b := random.NextFloat(), random.NextFloat()
	rangeSize := float32(6)
	plateauStart := (rangeSize - float32(2)) / float32(2)
	plateauEnd := rangeSize - plateauStart
	return a*plateauEnd + b*plateauStart
}

func (c *netherCaves) carveTunnelSeed(targetX, targetZ int, target *chunk.Chunk, tunnelSeed int64, x, y, z float64, thickness, horizontal, vertical float32, step, distance int, startVerticalMultiplier, horizontalMultiplier, verticalMultiplier, floorLevel float64, mask *[netherCaveMaskWords]uint64) {
	random := vanilla.NewLegacyRandom(tunnelSeed)
	split := int(random.NextInt(int32(distance/2))) + distance/4
	steep := random.NextInt(6) == 0
	var yRotation, xRotation float32
	for current := step; current < distance; current++ {
		sinAngle := float32(math.Pi) * float32(current) / float32(distance)
		horizontalRadius := 1.5 + float64(vanilla.Sin(float64(sinAngle))*thickness)
		verticalRadius := horizontalRadius * startVerticalMultiplier
		cosVertical := vanilla.Cos(float64(vertical))
		x += float64(vanilla.Cos(float64(horizontal)) * cosVertical)
		y += float64(vanilla.Sin(float64(vertical)))
		z += float64(vanilla.Sin(float64(horizontal)) * cosVertical)
		if steep {
			vertical *= float32(0.92)
		} else {
			vertical *= float32(0.7)
		}
		vertical += xRotation * float32(0.1)
		horizontal += yRotation * float32(0.1)
		xRotation *= float32(0.9)
		yRotation *= float32(0.75)
		xRotation += (random.NextFloat() - random.NextFloat()) * random.NextFloat() * float32(2)
		yRotation += (random.NextFloat() - random.NextFloat()) * random.NextFloat() * float32(4)
		if current == split && thickness > 1 {
			branchSeed := random.NextLong()
			branchThickness := random.NextFloat()*float32(0.5) + float32(0.5)
			c.carveTunnelSeed(targetX, targetZ, target, branchSeed, x, y, z, branchThickness, horizontal-float32(math.Pi/2), vertical/float32(3), current, distance, 1, horizontalMultiplier, verticalMultiplier, floorLevel, mask)
			branchSeed = random.NextLong()
			branchThickness = random.NextFloat()*float32(0.5) + float32(0.5)
			c.carveTunnelSeed(targetX, targetZ, target, branchSeed, x, y, z, branchThickness, horizontal+float32(math.Pi/2), vertical/float32(3), current, distance, 1, horizontalMultiplier, verticalMultiplier, floorLevel, mask)
			return
		}
		if random.NextInt(4) != 0 {
			remaining := float64(distance - current)
			radius := float64(thickness) + 18
			midX, midZ := float64(targetX*16+8), float64(targetZ*16+8)
			dx, dz := x-midX, z-midZ
			if dx*dx+dz*dz-remaining*remaining > radius*radius {
				return
			}
			c.carveEllipsoid(targetX, targetZ, target, x, y, z, horizontalRadius*horizontalMultiplier, verticalRadius*verticalMultiplier, floorLevel, mask)
		}
	}
}

func (c *netherCaves) carveEllipsoid(chunkX, chunkZ int, target *chunk.Chunk, x, y, z, horizontalRadius, verticalRadius, floorLevel float64, mask *[netherCaveMaskWords]uint64) {
	centerX, centerZ := float64(chunkX*16+8), float64(chunkZ*16+8)
	if math.Abs(x-centerX) > 16+horizontalRadius*2 || math.Abs(z-centerZ) > 16+horizontalRadius*2 {
		return
	}
	minX := maxInt(int(math.Floor(x-horizontalRadius))-chunkX*16-1, 0)
	maxX := minInt(int(math.Floor(x+horizontalRadius))-chunkX*16, 15)
	minZ := maxInt(int(math.Floor(z-horizontalRadius))-chunkZ*16-1, 0)
	maxZ := minInt(int(math.Floor(z+horizontalRadius))-chunkZ*16, 15)
	minY := maxInt(int(math.Floor(y-verticalRadius))-1, 1)
	maxY := minInt(int(math.Floor(y+verticalRadius))+1, 120)
	for localX := minX; localX <= maxX; localX++ {
		worldX := float64(chunkX*16 + localX)
		xd := (worldX + 0.5 - x) / horizontalRadius
		for localZ := minZ; localZ <= maxZ; localZ++ {
			worldZ := float64(chunkZ*16 + localZ)
			zd := (worldZ + 0.5 - z) / horizontalRadius
			if xd*xd+zd*zd >= 1 {
				continue
			}
			for worldY := maxY; worldY > minY; worldY-- {
				yd := (float64(worldY) - 0.5 - y) / verticalRadius
				if yd <= floorLevel || xd*xd+yd*yd+zd*zd >= 1 {
					continue
				}
				maskIndex := (worldY*16+localZ)*16 + localX
				word, bit := maskIndex>>6, uint(maskIndex&63)
				if mask[word]&(uint64(1)<<bit) != 0 {
					continue
				}
				mask[word] |= uint64(1) << bit
				rid := target.Block(uint8(localX), int16(worldY), uint8(localZ), 0)
				name, _, ok := target.BlockRegistry().RuntimeIDToState(rid)
				if !ok {
					panic("nether: cave carver encountered unknown block runtime ID")
				}
				if _, uncarvable := netherCaveUncarvableBlocks[name]; uncarvable {
					continue
				}
				carved := c.g.airRID
				if worldY < netherSeaLevel {
					carved = c.g.lavaStillRID
				}
				target.SetBlock(uint8(localX), int16(worldY), uint8(localZ), 0, carved)
			}
		}
	}
}
