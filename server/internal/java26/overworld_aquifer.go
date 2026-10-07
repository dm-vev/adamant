package java26

import "math"

// AquiferMaterial distinguishes a null material (solid default terrain) from air.
type AquiferMaterial uint8

const (
	AquiferSolid AquiferMaterial = iota
	AquiferAir
	AquiferWater
	AquiferLava
)

const aquiferDryLevel int32 = -32512 // DimensionType.WAY_BELOW_MIN_Y.

type aquiferStatus struct {
	level    int32
	material AquiferMaterial
}

func (s aquiferStatus) at(y int32) AquiferMaterial {
	if y < s.level {
		return s.material
	}
	return AquiferAir
}

type aquiferCenter struct {
	x, y, z                    int32
	status                     aquiferStatus
	locationReady, statusReady bool
}

// OverworldAquifer ports Java's four-nearest-centre aquifer. All mutable caches
// are worker-local; a prepared instance accepts positions in its 16x384x16 chunk.
type OverworldAquifer struct {
	noise                            *OverworldNoise
	random                           PositionalFactory
	baseX, baseZ, minGridX, minGridZ int32
	skipAbove                        int32
	centers                          [315]aquiferCenter
	surface                          [27][19]int32
	surfaceReady                     [27][19]bool
}

func (a *OverworldAquifer) Prepare(noise *OverworldNoise, baseX, baseZ int32) {
	a.noise, a.baseX, a.baseZ = noise, baseX, baseZ
	random := noise.Random.FromHashOf("minecraft:aquifer")
	a.random = random.ForkPositional()
	a.minGridX, a.minGridZ = (baseX-5)>>4, (baseZ-5)>>4
	clear(a.centers[:])
	clear(a.surfaceReady[:])
	maximum := int32(math.MinInt32)
	for x := range 11 {
		for z := range 11 {
			maximum = max(maximum, a.surfaceLevel(baseX-16+int32(x)*4, baseZ-16+int32(z)*4))
		}
	}
	a.skipAbove = (floorDiv32(maximum+20, 12)+1)*12 + 10
}

func globalAquiferStatus(y int32) aquiferStatus {
	if y < -54 {
		return aquiferStatus{-54, AquiferLava}
	}
	return aquiferStatus{63, AquiferWater}
}

// Substance returns the base material and Java's shouldScheduleFluidUpdate.
func (a *OverworldAquifer) Substance(x, y, z int32, density float64) (AquiferMaterial, bool) {
	if density > 0 {
		return AquiferSolid, false
	}
	global := globalAquiferStatus(y)
	if y > a.skipAbove {
		return global.at(y), false
	}
	if global.at(y) == AquiferLava {
		return AquiferLava, false
	}
	anchorX, anchorY, anchorZ := (x-5)>>4, floorDiv32(y+1, 12), (z-5)>>4
	distances := [4]int32{math.MaxInt32, math.MaxInt32, math.MaxInt32, math.MaxInt32}
	var closest [4]int
	for dx := range 2 {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := range 2 {
				gx, gy, gz := anchorX+int32(dx), anchorY+dy, anchorZ+int32(dz)
				index := int(((gy+7)*3+(gz-a.minGridZ))*3 + (gx - a.minGridX))
				center := &a.centers[index]
				if !center.locationReady {
					random := a.random.At(gx, gy, gz)
					center.x, center.y, center.z = gx*16+random.NextInt(10), gy*12+random.NextInt(9), gz*16+random.NextInt(10)
					center.locationReady = true
				}
				xd, yd, zd := center.x-x, center.y-y, center.z-z
				distance := xd*xd + yd*yd + zd*zd
				for rank := range 4 {
					if distance <= distances[rank] {
						for move := 3; move > rank; move-- {
							distances[move], closest[move] = distances[move-1], closest[move-1]
						}
						distances[rank], closest[rank] = distance, index
						break
					}
				}
			}
		}
	}
	first := a.status(closest[0])
	similarity12 := aquiferSimilarity(distances[0], distances[1])
	material := first.at(y)
	const flowingSimilarity = 1.0 - (144.0-100.0)/25.0
	if similarity12 <= 0 {
		return material, similarity12 >= flowingSimilarity && first != a.status(closest[1])
	}
	if material == AquiferWater && globalAquiferStatus(y-1).at(y-1) == AquiferLava {
		return material, true
	}
	barrier := math.NaN()
	second := a.status(closest[1])
	if density+similarity12*a.pressure(x, y, z, &barrier, first, second) > 0 {
		return AquiferSolid, false
	}
	third := a.status(closest[2])
	similarity13 := aquiferSimilarity(distances[0], distances[2])
	if similarity13 > 0 && density+similarity12*similarity13*a.pressure(x, y, z, &barrier, first, third) > 0 {
		return AquiferSolid, false
	}
	similarity23 := aquiferSimilarity(distances[1], distances[2])
	if similarity23 > 0 && density+similarity12*similarity23*a.pressure(x, y, z, &barrier, second, third) > 0 {
		return AquiferSolid, false
	}
	if first != second || similarity23 >= flowingSimilarity && second != third || similarity13 >= flowingSimilarity && first != third {
		return material, true
	}
	return material, similarity13 >= flowingSimilarity && aquiferSimilarity(distances[0], distances[3]) >= flowingSimilarity && first != a.status(closest[3])
}

