package overworld

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

const caveRange = 8

const (
	pi     = float32(math.Pi)
	twoPi  = float32(2 * math.Pi)
	halfPi = float32(math.Pi / 2)
)

func (g *Overworld) carveCaves(chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef) {
	r := mc112.NewRand(0)
	for startX := chunkX - caveRange; startX <= chunkX+caveRange; startX++ {
		for startZ := chunkZ - caveRange; startZ <= chunkZ+caveRange; startZ++ {
			seed := (int64(startX) * g.mapGenJ) ^ (int64(startZ) * g.mapGenK) ^ g.seed
			r.SetSeed(seed)
			g.recursiveGenerateCaves(r, startX, startZ, chunkX, chunkZ, c, biomes)
		}
	}
}

func (g *Overworld) recursiveGenerateCaves(r *mc112.Rand, startChunkX, startChunkZ, chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef) {
	count := int(r.Intn(r.Intn(r.Intn(15)+1) + 1))
	if r.Intn(7) != 0 {
		count = 0
	}

	for i := 0; i < count; i++ {
		x := float64(startChunkX*16 + int(r.Intn(16)))
		y := float64(r.Intn(r.Intn(120) + 8))
		z := float64(startChunkZ*16 + int(r.Intn(16)))

		branches := 1
		if r.Intn(4) == 0 {
			g.carveCaveRoom(r.Long(), chunkX, chunkZ, c, biomes, x, y, z)
			branches += int(r.Intn(4))
		}

		for j := 0; j < branches; j++ {
			yaw := r.Float32() * twoPi
			pitch := (r.Float32() - 0.5) * 2 / 8
			radius := r.Float32()*2 + r.Float32()
			if r.Intn(10) == 0 {
				radius *= r.Float32()*r.Float32()*3 + 1
			}
			g.carveCaveTunnel(r.Long(), chunkX, chunkZ, c, biomes, x, y, z, radius, yaw, pitch, 0, 0, 1.0)
		}
	}
}

func (g *Overworld) carveCaveRoom(seed int64, chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef, x, y, z float64) {
	r := mc112.NewRand(seed)
	radius := 1.0 + float64(r.Float32())*6.0
	g.carveCaveTunnel(seed, chunkX, chunkZ, c, biomes, x, y, z, float32(radius), 0, 0, -1, -1, 0.5)
}

