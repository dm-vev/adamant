package bedrock

// Aquifer holds immutable Overworld fluid noises and positional randomness.
// Workers keep their mutable cell caches in separate AquiferChunk values.
type Aquifer struct {
	barrier, floodedness, spread, lava NormalNoise
	positions                          RandomFactory
	climate                            *Climate
}

const (
	aquiferMinY    = -6
	aquiferHeight  = 34
	aquiferUnknown = int32(0x7fffffff)
)

type aquiferCell struct {
	x, y, z, level int32
	lava           bool
}

// AquiferChunk caches fluid centres for one 16×16 Overworld chunk. Sampling is
// restricted to that chunk's block coordinates and the height range -64..319.
type AquiferChunk struct {
	aquifer       *Aquifer
	surfaceHeight func(x, z int32) int32
	minX, minZ    int32
	cells         [3 * aquiferHeight * 3]aquiferCell
}

// AquiferSample describes the nearest fluid and the density of its separating
// barrier. A positive terrain density plus Pressure produces solid terrain;
// otherwise positions below Level contain the selected fluid, and the rest air.
type AquiferSample struct {
	Level               int32
	Lava                bool
	Pressure            float32
	ScheduleFluidUpdate bool
}

func NewAquifer(seed int64, climate *Climate) *Aquifer {
	factory := NewRandomFactory(seed)
	random := factory.fromName([]byte("minecraft:aquifer"))
	return &Aquifer{
		barrier:     factory.Noise("minecraft:aquifer_barrier", -3, 1),
		floodedness: factory.Noise("minecraft:aquifer_fluid_level_floodedness", -7, 1),
		spread:      factory.Noise("minecraft:aquifer_fluid_level_spread", -5, 1),
		lava:        factory.Noise("minecraft:aquifer_lava", -1, 1),
		positions:   random.fork(),
		climate:     climate,
	}
}

// Prepare resets scratch for chunk coordinates. surfaceHeight receives aligned
// block coordinates and returns the preliminary terrain surface, not the final
// decorated heightmap. The callback and scratch belong to the worker.
func (a *Aquifer) Prepare(chunkX, chunkZ int32, surfaceHeight func(x, z int32) int32, scratch *AquiferChunk) {
	scratch.aquifer = a
	scratch.surfaceHeight = surfaceHeight
	scratch.minX, scratch.minZ = chunkX-1, chunkZ-1
	for i := range scratch.cells {
		scratch.cells[i].x = aquiferUnknown
		scratch.cells[i].level = aquiferUnknown
	}
}

func (c *AquiferChunk) cell(x, y, z int32) *aquiferCell {
	cell := &c.cells[((x-c.minX)*aquiferHeight+y-aquiferMinY)*3+z-c.minZ]
	if cell.x == aquiferUnknown {
		random := c.aquifer.positions.at(x, y, z)
		cell.x = x*16 + int32(random.bounded(10))
		cell.y = y*12 + int32(random.bounded(9))
		cell.z = z*16 + int32(random.bounded(10))
	}
	return cell
}

func (c *AquiferChunk) Sample(x, y, z int32) AquiferSample {
	if y < -54 {
		return AquiferSample{Level: -54, Lava: true, ScheduleFluidUpdate: y == -55}
	}
	gridX, gridY, gridZ := (x-5)>>4, floorDiv32(y+1, 12), (z-5)>>4
	var nearest [3]*aquiferCell
	distances := [3]int64{0x7fffffffffffffff, 0x7fffffffffffffff, 0x7fffffffffffffff}
	for dx := int32(0); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(0); dz <= 1; dz++ {
				cell := c.cell(gridX+dx, gridY+dy, gridZ+dz)
				xd, yd, zd := int64(cell.x-x), int64(cell.y-y), int64(cell.z-z)
				distance := xd*xd + yd*yd + zd*zd
				if distance <= distances[0] {
					distances[2], nearest[2] = distances[1], nearest[1]
					distances[1], nearest[1] = distances[0], nearest[0]
					distances[0], nearest[0] = distance, cell
				} else if distance <= distances[1] {
					distances[2], nearest[2] = distances[1], nearest[1]
					distances[1], nearest[1] = distance, cell
				} else if distance <= distances[2] {
					distances[2], nearest[2] = distance, cell
				}
			}
		}
	}
	c.fluidLevel(nearest[0])
	similarity12 := float32(float32(float32(distances[1]-distances[0])*-0.04) + 1)
	result := AquiferSample{
		Level:               nearest[0].level,
		Lava:                nearest[0].lava,
		ScheduleFluidUpdate: similarity12 > -0.4,
	}
	if similarity12 <= -1 {
		return result
	}
	c.fluidLevel(nearest[1])
	c.fluidLevel(nearest[2])
	barrier := float32(0x1p-126)
	pressure12 := c.pressure(x, y, z, nearest[0], nearest[1], &barrier)
	pressure13 := c.pressure(x, y, z, nearest[0], nearest[2], &barrier)
	pressure23 := c.pressure(x, y, z, nearest[1], nearest[2], &barrier)
	similarity13 := float32(1 - float32(float32(distances[2]-distances[0])*0.04))
	similarity23 := float32(1 - float32(float32(distances[2]-distances[1])*0.04))
	pressure := float32(max(float32(0), similarity13) * pressure13)
	pressure23 = float32(max(float32(0), similarity23) * pressure23)
	if pressure <= pressure23 {
		pressure = pressure23
	}
	if pressure12 <= pressure {
		pressure12 = pressure
	}
	similarity12 = max(0, similarity12)
	pressure = float32(float32(similarity12+similarity12) * pressure12)
	// Native comparisons retain negative zero, unlike max(0, pressure).
	if pressure >= 0 {
		result.Pressure = pressure
	}
	return result
}

