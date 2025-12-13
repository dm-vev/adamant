package model

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// ChorusPlant is a model used by chorus plant blocks. The collision boxes vary based on nearby chorus blocks.
type ChorusPlant struct{}

// BBox returns multiple cube.BBox depending on how many connections the chorus plant has with its surrounding blocks.
func (ChorusPlant) BBox(pos cube.Pos, s world.BlockSource) []cube.BBox {
	const (
		min = 0.1875
		max = 0.8125
	)

	boxes := make([]cube.BBox, 0, 7)
	boxes = append(boxes, cube.Box(min, min, min, max, max, max))

	if isChorusPlantOrFlower(s.Block(pos.Side(cube.FaceWest))) {
		boxes = append(boxes, cube.Box(0, min, min, min, max, max))
	}
	if isChorusPlantOrFlower(s.Block(pos.Side(cube.FaceEast))) {
		boxes = append(boxes, cube.Box(max, min, min, 1, max, max))
	}
	if isChorusPlantOrFlower(s.Block(pos.Side(cube.FaceUp))) {
		boxes = append(boxes, cube.Box(min, max, min, max, 1, max))
	}
	if isChorusPlantOrFlowerOrEndStone(s.Block(pos.Side(cube.FaceDown))) {
		boxes = append(boxes, cube.Box(min, 0, min, max, min, max))
	}
	if isChorusPlantOrFlower(s.Block(pos.Side(cube.FaceNorth))) {
		boxes = append(boxes, cube.Box(min, min, 0, max, max, min))
	}
	if isChorusPlantOrFlower(s.Block(pos.Side(cube.FaceSouth))) {
		boxes = append(boxes, cube.Box(min, min, max, max, max, 1))
	}
	return boxes
}

// FaceSolid always returns false.
func (ChorusPlant) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}

func isChorusPlantOrFlower(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:chorus_plant" || name == "minecraft:chorus_flower"
}

func isChorusPlantOrFlowerOrEndStone(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:chorus_plant" || name == "minecraft:chorus_flower" || name == "minecraft:end_stone"
}
