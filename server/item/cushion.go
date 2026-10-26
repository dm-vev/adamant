package item

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
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
	face, clickPos = cushionCollisionHit(pos, face, clickPos, tx, user)
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
	if !cushionCanBePlacedAt(tx, box) || cushionOccupied(box, tx) {
		return false
	}
	spawn := tx.World().EntityRegistry().Config().Cushion
	if spawn == nil {
		return false
	}
	// Direction.fromYRot(yaw).toYRot(): the cushion faces the player's
	// nearest cardinal direction.
	yaw := math.Mod(math.Floor(user.Rotation().Yaw()/90+0.5), 4)
	if yaw < 0 {
		yaw += 4
	}
	opts := stackSpawnOpts(user, world.EntitySpawnOpts{Position: spawnPos, Rotation: cube.Rotation{yaw * 90}})
	tx.AddEntity(spawn(opts, int(c.Colour.Uint8())))
	tx.PlaySound(spawnPos, sound.Custom{Name: "entity.cushion.place", Volume: 0.75, Pitch: 0.8})
	ctx.SubtractFromCount(1)
	return true
}

// cushionCollisionHit ports CushionItem.recalculateContextForSpecialCollisionShapes:
// on cauldrons, hoppers and composters the click is traced on to the
// block's collision shape, so a cushion goes inside them.
func cushionCollisionHit(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user User) (cube.Face, mgl64.Vec3) {
	if user == nil {
		return face, clickPos
	}
	b := tx.Block(pos)
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return face, clickPos
	}
	if in, _ := gamedata.BlockTagContains("minecraft:cushion_uses_collision_shape", state.Name); !in {
		return face, clickPos
	}
	eye := user.Position()
	if e, ok := user.(interface{ EyeHeight() float64 }); ok {
		eye[1] += e.EyeHeight()
	}
	click := pos.Vec3().Add(clickPos)
	end := click.Add(click.Sub(eye).Normalize().Mul(0.001))
	r, ok := trace.BlockIntercept(pos, tx, b, eye, end, trace.CollisionShape)
	if !ok {
		return face, clickPos
	}
	return r.Face(), r.Position().Sub(pos.Vec3())
}

// cushionSlice is the thickness of the slices Cushion checks its anchor in.
const cushionSlice = 0.015625

// CushionWouldSurviveAt ports Cushion.wouldSurviveAt: a block shape lies
// within 1/64 below the cushion's box, and not every block in the box
// suffocates.
func CushionWouldSurviveAt(tx *world.Tx, box cube.BBox) bool {
	return cushionAnchorBelow(tx, box) && !cushionCovered(tx, box)
}

// cushionCanBePlacedAt ports Cushion.canBePlacedAt: the cushion would
// survive, and block collisions do not cover the bottom of its box.
func cushionCanBePlacedAt(tx *world.Tx, box cube.BBox) bool {
	return CushionWouldSurviveAt(tx, box) && !cushionAnchorBuried(tx, box)
}