func (c *AquiferChunk) pressure(x, y, z int32, first, second *aquiferCell, barrier *float32) float32 {
	if y < first.level && y < second.level && first.lava != second.lava {
		return 1
	}
	if first.level == second.level {
		return 0
	}
	difference := first.level - second.level
	if difference < 0 {
		difference = -difference
	}
	midpoint := float32(float32(first.level+second.level) * 0.5)
	offset := float32(float32(float32(y)+0.5) - midpoint)
	absolute := offset
	if absolute < 0 {
		absolute = -absolute
	}
	pressure := float32(float32(float32(difference)*0.5) - absolute)
	if offset > 0 {
		if pressure > 0 {
			pressure = float32(pressure / 1.5)
		} else {
			pressure = float32(pressure / 2.5)
		}
	} else {
		pressure = float32(pressure + 3)
		if pressure > 0 {
			pressure = float32(pressure / 3)
		} else {
			pressure = float32(pressure / 10)
		}
	}
	if pressure >= -2 && pressure <= 2 {
		if *barrier == 0x1p-126 {
			*barrier = c.aquifer.barrier.Sample(float32(x), float32(y)*0.5, float32(z))
		}
		pressure = float32(pressure + *barrier)
	}
	return pressure
}

var aquiferSurfaceOffsets = [...][2]int32{
	{-2, -1}, {-1, -1}, {0, -1}, {1, -1},
	{-3, 0}, {-2, 0}, {-1, 0}, {0, 0}, {1, 0},
	{-2, 1}, {-1, 1}, {0, 1}, {1, 1},
}

func (c *AquiferChunk) fluidLevel(cell *aquiferCell) {
	if cell.level != aquiferUnknown {
		return
	}
	cell.level, cell.lava = 63, false
	minimumSurface := int32(0x7fff)
	centreUnderWater := false
	for _, offset := range aquiferSurfaceOffsets {
		surface := c.surfaceHeight((cell.x+offset[0]*16)&^3, (cell.z+offset[1]*16)&^3)
		centre := offset[0] == 0 && offset[1] == 0
		if centre && surface+8 < cell.y-12 {
			return
		}
		if (centre || cell.y+4 > surface) && surface+8 < 63 {
			if surface < cell.y+4 {
				return
			}
			centreUnderWater = centreUnderWater || centre
		}
		minimumSurface = min(minimumSurface, surface)
	}
	a := c.aquifer
	_, _, continents, erosion, weirdness := a.climate.terrainAt(cell.x, cell.z)
	offset := TerrainOffset(continents, erosion, weirdness)
	depth := float32(float32(float32(float32(cell.y)*-0.0078125)+1) + offset)
	depth = float32(depth - 0.50375)
	if erosion < -0.225 && depth > 0.9 {
		cell.level = -4096
		return
	}
	floodThreshold := float32(1)
	if centreUnderWater {
		floodThreshold = min(float32(1), max(float32(0), float32(float32(minimumSurface+8-cell.y)*0.015625)))
	}
	floodedness := a.floodedness.Sample(float32(cell.x), float32(cell.y)*0.67, float32(cell.z))
	floodedness = min(float32(1), max(float32(-1), floodedness))
	if float32(float32(floodThreshold*1.1)-0.3) < floodedness {
		return
	}
	if floodedness < float32(float32(floodThreshold*1.2)-0.8) {
		cell.level = -4096
		return
	}
	yCell := floorDiv32(cell.y, 40)
	spread := a.spread.Sample(float32(cell.x>>4), float32(yCell)/1.4, float32(cell.z>>4))
	spread = float32(float32(spread*10) / 3)
	spreadFloor := int32(spread)
	if spread < float32(spreadFloor) {
		spreadFloor--
	}
	level := spreadFloor*3 + yCell*40 + 20
	cell.level = min(minimumSurface, level)
	if level < -9 {
		lava := a.lava.Sample(float32(cell.x>>6), float32(yCell), float32(cell.z>>6))
		cell.lava = lava < -0.3 || lava > 0.3
	}
}

func floorDiv32(value, divisor int32) int32 {
	quotient := value / divisor
	if value%divisor < 0 {
		quotient--
	}
	return quotient
}