func (g *Overworld) carveCaveTunnel(seed int64, chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef, x, y, z float64, radius, yaw, pitch float32, startStep, totalSteps int, verticalScale float64) {
	r := mc112.NewRand(seed)

	chunkCenterX := float64(chunkX*16 + 8)
	chunkCenterZ := float64(chunkZ*16 + 8)

	var yawChange, pitchChange float32
	if totalSteps <= 0 {
		max := caveRange*16 - 16
		totalSteps = max - int(r.Intn(int32(max/4)))
	}
	isRoom := false
	if startStep == -1 {
		startStep = totalSteps / 2
		isRoom = true
	}

	splitStep := int(r.Intn(int32(totalSteps/2))) + totalSteps/4
	steep := r.Intn(6) == 0

	for step := startStep; step < totalSteps; step++ {
		t := float32(step) * pi / float32(totalSteps)
		radiusH := 1.5 + float64(mc112.Sin(t)*radius)
		radiusV := radiusH * verticalScale

		cosPitch := mc112.Cos(pitch)
		sinPitch := mc112.Sin(pitch)

		x += float64(mc112.Cos(yaw) * cosPitch)
		y += float64(sinPitch)
		z += float64(mc112.Sin(yaw) * cosPitch)

		if steep {
			pitch *= 0.92
		} else {
			pitch *= 0.7
		}
		pitch += pitchChange * 0.1
		yaw += yawChange * 0.1
		pitchChange *= 0.9
		yawChange *= 0.75
		pitchChange += (r.Float32() - r.Float32()) * r.Float32() * 2.0
		yawChange += (r.Float32() - r.Float32()) * r.Float32() * 4.0

		if !isRoom && step == splitStep && radius > 1.0 && totalSteps > 0 {
			g.carveCaveTunnel(r.Long(), chunkX, chunkZ, c, biomes, x, y, z, r.Float32()*0.5+0.5, yaw-halfPi, pitch/3, step, totalSteps, 1.0)
			g.carveCaveTunnel(r.Long(), chunkX, chunkZ, c, biomes, x, y, z, r.Float32()*0.5+0.5, yaw+halfPi, pitch/3, step, totalSteps, 1.0)
			return
		}

		if isRoom || r.Intn(4) != 0 {
			dx := x - chunkCenterX
			dz := z - chunkCenterZ
			remaining := float64(totalSteps - step)
			maxDist := float64(radius) + 2.0 + 16.0
			if dx*dx+dz*dz-remaining*remaining > maxDist*maxDist {
				return
			}

			if x >= chunkCenterX-16.0-radiusH*2.0 && x <= chunkCenterX+16.0+radiusH*2.0 &&
				z >= chunkCenterZ-16.0-radiusH*2.0 && z <= chunkCenterZ+16.0+radiusH*2.0 {

				xStart := mc112.Floor(x-radiusH) - chunkX*16 - 1
				xEnd := mc112.Floor(x+radiusH) - chunkX*16 + 1
				yStart := mc112.Floor(y-radiusV) - 1
				yEnd := mc112.Floor(y+radiusV) + 1
				zStart := mc112.Floor(z-radiusH) - chunkZ*16 - 1
				zEnd := mc112.Floor(z+radiusH) - chunkZ*16 + 1

				if xStart < 0 {
					xStart = 0
				}
				if xEnd > 16 {
					xEnd = 16
				}
				if zStart < 0 {
					zStart = 0
				}
				if zEnd > 16 {
					zEnd = 16
				}

				if yStart < 1 {
					yStart = 1
				}
				if yEnd > 248 {
					yEnd = 248
				}

				if g.caveHitsWater(c, xStart, xEnd, yStart, yEnd, zStart, zEnd) {
					continue
				}

				for lx := xStart; lx < xEnd; lx++ {
					worldX := float64(chunkX*16+lx) + 0.5
					dxNorm := (worldX - x) / radiusH
					dxNormSq := dxNorm * dxNorm

					for lz := zStart; lz < zEnd; lz++ {
						worldZ := float64(chunkZ*16+lz) + 0.5
						dzNorm := (worldZ - z) / radiusH
						dzNormSq := dzNorm * dzNorm

						if dxNormSq+dzNormSq >= 1.0 {
							continue
						}

						foundTop := false
						for ly := yEnd; ly >= yStart; ly-- {
							worldY := float64(ly) + 0.5
							dyNorm := (worldY - y) / radiusV
							if dyNorm <= -0.7 {
								continue
							}
							if dxNormSq+dyNorm*dyNorm+dzNormSq >= 1.0 {
								continue
							}

							rid := c.Block(uint8(lx), int16(ly), uint8(lz), 0)
							if !foundTop && rid == biomes[lz+lx*16].topRID {
								foundTop = true
							}
							g.digCarveBlock(c, biomes, lx, ly, lz, foundTop)
						}
					}
				}

				if isRoom {
					break
				}
			}
		}
	}
}

func (g *Overworld) caveHitsWater(c *chunk.Chunk, xStart, xEnd, yStart, yEnd, zStart, zEnd int) bool {
	for x := xStart; x < xEnd; x++ {
		for z := zStart; z < zEnd; z++ {
			for y := yEnd; y >= yStart; y-- {
				rid := c.Block(uint8(x), int16(y), uint8(z), 0)
				if rid == g.waterRID {
					return true
				}
				if y != yStart && x != xStart && x != xEnd-1 && z != zStart && z != zEnd-1 {
					y = yStart
				}
			}
		}
	}
	return false
}

func (g *Overworld) digCarveBlock(c *chunk.Chunk, biomes []*biomeDef, x, y, z int, foundTop bool) {
	yMin, yMax := int(c.Range().Min()), int(c.Range().Max())
	if y < yMin || y > yMax || y < 0 || y > 255 {
		return
	}

	rid := c.Block(uint8(x), int16(y), uint8(z), 0)
	if _, ok := g.carvable[rid]; !ok {
		return
	}

	if y < 11 {
		c.SetBlock(uint8(x), int16(y), uint8(z), 0, g.lavaRID)
		return
	}

	c.SetBlock(uint8(x), int16(y), uint8(z), 0, g.airRID)

	aboveY := y + 1
	if aboveY <= 255 && aboveY <= yMax {
		above := c.Block(uint8(x), int16(aboveY), uint8(z), 0)
		switch above {
		case g.sandRID:
			c.SetBlock(uint8(x), int16(aboveY), uint8(z), 0, g.sandstoneRID)
		case g.redSandRID:
			c.SetBlock(uint8(x), int16(aboveY), uint8(z), 0, g.redSandstoneRID)
		}
	}

	if !foundTop || y-1 < yMin {
		return
	}
	belowY := y - 1
	below := c.Block(uint8(x), int16(belowY), uint8(z), 0)
	if below != g.dirtRID {
		return
	}
	b := biomes[z+x*16]
	c.SetBlock(uint8(x), int16(belowY), uint8(z), 0, b.topRID)
}
