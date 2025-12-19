package nether

import (
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

type netherCaves struct {
	seed int64
	rngJ int64
	rngK int64
	g    *Nether
}

func newNetherCaves(seed int64, g *Nether) *netherCaves {
	r := mc112.NewRand(seed)
	return &netherCaves{
		seed: seed,
		rngJ: r.Long(),
		rngK: r.Long(),
		g:    g,
	}
}

func (g *Nether) carveCaves(chunkX, chunkZ int, c *chunk.Chunk) {
	g.caves.generate(chunkX, chunkZ, c)
}

func (c *netherCaves) generate(chunkX, chunkZ int, ch *chunk.Chunk) {
	const scanRange = 8
	r := mc112.NewRand(c.seed)

	for x := chunkX - scanRange; x <= chunkX+scanRange; x++ {
		for z := chunkZ - scanRange; z <= chunkZ+scanRange; z++ {
			r.SetSeed(int64(x)*c.rngJ ^ int64(z)*c.rngK ^ c.seed)
			c.recursiveGenerate(x, z, chunkX, chunkZ, ch, r)
		}
	}
}

func (c *netherCaves) recursiveGenerate(chunkX, chunkZ, origX, origZ int, ch *chunk.Chunk, r *mc112.Rand) {
	i := int(r.Intn(int32(r.Intn(int32(r.Intn(10)+1))+1) + 1))
	if r.Intn(5) != 0 {
		i = 0
	}

	for j := 0; j < i; j++ {
		d0 := float64(chunkX*16 + int(r.Intn(16)))
		d1 := float64(r.Intn(128))
		d2 := float64(chunkZ*16 + int(r.Intn(16)))
		k := 1

		if r.Intn(4) == 0 {
			c.addRoom(r.Long(), origX, origZ, ch, d0, d1, d2, r)
			k += int(r.Intn(4))
		}

		for l := 0; l < k; l++ {
			f := r.Float32() * float32(math.Pi*2.0)
			f1 := (r.Float32() - 0.5) * 2.0 / 8.0
			f2 := r.Float32()*2.0 + r.Float32()
			c.addTunnel(r.Long(), origX, origZ, ch, d0, d1, d2, f2*2.0, f, f1, 0, 0, 0.5)
		}
	}
}

func (c *netherCaves) addRoom(seed int64, origX, origZ int, ch *chunk.Chunk, x, y, z float64, r *mc112.Rand) {
	c.addTunnel(seed, origX, origZ, ch, x, y, z, 1.0+r.Float32()*6.0, 0.0, 0.0, -1, -1, 0.5)
}

func (c *netherCaves) addTunnel(seed int64, origX, origZ int, ch *chunk.Chunk, x, y, z float64, radius, yaw, pitch float32, step, maxStep int, yScale float64) {
	d0 := float64(origX*16 + 8)
	d1 := float64(origZ*16 + 8)
	f := float32(0.0)
	f1 := float32(0.0)
	random := mc112.NewRand(seed)

	if maxStep <= 0 {
		i := 8*16 - 16
		maxStep = i - int(random.Intn(int32(i/4)))
	}

	flag1 := false
	if step == -1 {
		step = maxStep / 2
		flag1 = true
	}

	j := int(random.Intn(int32(maxStep/2)) + int32(maxStep/4))
	flag := random.Intn(6) == 0

	for ; step < maxStep; step++ {
		d2 := float64(1.5 + float64(mc112.Sin(float32(step)*math.Pi/float32(maxStep)))*float64(radius))
		d3 := d2 * yScale
		f2 := mc112.Cos(pitch)
		f3 := mc112.Sin(pitch)
		x += float64(mc112.Cos(yaw) * f2)
		y += float64(f3)
		z += float64(mc112.Sin(yaw) * f2)

		if flag {
			pitch *= 0.92
		} else {
			pitch *= 0.7
		}

		pitch += f1 * 0.1
		yaw += f * 0.1
		f1 *= 0.9
		f *= 0.75
		f1 += (random.Float32()-random.Float32())*random.Float32()*2.0
		f += (random.Float32()-random.Float32())*random.Float32()*4.0

		if !flag1 && step == j && radius > 1.0 {
			c.addTunnel(random.Long(), origX, origZ, ch, x, y, z, random.Float32()*0.5+0.5, yaw-float32(math.Pi/2.0), pitch/3.0, step, maxStep, 1.0)
			c.addTunnel(random.Long(), origX, origZ, ch, x, y, z, random.Float32()*0.5+0.5, yaw+float32(math.Pi/2.0), pitch/3.0, step, maxStep, 1.0)
			return
		}

		if !flag1 && random.Intn(4) == 0 {
			continue
		}

		d4 := x - d0
		d5 := z - d1
		d6 := float64(maxStep - step)
		d7 := float64(radius) + 2.0 + 16.0
		if d4*d4+d5*d5-d6*d6 > d7*d7 {
			return
		}

		if x >= d0-16.0-d2*2.0 && z >= d1-16.0-d2*2.0 && x <= d0+16.0+d2*2.0 && z <= d1+16.0+d2*2.0 {
			j2 := mc112.Floor(x-d2) - origX*16 - 1
			k := mc112.Floor(x+d2) - origX*16 + 1
			k2 := mc112.Floor(y-d3) - 1
			l := mc112.Floor(y+d3) + 1
			l2 := mc112.Floor(z-d2) - origZ*16 - 1
			i1 := mc112.Floor(z+d2) - origZ*16 + 1

			if j2 < 0 {
				j2 = 0
			}
			if k > 16 {
				k = 16
			}
			if k2 < 1 {
				k2 = 1
			}
			if l > 120 {
				l = 120
			}
			if l2 < 0 {
				l2 = 0
			}
			if i1 > 16 {
				i1 = 16
			}

			flag2 := false
			for j1 := j2; !flag2 && j1 < k; j1++ {
				for k1 := l2; !flag2 && k1 < i1; k1++ {
					for l1 := l + 1; !flag2 && l1 >= k2-1; l1-- {
						if l1 >= 0 && l1 < 128 {
							rid := ch.Block(uint8(j1), int16(l1), uint8(k1), 0)
							if rid == c.g.lavaFlowRID || rid == c.g.lavaStillRID {
								flag2 = true
							}
							if l1 != k2-1 && j1 != j2 && j1 != k-1 && k1 != l2 && k1 != i1-1 {
								l1 = k2
							}
						}
					}
				}
			}

			if !flag2 {
				for i3 := j2; i3 < k; i3++ {
					d10 := (float64(i3+origX*16) + 0.5 - x) / d2
					for j3 := l2; j3 < i1; j3++ {
						d8 := (float64(j3+origZ*16) + 0.5 - z) / d2
						for i2 := l; i2 > k2; i2-- {
							d9 := (float64(i2-1) + 0.5 - y) / d3
							if d9 > -0.7 && d10*d10+d9*d9+d8*d8 < 1.0 {
								rid := ch.Block(uint8(i3), int16(i2), uint8(j3), 0)
								if rid == c.g.netherrackRID || rid == c.g.dirtRID || rid == c.g.grassRID {
									ch.SetBlock(uint8(i3), int16(i2), uint8(j3), 0, c.g.airRID)
								}
							}
						}
					}
				}
				if flag1 {
					break
				}
			}
		}
	}
}
