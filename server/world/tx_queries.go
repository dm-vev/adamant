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

// RandomUint32 draws from the world's simulation RNG, shared with block-actor
// scheduling. Use it when a block's native algorithm consumes raw random words.
func (tx *Tx) RandomUint32() uint32 {
	return tx.World().randomUint32()
}

// RandomFloat32 consumes one raw simulation word using Core::Random's
// binary32 conversion. It may round a word near uint32's maximum to one.
func (tx *Tx) RandomFloat32() float32 {
	return nativeRandomFloat32(tx.World().randomUint32())
}

// RandomGaussianFloat32 draws from the native cached binary32 Gaussian stream.
// It shares raw simulation words with RandomUint32 and RandomFloat32.
func (tx *Tx) RandomGaussianFloat32() float32 {
	w := tx.World()
	return w.nativeGaussianFloat.next(w.randomUint32)
}

// HighestLightBlocker gets the Y value of the highest fully light blocking
// block at the x and z values passed in the World.
func (tx *Tx) HighestLightBlocker(x, z int) int {
	return tx.highestLightBlocker(x, z)
}

// HighestBlock looks up the highest non-air block in the World at a specific x
// and z. The y value of the highest block is returned, or 0 if no blocks were
// present in the column.
func (tx *Tx) HighestBlock(x, z int) int {
	return tx.highestBlock(x, z)
}

// Temperature returns the temperature in the World at a specific position.
// Higher altitudes and different biomes influence the temperature returned.
func (tx *Tx) Temperature(pos cube.Pos) float64 {
	return tx.temperature(pos)
}

// RainingAt checks if it is raining at a specific cube.Pos in the World. True
// is returned if it is raining, if the temperature is high enough in the biome
// for it not to be snow and if the block is above the top-most obstructing
// block.
func (tx *Tx) RainingAt(pos cube.Pos) bool {
	return tx.rainingAt(pos)
}

// SnowingAt checks if it is snowing at a specific cube.Pos in the World. True
// is returned if the temperature in the Biome at that position is sufficiently
// low, if it is raining and if it's above the top-most obstructing block.
func (tx *Tx) SnowingAt(pos cube.Pos) bool {
	return tx.snowingAt(pos)
}

// ThunderingAt checks if it is thundering at a specific cube.Pos in the World.
// True is returned if RainingAt returns true and if it is thundering in the
// world.
func (tx *Tx) ThunderingAt(pos cube.Pos) bool {
	return tx.thunderingAt(pos)
}

// Raining checks if it is raining anywhere in the World.
func (tx *Tx) Raining() bool {
	return tx.World().raining()
}

// Thundering checks if it is thundering anywhere in the World.
func (tx *Tx) Thundering() bool {
	return tx.World().thundering()
}
