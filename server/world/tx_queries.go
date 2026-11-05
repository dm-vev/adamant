package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
)

// highestLightBlocker gets the Y value of the highest fully light blocking
// block at the x and z values passed in the World.
func (tx *Tx) highestLightBlocker(x, z int) int {
	return int(tx.chunk(ChunkPos{int32(x >> 4), int32(z >> 4)}).HighestLightBlocker(uint8(x), uint8(z)))
}

// highestBlock looks up the highest non-air block in the World at a specific x
// and z The y value of the highest block is returned, or 0 if no blocks were
// present in the column.
func (tx *Tx) highestBlock(x, z int) int {
	return int(tx.chunk(ChunkPos{int32(x >> 4), int32(z >> 4)}).highestBlock(uint8(x), uint8(z)))
}

// surfaceHeights caches the highest block in each x and z of a column, as
// Java keeps the WORLD_SURFACE height map of a chunk rather than scanning
// it, which mobs spawning naturally ask of every chunk every tick. Setting a
// block through Column.SetBlock keeps it up to date, as Java updates the
// height map with every block set; changing blocks otherwise drops it.
type surfaceHeights struct {
	y     [256]int16
	stale [256]bool
}

// highestBlock returns what HighestBlock returns for x and z, from the
// cache, which the first call fills for the whole column at once.
func (c *Column) highestBlock(x, z uint8) int16 {
	s := c.surface
	if s == nil {
		s = &surfaceHeights{}
		c.HighestBlocks(&s.y)
		c.surface = s
	}
	i := int(x&15)<<4 | int(z&15)
	if s.stale[i] {
		s.y[i], s.stale[i] = c.HighestBlock(x&15, z&15), false
	}
	return s.y[i]
}

// surfaceBlockSet updates the highest block cached for x and z once the
// block at x, y and z of the first layer was set to the runtime ID passed.
func (c *Column) surfaceBlockSet(x uint8, y int16, z uint8, rid uint32) {
	s := c.surface
	if s == nil {
		return
	}
	i := int(x&15)<<4 | int(z&15)
	if s.stale[i] {
		return
	}
	if air := rid == c.BlockRegistry().AirRuntimeID(); !air && y > s.y[i] {
		s.y[i] = y
	} else if air && y == s.y[i] {
		// The highest block was removed: find the next one when asked.
		s.stale[i] = true
	}
}

// highestObstructingBlock returns the highest block in the World at a given x
// and z that has at least a solid top or bottom face.
func (tx *Tx) highestObstructingBlock(x, z int) int {
	yHigh := tx.highestBlock(x, z)
	src := worldSource{tx: tx}
	for y := yHigh; y >= tx.Range()[0]; y-- {
		pos := cube.Pos{x, y, z}
		m := tx.block(pos).Model()
		if m.FaceSolid(pos, cube.FaceUp, src) || m.FaceSolid(pos, cube.FaceDown, src) {
			return y
		}
	}
	return tx.Range()[0]
}

// temperature returns the temperature in the World at a specific position.
// Higher altitudes and different biomes influence the temperature returned.
func (tx *Tx) temperature(pos cube.Pos) float64 {
	const (
		tempDrop = 1.0 / 600
		seaLevel = 64
	)
	diff := max(pos[1]-seaLevel, 0)
	return tx.biome(pos).Temperature() - float64(diff)*tempDrop
}