func aquiferSimilarity(first, second int32) float64 { return 1 - float64(second-first)/25 }

func (a *OverworldAquifer) pressure(x, y, z int32, barrier *float64, first, second aquiferStatus) float64 {
	one, two := first.at(y), second.at(y)
	if one == AquiferLava && two == AquiferWater || one == AquiferWater && two == AquiferLava {
		return 2
	}
	difference := math.Abs(float64(first.level - second.level))
	if difference == 0 {
		return 0
	}
	above := float64(y) + 0.5 - float64(first.level+second.level)*0.5
	edge := difference*0.5 - math.Abs(above)
	var gradient float64
	if above > 0 {
		if edge > 0 {
			gradient = edge / 1.5
		} else {
			gradient = edge / 2.5
		}
	} else {
		edge += 3
		if edge > 0 {
			gradient = edge / 3
		} else {
			gradient = edge / 10
		}
	}
	noise := 0.0
	if gradient >= -2 && gradient <= 2 {
		if math.IsNaN(*barrier) {
			// DensityFunctions.noise(parameters, 0.5) scales Y only.
			*barrier = float64(a.noise.barrier.Sample(float64(x), float64(y)*0.5, float64(z)))
		}
		noise = *barrier
	}
	return 2 * (noise + gradient)
}

func (a *OverworldAquifer) status(index int) aquiferStatus {
	center := &a.centers[index]
	if !center.statusReady {
		center.status = a.computeFluid(center.x, center.y, center.z)
		center.statusReady = true
	}
	return center.status
}

var aquiferSurfaceOffsets = [...][2]int32{{0, 0}, {-2, -1}, {-1, -1}, {0, -1}, {1, -1}, {-3, 0}, {-2, 0}, {-1, 0}, {1, 0}, {-2, 1}, {-1, 1}, {0, 1}, {1, 1}}

func (a *OverworldAquifer) surfaceLevel(x, z int32) int32 {
	x, z = x&^3, z&^3
	ix, iz := ((x-a.baseX)>>2)+16, ((z-a.baseZ)>>2)+8
	if !a.surfaceReady[ix][iz] {
		a.surface[ix][iz] = a.noise.PreliminarySurfaceHeight(x, z)
		a.surfaceReady[ix][iz] = true
	}
	return a.surface[ix][iz]
}

func (a *OverworldAquifer) computeFluid(x, y, z int32) aquiferStatus {
	global := globalAquiferStatus(y)
	lowest := int32(math.MaxInt32)
	underGlobalFluid := false
	for i, offset := range aquiferSurfaceOffsets {
		sx, sz := x+offset[0]*16, z+offset[1]*16
		surface := a.surfaceLevel(sx, sz)
		adjusted := surface + 8
		if i == 0 && y-12 > adjusted {
			return global
		}
		pokesAbove := y+12 > adjusted
		if pokesAbove || i == 0 {
			globalAtSurface := globalAquiferStatus(adjusted)
			if globalAtSurface.at(adjusted) != AquiferAir {
				if i == 0 {
					underGlobalFluid = true
				}
				if pokesAbove {
					return globalAtSurface
				}
			}
		}
		lowest = min(lowest, surface)
	}
	level := a.computeSurfaceLevel(x, y, z, global, lowest, underGlobalFluid)
	material := global.material
	if level <= -10 && level != aquiferDryLevel && material != AquiferLava {
		lava := a.noise.lava.Sample(float64(x>>6), float64(floorDiv32(y, 40)), float64(z>>6))
		if terrainAbs(lava) > 0.3 {
			material = AquiferLava
		}
	}
	return aquiferStatus{level, material}
}

func (a *OverworldAquifer) computeSurfaceLevel(x, y, z int32, global aquiferStatus, lowest int32, underGlobalFluid bool) int32 {
	column, _, _ := a.noise.shapeColumn(x, z)
	if column.Erosion < -0.225 && column.Depth(y) > 0.9 {
		return aquiferDryLevel
	}
	factor := 0.0
	if underGlobalFluid {
		factor = max(0, min(1, 1-float64(lowest+8-y)/64))
	}
	floodedness := float64(clampDensity(a.noise.floodedness.Sample(float64(x), float64(y)*0.67, float64(z)), -1, 1))
	if floodedness > 0.8-1.1*factor {
		return global.level
	}
	if floodedness <= 0.4-1.2*factor {
		return aquiferDryLevel
	}
	cellX, cellY, cellZ := x>>4, floorDiv32(y, 40), z>>4
	spread := a.noise.spread.Sample(float64(cellX), float64(cellY)*0.7142857142857143, float64(cellZ)) * 10
	quantized := int32(math.Floor(float64(spread)/3)) * 3
	return min(lowest, cellY*40+20+quantized)
}

func floorDiv32(value, divisor int32) int32 {
	quotient := value / divisor
	if value%divisor < 0 {
		quotient--
	}
	return quotient
}