// cushionAnchorBelow ports Cushion.hasAnchorBelow.
func cushionAnchorBelow(tx *world.Tx, box cube.BBox) bool {
	anchor := cube.Box(box.Min()[0], box.Min()[1]-cushionSlice, box.Min()[2], math.Nextafter(box.Max()[0], math.Inf(-1)), box.Min()[1], math.Nextafter(box.Max()[2], math.Inf(-1)))
	for x := int(math.Floor(anchor.Min()[0])); x <= int(math.Floor(anchor.Max()[0])); x++ {
		for y := int(math.Floor(anchor.Min()[1] - 0.125)); y <= int(math.Floor(anchor.Max()[1])); y++ {
			for z := int(math.Floor(anchor.Min()[2])); z <= int(math.Floor(anchor.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				boxes := tx.Block(pos).Model().BBox(pos, tx)
				if len(boxes) == 0 {
					continue
				}
				bounds := boxes[0]
				for _, bb := range boxes[1:] {
					bounds = cube.Box(min(bounds.Min()[0], bb.Min()[0]), min(bounds.Min()[1], bb.Min()[1]), min(bounds.Min()[2], bb.Min()[2]),
						max(bounds.Max()[0], bb.Max()[0]), max(bounds.Max()[1], bb.Max()[1]), max(bounds.Max()[2], bb.Max()[2]))
				}
				if aabbIntersects(bounds.Translate(pos.Vec3()), anchor) {
					return true
				}
			}
		}
	}
	return false
}

// aabbIntersects ports AABB.intersects: the boxes overlap with a volume.
func aabbIntersects(a, b cube.BBox) bool {
	return a.Min()[0] < b.Max()[0] && a.Max()[0] > b.Min()[0] &&
		a.Min()[1] < b.Max()[1] && a.Max()[1] > b.Min()[1] &&
		a.Min()[2] < b.Max()[2] && a.Max()[2] > b.Min()[2]
}

// cushionCovered ports Cushion.isCoveredBySuffocatingBlocks: every block
// within the box suffocates.
func cushionCovered(tx *world.Tx, box cube.BBox) bool {
	box = box.Grow(-1e-7)
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y <= int(math.Floor(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				b := tx.Block(pos)
				if p, ok := b.(interface{ PreventsSuffocation() bool }); ok && p.PreventsSuffocation() {
					return false
				}
				boxes := b.Model().BBox(pos, tx)
				if len(boxes) != 1 || boxes[0] != cube.Box(0, 0, 0, 1, 1, 1) {
					return false
				}
			}
		}
	}
	return true
}

// cushionAnchorBuried ports Cushion.isAnchorBuried: block collisions leave
// nothing of the bottom 1/64 of the box exposed.
func cushionAnchorBuried(tx *world.Tx, box cube.BBox) bool {
	slice := cube.Box(box.Min()[0], box.Min()[1], box.Min()[2], box.Max()[0], box.Min()[1]+cushionSlice, box.Max()[2]).Grow(-1e-7)
	var covers []cube.BBox
	for x := int(math.Floor(slice.Min()[0])); x <= int(math.Floor(slice.Max()[0])); x++ {
		for y := int(math.Floor(slice.Min()[1])); y <= int(math.Floor(slice.Max()[1])); y++ {
			for z := int(math.Floor(slice.Min()[2])); z <= int(math.Floor(slice.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				for _, bb := range tx.Block(pos).Model().BBox(pos, tx) {
					bb = bb.Translate(pos.Vec3())
					if bb.Min()[1] <= slice.Min()[1] && bb.Max()[1] >= slice.Max()[1] && aabbIntersects(bb, slice) {
						covers = append(covers, bb)
					}
				}
			}
		}
	}
	// The slice is covered if every cell between the edges of the colliders
	// lies in one of them.
	xs, zs := []float64{slice.Min()[0], slice.Max()[0]}, []float64{slice.Min()[2], slice.Max()[2]}
	for _, c := range covers {
		xs = append(xs, max(slice.Min()[0], min(slice.Max()[0], c.Min()[0])), max(slice.Min()[0], min(slice.Max()[0], c.Max()[0])))
		zs = append(zs, max(slice.Min()[2], min(slice.Max()[2], c.Min()[2])), max(slice.Min()[2], min(slice.Max()[2], c.Max()[2])))
	}
	slices.Sort(xs)
	slices.Sort(zs)
	for i := 0; i+1 < len(xs); i++ {
		for j := 0; j+1 < len(zs); j++ {
			if xs[i+1]-xs[i] <= 0 || zs[j+1]-zs[j] <= 0 {
				continue
			}
			cx, cz := (xs[i]+xs[i+1])/2, (zs[j]+zs[j+1])/2
			covered := false
			for _, c := range covers {
				if cx > c.Min()[0] && cx < c.Max()[0] && cz > c.Min()[2] && cz < c.Max()[2] {
					covered = true
					break
				}
			}
			if !covered {
				return false
			}
		}
	}
	return true
}

func cushionOccupied(box cube.BBox, tx *world.Tx) bool {
	for other := range tx.EntitiesWithin(box) {
		if other.H().Type().EncodeEntity() == "minecraft:cushion" && other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(box) {
			return true
		}
	}
	return false
}
