package item

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Cushion is a coloured, rideable decoration item.
type Cushion struct {
	Colour Colour
}

// MaxCount returns the cushion stack limit.
func (Cushion) MaxCount() int { return 16 }

// EncodeItem returns the Bedrock item identifier.
func (c Cushion) EncodeItem() (string, int16) {
	return "minecraft:" + c.Colour.String() + "_cushion", 0
}

// UseOnBlock places the cushion on the top of a supported block.
func (c Cushion) UseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if face != cube.FaceUp {
		return false
	}
	blockPos := pos
	if _, replaceable := tx.Block(pos).(interface{ ReplaceableBy(world.Block) bool }); !replaceable {
		blockPos = pos.Side(face)
	}
	spawnPos := blockPos.Vec3Centre()
	spawnPos[1] = float64(pos[1]) + clickPos[1]
	box := cube.Box(-0.5, 0, -0.5, 0.5, 0.25, 0.5).Translate(spawnPos)
	if !cushionHasSupport(box, tx) || cushionCovered(box, tx) || cushionOccupied(box, tx) {
		return false
	}
	spawn := tx.World().EntityRegistry().Config().Cushion
	if spawn == nil {
		return false
	}
	opts := world.EntitySpawnOpts{Position: spawnPos, Rotation: cube.Rotation{user.Rotation().Yaw()}}
	tx.AddEntity(spawn(opts, int(c.Colour.Uint8())))
	tx.PlaySound(spawnPos, sound.Custom{Name: "entity.cushion.place", Volume: 0.75, Pitch: 0.8})
	ctx.SubtractFromCount(1)
	return true
}

func cushionHasSupport(box cube.BBox, tx *world.Tx) bool {
	anchor := cube.Box(box.Min().X(), box.Min().Y()-0.125, box.Min().Z(), box.Max().X(), box.Min().Y(), box.Max().Z())
	minPos, maxPos := anchor.Min(), anchor.Max()
	for x := int(math.Floor(minPos[0])); x <= int(math.Floor(maxPos[0])); x++ {
		for y := int(math.Floor(minPos[1])); y <= int(math.Floor(maxPos[1])); y++ {
			for z := int(math.Floor(minPos[2])); z <= int(math.Floor(maxPos[2])); z++ {
				pos := cube.Pos{x, y, z}
				for _, support := range tx.Block(pos).Model().BBox(pos, tx) {
					if support.Translate(pos.Vec3()).IntersectsWith(anchor) {
						return true
					}
				}
			}
		}
	}
	return false
}

func cushionCovered(box cube.BBox, tx *world.Tx) bool {
	minPos, maxPos := box.Min(), box.Max()
	for x := int(math.Floor(minPos[0])); x <= int(math.Floor(maxPos[0])); x++ {
		for y := int(math.Floor(minPos[1])); y <= int(math.Floor(maxPos[1])); y++ {
			for z := int(math.Floor(minPos[2])); z <= int(math.Floor(maxPos[2])); z++ {
				pos := cube.Pos{x, y, z}
				if nonSuffocating, ok := tx.Block(pos).(interface{ PreventsSuffocation() bool }); ok && nonSuffocating.PreventsSuffocation() {
					continue
				}
				for _, obstacle := range tx.Block(pos).Model().BBox(pos, tx) {
					if obstacle.Translate(pos.Vec3()).IntersectsWith(box) {
						return true
					}
				}
			}
		}
	}
	return false
}

func cushionOccupied(box cube.BBox, tx *world.Tx) bool {
	for other := range tx.EntitiesWithin(box) {
		if other.H().Type().EncodeEntity() == "minecraft:cushion" && other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(box) {
			return true
		}
	}
	return false
}
