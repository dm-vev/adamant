package item

import (
	"log/slog"
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/painting"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Painting is an item that may be used to hang decorative paintings on walls.
type Painting struct{}

// UseOnBlock attempts to place a painting entity on the targeted face if there is enough space and backing.
func (Painting) UseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	if face == cube.FaceUp || face == cube.FaceDown {
		return false
	}

	dir := face.Direction()
	right := dir.RotateRight()

	u := horizontalComponent(right, clickPos)
	v := clampUnit(clickPos[1])

	type candidate struct {
		motive painting.Motive
		origin cube.Pos
	}

	var (
		bestArea float64
		choices  []candidate
	)

	for _, motive := range painting.Motives() {
		widthF, heightF := motive.Size()
		width := int(math.Round(widthF))
		height := int(math.Round(heightF))
		if width == 0 || height == 0 {
			continue
		}

		col := clampIndex(int(math.Floor(u*widthF)), width)
		row := clampIndex(int(math.Floor(v*heightF)), height)

		origin := pos
		if col > 0 {
			for i := 0; i < col; i++ {
				origin = origin.Side(right.Face().Opposite())
			}
		}
		if row > 0 {
			for j := 0; j < row; j++ {
				origin = origin.Side(cube.FaceDown)
			}
		}

		if !paintingFits(tx, origin, face, right, width, height) {
			continue
		}

		area := widthF * heightF
		if area > bestArea {
			bestArea = area
			choices = choices[:0]
		}
		if area == bestArea {
			choices = append(choices, candidate{motive: motive, origin: origin})
		}
	}

	if len(choices) == 0 {
		return false
	}

	choice := choices[0]
	if len(choices) > 1 {
		choice = choices[rand.N(len(choices))]
	}

	create := tx.World().EntityRegistry().Config().Painting
	if create == nil {
		slog.Default().Info("painting: reject - entity factory missing", "world", tx.World().Name())
		return false
	}

	widthF, _ := choice.motive.Size()
	spawn := paintingSpawnPosition(choice.origin, face, right, int(math.Round(widthF)))

	opts := world.EntitySpawnOpts{Position: spawn}
	tx.AddEntity(create(opts, choice.motive, dir))

	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem returns the item ID and meta of the painting item.
func (Painting) EncodeItem() (string, int16) {
	return "minecraft:painting", 0
}

func horizontalComponent(right cube.Direction, clickPos mgl64.Vec3) float64 {
	switch right {
	case cube.North:
		return clampUnit(1 - clickPos[2])
	case cube.South:
		return clampUnit(clickPos[2])
	case cube.West:
		return clampUnit(1 - clickPos[0])
	case cube.East:
		fallthrough
	default:
		return clampUnit(clickPos[0])
	}
}

func clampUnit(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v >= 1 {
		return math.Nextafter(1, 0)
	}
	return v
}

func clampIndex(i, length int) int {
	if i < 0 {
		return 0
	}
	if i >= length {
		return length - 1
	}
	return i
}

func paintingFits(tx *world.Tx, origin cube.Pos, face cube.Face, right cube.Direction, width, height int) bool {
	rightFace := right.Face()
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			support := origin
			for i := 0; i < x; i++ {
				support = support.Side(rightFace)
			}
			for j := 0; j < y; j++ {
				support = support.Side(cube.FaceUp)
			}

			if !tx.Block(support).Model().FaceSolid(support, face, tx) {
				return false
			}

			front := support.Side(face)
			if _, ok := tx.Block(front).Model().(model.Empty); !ok {
				return false
			}
			if _, liquid := tx.Liquid(front); liquid {
				return false
			}
		}
	}
	bounds := paintingBounds(origin, face, right, width, height)
	for existing := range tx.EntitiesWithin(bounds) {
		if existing.H().Type().EncodeEntity() == "minecraft:painting" {
			return false
		}
	}
	return true
}

func paintingSpawnPosition(origin cube.Pos, face cube.Face, right cube.Direction, width int) mgl64.Vec3 {
	front := origin.Side(face)
	pos := front.Vec3()

	rightVec := directionVec(right)
	dir := face.Direction()
	wallVec := directionVec(dir.Opposite())

	pos = pos.Add(rightVec.Mul(float64(width) / 2))
	pos = pos.Add(wallVec.Mul(0.5))
	return pos
}

func directionVec(d cube.Direction) mgl64.Vec3 {
	switch d {
	case cube.North:
		return mgl64.Vec3{0, 0, -1}
	case cube.South:
		return mgl64.Vec3{0, 0, 1}
	case cube.West:
		return mgl64.Vec3{-1, 0, 0}
	case cube.East:
		return mgl64.Vec3{1, 0, 0}
	default:
		return mgl64.Vec3{}
	}
}

func paintingBounds(origin cube.Pos, face cube.Face, right cube.Direction, width, height int) cube.BBox {
	minX, minY, minZ := math.Inf(1), math.Inf(1), math.Inf(1)
	maxX, maxY, maxZ := math.Inf(-1), math.Inf(-1), math.Inf(-1)
	rightFace := right.Face()
	for x := 0; x < width; x++ {
		for y := 0; y < height; y++ {
			front := origin
			for i := 0; i < x; i++ {
				front = front.Side(rightFace)
			}
			for j := 0; j < y; j++ {
				front = front.Side(cube.FaceUp)
			}
			front = front.Side(face)
			blockMin := front.Vec3()
			blockMax := blockMin.Add(mgl64.Vec3{1, 1, 1})
			if blockMin[0] < minX {
				minX = blockMin[0]
			}
			if blockMin[1] < minY {
				minY = blockMin[1]
			}
			if blockMin[2] < minZ {
				minZ = blockMin[2]
			}
			if blockMax[0] > maxX {
				maxX = blockMax[0]
			}
			if blockMax[1] > maxY {
				maxY = blockMax[1]
			}
			if blockMax[2] > maxZ {
				maxZ = blockMax[2]
			}
		}
	}
	return cube.Box(minX, minY, minZ, maxX, maxY, maxZ)
}
