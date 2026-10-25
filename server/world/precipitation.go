package world

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// tickPrecipitation ports the precipitation step of ServerLevel.tickChunk:
// with a 1/16 chance per ticked chunk a random column is picked; exposed
// water below its motion-blocking height freezes into ice in cold biomes and,
// while it rains, snow accumulates up to max_snow_accumulation_height layers.
func (w *World) tickPrecipitation(tx *Tx, chunk ChunkPos) {
	if w.r.IntN(16) != 0 {
		return
	}
	x, z := int(chunk[0])<<4+w.r.IntN(16), int(chunk[1])<<4+w.r.IntN(16)
	top := cube.Pos{x, tx.motionBlockingHeight(x, z), z}
	below := top.Side(cube.FaceDown)
	if tx.shouldFreeze(below) {
		if ice, ok := w.conf.Blocks.BlockByName("minecraft:ice", nil); ok {
			tx.SetBlock(below, ice, nil)
		}
	}
	w.set.Lock()
	raining := w.set.Raining
	w.set.Unlock()
	if !raining || !w.Dimension().WeatherCycle() {
		return
	}
	w.accumulateSnow(tx, top)
	if p := tx.precipitationAt(below); p != PrecipitationNone {
		if receiver, ok := tx.Block(below).(PrecipitationReceiver); ok {
			receiver.HandlePrecipitation(below, tx, p)
		}
	}
}

// Precipitation is the kind of weather falling at a position.
type Precipitation uint8

const (
	PrecipitationNone Precipitation = iota
	PrecipitationRain
	PrecipitationSnow
)

// PrecipitationReceiver is a block that reacts to rain or snow falling onto
// it, as Block.handlePrecipitation.
type PrecipitationReceiver interface {
	HandlePrecipitation(pos cube.Pos, tx *Tx, p Precipitation)
}

// precipitationAt ports Biome.getPrecipitationAt: no precipitation in dry
// biomes, snow where it is cold enough and rain elsewhere.
func (tx *Tx) precipitationAt(pos cube.Pos) Precipitation {
	if !tx.biome(pos).HasPrecipitation() {
		return PrecipitationNone
	}
	if tx.coldEnoughToSnow(pos) {
		return PrecipitationSnow
	}
	return PrecipitationRain
}

// accumulateSnow adds a snow layer at top up to max_snow_accumulation_height
// layers where snow can form.
func (w *World) accumulateSnow(tx *Tx, top cube.Pos) {
	maxHeight := w.GameRuleInt("max_snow_accumulation_height")
	if maxHeight <= 0 || !tx.shouldSnow(top) {
		return
	}
	layers := 0
	if name, props := tx.Block(top).EncodeBlock(); name == "minecraft:snow_layer" {
		height, _ := props["height"].(int32)
		layers = int(height) + 1
		if layers >= min(maxHeight, 8) {
			return
		}
	}
	if snow, ok := w.conf.Blocks.BlockByName("minecraft:snow_layer", map[string]any{"height": int32(layers), "covered_bit": uint8(0)}); ok {
		tx.SetBlock(top, snow, nil)
	}
}

// motionBlockingHeight returns the first Y above the highest block that has
// a collision box or contains a liquid (Java's MOTION_BLOCKING heightmap).
func (tx *Tx) motionBlockingHeight(x, z int) int {
	r := tx.Range()
	for y := max(tx.HighestBlock(x, z), r.Min()); y >= r.Min(); y-- {
		pos := cube.Pos{x, y, z}
		if _, liquid := tx.Liquid(pos); liquid || len(tx.Block(pos).Model().BBox(pos, tx)) > 0 {
			return y + 1
		}
	}
	return r.Min()
}

// coldEnoughToSnow ports Biome.coldEnoughToSnow.
func (tx *Tx) coldEnoughToSnow(pos cube.Pos) bool {
	return tx.temperature(pos) < 0.15
}

// blockLight returns the block light level at pos.
func (tx *Tx) blockLight(pos cube.Pos) uint8 {
	tx.World().flushLight()
	c, ok := tx.World().loadedChunk(chunkPosFromBlockPos(pos))
	if !ok || pos.OutOfBounds(tx.Range()) {
		return 0
	}
	return c.SubChunk(int16(pos[1])).BlockLight(uint8(pos[0]&15), uint8(pos[1]&15), uint8(pos[2]&15))
}

// shouldFreeze ports Biome.shouldFreeze with neighbour checks: a water
// source block in a cold biome with block light below 10 freezes unless it is
// surrounded by water on all four horizontal sides.
func (tx *Tx) shouldFreeze(pos cube.Pos) bool {
	if !tx.coldEnoughToSnow(pos) || pos.OutOfBounds(tx.Range()) || tx.blockLight(pos) >= 10 {
		return false
	}
	l, ok := tx.Block(pos).(Liquid)
	if !ok || !isWater(l) || l.LiquidDepth() != 8 || l.LiquidFalling() {
		return false
	}
	for _, face := range cube.HorizontalFaces() {
		if n, ok := tx.Liquid(pos.Side(face)); !ok || !isWater(n) {
			return true
		}
	}
	return false
}

// shouldSnow ports Biome.shouldSnow: snow forms in cold, dark-enough air or
// on existing snow layers where a snow layer can survive.
func (tx *Tx) shouldSnow(pos cube.Pos) bool {
	if !tx.coldEnoughToSnow(pos) || pos.OutOfBounds(tx.Range()) || tx.blockLight(pos) >= 10 {
		return false
	}
	name, _ := tx.Block(pos).EncodeBlock()
	if name != "minecraft:air" && name != "minecraft:snow_layer" {
		return false
	}
	below := pos.Side(cube.FaceDown)
	belowName, props := tx.Block(below).EncodeBlock()
	switch belowName {
	case "minecraft:ice", "minecraft:packed_ice", "minecraft:barrier":
		return false
	case "minecraft:honey_block", "minecraft:soul_sand", "minecraft:mud":
		return true
	case "minecraft:snow_layer":
		height, _ := props["height"].(int32)
		return height == 7
	}
	return tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

func isWater(l Liquid) bool {
	name, _ := l.EncodeBlock()
	return strings.HasSuffix(name, "water")
}
