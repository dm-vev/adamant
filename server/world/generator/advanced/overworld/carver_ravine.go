package overworld

import (
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

const ravineRange = 8

func (g *Overworld) carveRavines(chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef) {
	r := mc112.NewRand(0)
	for startX := chunkX - ravineRange; startX <= chunkX+ravineRange; startX++ {
		for startZ := chunkZ - ravineRange; startZ <= chunkZ+ravineRange; startZ++ {
			seed := (int64(startX) * g.mapGenJ) ^ (int64(startZ) * g.mapGenK) ^ g.seed
			r.SetSeed(seed)
			g.recursiveGenerateRavines(r, startX, startZ, chunkX, chunkZ, c, biomes)
		}
	}
}

func (g *Overworld) recursiveGenerateRavines(r *mc112.Rand, startChunkX, startChunkZ, chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef) {
	if r.Intn(50) != 0 {
		return
	}

	x := float64(startChunkX*16 + int(r.Intn(16)))
	y := float64(r.Intn(r.Intn(40)+8) + 20)
	z := float64(startChunkZ*16 + int(r.Intn(16)))

	yaw := r.Float32() * twoPi
	pitch := (r.Float32() - 0.5) * 2 / 8
	width := (r.Float32()*2 + r.Float32()) * 2

	g.carveRavineTunnel(r.Long(), chunkX, chunkZ, c, biomes, x, y, z, width, yaw, pitch, 0, 0, 3.0)
}

func (g *Overworld) carveRavineTunnel(seed int64, chunkX, chunkZ int, c *chunk.Chunk, biomes []*biomeDef, x, y, z float64, width, yaw, pitch float32, startStep, totalSteps int, verticalScale float64) {
	r := mc112.NewRand(seed)

	chunkCenterX := float64(chunkX*16 + 8)
	chunkCenterZ := float64(chunkZ*16 + 8)

	factors := make([]float32, 256)
	f := float32(1.0)
	for i := 0; i < 256; i++ {
		if i == 0 || r.Intn(3) == 0 {
			f = 1.0 + r.Float32()*r.Float32()
		}
		factors[i] = f * f
	}

	var yawChange, pitchChange float32
	if totalSteps <= 0 {
		max := ravineRange*16 - 16
		totalSteps = max - int(r.Intn(int32(max/4)))
	}
	isRoom := false
	if startStep == -1 {
		startStep = totalSteps / 2
		isRoom = true
	}

	for step := startStep; step < totalSteps; step++ {
		t := float32(step) * pi / float32(totalSteps)
		radiusH := 1.5 + float64(mc112.Sin(t)*width)
		radiusV := radiusH * verticalScale

		radiusH *= float64(r.Float32()*0.25 + 0.75)
		radiusV *= float64(r.Float32()*0.25 + 0.75)

		cosPitch := mc112.Cos(pitch)
		sinPitch := mc112.Sin(pitch)

		x += float64(mc112.Cos(yaw) * cosPitch)
		y += float64(sinPitch)
		z += float64(mc112.Sin(yaw) * cosPitch)

		pitch *= 0.7
		pitch += pitchChange * 0.05
		yaw += yawChange * 0.05

		pitchChange *= 0.8
		yawChange *= 0.5
		pitchChange += (r.Float32() - r.Float32()) * r.Float32() * 2.0
		yawChange += (r.Float32() - r.Float32()) * r.Float32() * 4.0

		if isRoom || r.Intn(4) != 0 {
			dx := x - chunkCenterX
			dz := z - chunkCenterZ
			remaining := float64(totalSteps - step)
			maxDist := float64(width) + 2.0 + 16.0
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
				ryMin, ryMax := int(c.Range().Min()), int(c.Range().Max())
				if yStart < ryMin {
					yStart = ryMin
				}
				if yEnd > ryMax {
					yEnd = ryMax
				}
				if yEnd < yStart {
					continue
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

							h := (dxNormSq + dzNormSq) * float64(factors[ly])
							if h+dyNorm*dyNorm/6.0 >= 1.0 {
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
