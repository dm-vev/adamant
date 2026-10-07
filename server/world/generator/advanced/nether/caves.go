package nether

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/internal/java26"
)

// netherCaves is the configured minecraft:nether_cave CaveWorldCarver. It is
// immutable after construction and derives each neighbouring source chunk's
// random stream directly from the world seed.
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
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			sourceX, sourceZ := chunkX+dx, chunkZ+dz
			random := largeFeatureRandom(c.seed, sourceX, sourceZ)
			if random.NextFloat() > 0.2 {
				continue
			}
			c.carveSource(chunkX, chunkZ, sourceX, sourceZ, target, &random)
		}
	}
}

func largeFeatureRandom(seed int64, chunkX, chunkZ int) java26.Random {
	random := java26.NewLegacyRandom(seed)
	xScale, zScale := random.NextLong(), random.NextLong()
	return java26.NewLegacyRandom(int64(chunkX)*xScale ^ int64(chunkZ)*zScale ^ seed)
}

func (c *netherCaves) carveSource(targetX, targetZ, sourceX, sourceZ int, target *chunk.Chunk, random *java26.Random) {
	count := int(random.NextInt(int32(random.NextInt(int32(random.NextInt(10)+1))+1)))
	for range count {
		x := float64(sourceX*16 + int(random.NextInt(16)))
		y := float64(random.NextInt(127))
		z := float64(sourceZ*16 + int(random.NextInt(16)))
		tunnels := 1
		if random.NextInt(4) == 0 {
			roomRadius := 1 + float64(random.NextFloat())*6
			horizontalRadius := 1.5 + roomRadius
			c.carveEllipsoid(targetX, targetZ, target, x+1, y, z, horizontalRadius, horizontalRadius*0.5, -0.7)
			tunnels += int(random.NextInt(4))
		}
		for range tunnels {
			horizontal := random.NextFloat() * float32(2*math.Pi)
			vertical := (random.NextFloat() - 0.5) / 4
			thickness := random.NextFloat()*4 + random.NextFloat()*2
			distance := 112 - int(random.NextInt(28))
			tunnelSeed := random.NextLong()
			c.carveTunnelSeed(targetX, targetZ, target, tunnelSeed, x, y, z, float64(thickness), horizontal, vertical, 0, distance, 5, 1, 1, -0.7)
		}
	}
}

func (c *netherCaves) carveTunnelSeed(targetX, targetZ int, target *chunk.Chunk, tunnelSeed int64, x, y, z, thickness float64, horizontal, vertical float32, step, distance int, startVerticalMultiplier, horizontalMultiplier, verticalMultiplier, floorLevel float64) {
	random := java26.NewLegacyRandom(tunnelSeed)
	split := int(random.NextInt(int32(distance/2))) + distance/4
	steep := random.NextInt(6) == 0
	var yRotation, xRotation float32
	for current := step; current < distance; current++ {
		horizontalRadius := 1.5 + float64(java26.Sin(float64(float32(math.Pi)*float32(current)/float32(distance))) * float32(thickness))
		verticalRadius := horizontalRadius * startVerticalMultiplier
		cosVertical := java26.Cos(float64(vertical))
		x += float64(java26.Cos(float64(horizontal))) * float64(cosVertical)
		y += float64(java26.Sin(float64(vertical)))
		z += float64(java26.Sin(float64(horizontal))) * float64(cosVertical)
		if steep {
			vertical *= 0.92
		} else {
			vertical *= 0.7
		}
		vertical += xRotation * 0.1
		horizontal += yRotation * 0.1
		xRotation *= 0.9
		yRotation *= 0.75
		xRotation += (random.NextFloat() - random.NextFloat()) * random.NextFloat() * 2
		yRotation += (random.NextFloat() - random.NextFloat()) * random.NextFloat() * 4
		if current == split && thickness > 1 {
			branchThickness := float64(random.NextFloat())*0.5 + 0.5
			branchSeed := random.NextLong()
			c.carveTunnelSeed(targetX, targetZ, target, branchSeed, x, y, z, float64(branchThickness), horizontal-float32(math.Pi/2), vertical/3, current, distance, 1, horizontalMultiplier, verticalMultiplier, floorLevel)
			branchThickness = float64(random.NextFloat())*0.5 + 0.5
			branchSeed = random.NextLong()
			c.carveTunnelSeed(targetX, targetZ, target, branchSeed, x, y, z, branchThickness, horizontal+float32(math.Pi/2), vertical/3, current, distance, 1, horizontalMultiplier, verticalMultiplier, floorLevel)
			return
		}
		if random.NextInt(4) != 0 {
			remaining := float64(distance - current)
			radius := thickness + 18
			midX, midZ := float64(targetX*16+8), float64(targetZ*16+8)
			dx, dz := x-midX, z-midZ
			if dx*dx+dz*dz-remaining*remaining > radius*radius {
				return
			}
			c.carveEllipsoid(targetX, targetZ, target, x, y, z, horizontalRadius*horizontalMultiplier, verticalRadius*verticalMultiplier, floorLevel)
		}
	}
}

func (c *netherCaves) carveEllipsoid(chunkX, chunkZ int, target *chunk.Chunk, x, y, z, horizontalRadius, verticalRadius, floorLevel float64) {
	centerX, centerZ := float64(chunkX*16+8), float64(chunkZ*16+8)
	if math.Abs(x-centerX) > 16+horizontalRadius*2 || math.Abs(z-centerZ) > 16+horizontalRadius*2 {
		return
	}
	minX := maxInt(int(math.Floor(x-horizontalRadius))-chunkX*16-1, 0)
	maxX := minInt(int(math.Floor(x+horizontalRadius))-chunkX*16, 15)
	minZ := maxInt(int(math.Floor(z-horizontalRadius))-chunkZ*16-1, 0)
	maxZ := minInt(int(math.Floor(z+horizontalRadius))-chunkZ*16, 15)
	minY := maxInt(int(math.Floor(y-verticalRadius))-1, 0)
	maxY := minInt(int(math.Floor(y+verticalRadius))+1, 127)
	for localX := minX; localX <= maxX; localX++ {
		worldX := float64(chunkX*16+localX)
		xd := (worldX + 0.5 - x) / horizontalRadius
		for localZ := minZ; localZ <= maxZ; localZ++ {
			worldZ := float64(chunkZ*16+localZ)
			zd := (worldZ + 0.5 - z) / horizontalRadius
			if xd*xd+zd*zd >= 1 {
				continue
			}
			for worldY := maxY; worldY > minY; worldY-- {
				yd := (float64(worldY) - 0.5 - y) / verticalRadius
				if yd <= floorLevel || xd*xd+yd*yd+zd*zd >= 1 {
					continue
				}
				if target.Block(uint8(localX), int16(worldY), uint8(localZ), 0) == c.g.netherrackRID {
					carved := c.g.airRID
					if worldY < netherSeaLevel {
						carved = c.g.lavaStillRID
					}
					target.SetBlock(uint8(localX), int16(worldY), uint8(localZ), 0, carved)
				}
			}
		}
	}
}
