package bedrock

import "math"

const biomeColumnCells = 49

// BiomeSource combines the native climate lookup and eight-block spatial zoom.
// Its state is immutable; each generation worker supplies its own BiomeVolume.
type BiomeSource struct {
	climate *Climate
	seed    uint32
	ids     [len(climateBiomeNames)]uint32
}

// NewBiomeSource resolves Java climate-catalogue names to the caller's persistent
// biome IDs once. resolve must cover every catalogue name; it is not retained.
func NewBiomeSource(seed int64, climate *Climate, resolve func(string) uint32) *BiomeSource {
	source := &BiomeSource{climate: climate, seed: uint32(seed)}
	for index, name := range climateBiomeNames {
		source.ids[index] = resolve(name)
	}
	return source
}

type cachedBiomeCell struct {
	id      uint32
	x, y, z float32
}

// BiomeVolume caches the nine climate columns and their spatial jitter needed
// for one Overworld chunk. Its zero value must be prepared before sampling.
type BiomeVolume struct {
	cells [3 * 3 * biomeColumnCells]cachedBiomeCell
}

// Prepare fills a worker-local volume for a chunk. Native climate lookup begins
// at Y=-72 to establish tie history before the first usable cell at Y=-64.
// Native extra horizontal padding columns are not needed for this chunk.
func (source *BiomeSource) Prepare(chunkX, chunkZ int32, volume *BiomeVolume) {
	for x := range 3 {
		cellX := chunkX*2 + int32(x)
		for z := range 3 {
			cellZ := chunkZ*2 + int32(z)
			var column ClimateColumn
			source.climate.SampleColumn(cellX*8, cellZ*8, &column)
			var lookup ClimateLookup
			lookup.biomeIndex(column.PointAt(-72))
			base := (x*3 + z) * biomeColumnCells
			for y := range biomeColumnCells {
				cellY := int32(y - 8)
				cell := &volume.cells[base+y]
				cell.id = source.ids[lookup.biomeIndex(column.PointAt(cellY*8))]
				cell.x, cell.y, cell.z = voronoiOffset(source.seed, cellX, cellY, cellZ)
			}
		}
	}
}

// Biome returns the persistent biome ID at local X/Z coordinates [0,15] and
// absolute Y in [-64,319]. It neither resamples noise nor advances random state.
func (volume *BiomeVolume) Biome(x uint8, y int16, z uint8) uint32 {
	base := (int(x>>3)*3+int(z>>3))*biomeColumnCells + int((y+64)>>3)
	fx, fy, fz := float32(x&7)*0.125, float32(y&7)*0.125, float32(z&7)*0.125
	best, selected := float32(math.MaxFloat32), base
	for corner := range 8 {
		dx, dy, dz := corner>>2, corner>>1&1, corner&1
		index := base + (dx*3+dz)*biomeColumnCells + dy
		cell := &volume.cells[index]
		ox := float32(cell.x + float32(fx-float32(dx)))
		oy := float32(cell.y + float32(fy-float32(dy)))
		oz := float32(cell.z + float32(fz-float32(dz)))
		distance := float32(float32(float32(ox*ox)+float32(oy*oy)) + float32(oz*oz))
		if distance < best {
			best, selected = distance, index
		}
	}
	return volume.cells[selected].id
}
