package item

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// placeContextPos ports BlockPlaceContext.getClickedPos for an item that
// places no block: a clicked block that gives way, such as short grass, is
// taken itself, otherwise the block next to the clicked face.
func placeContextPos(tx *world.Tx, pos cube.Pos, face cube.Face) cube.Pos {
	if r, ok := tx.Block(pos).(interface{ ReplaceableBy(world.Block) bool }); ok && r.ReplaceableBy(nil) {
		return pos
	}
	return pos.Side(face)
}

// noCollision ports Level.noCollision for a box: no block collision shape
// and no boat or shulker, the entities others collide with, intersect it.
func noCollision(tx *world.Tx, box cube.BBox, except *world.EntityHandle) bool {
	for x := int(math.Floor(box.Min()[0])); x < int(math.Ceil(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])) - 1; y < int(math.Ceil(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z < int(math.Ceil(box.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				b, ok := tx.BlockLoaded(pos)
				if !ok {
					return false
				}
				for _, bb := range b.Model().BBox(pos, tx) {
					if bb.Translate(pos.Vec3()).IntersectsWith(box) {
						return false
					}
				}
			}
		}
	}
	for e := range tx.EntitiesWithin(box) {
		if e.H() == except {
			continue
		}
		switch e.H().Type().EncodeEntity() {
		case "minecraft:boat", "minecraft:chest_boat", "minecraft:shulker":
			if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
				return false
			}
		}
	}
	return true
}

// containsAnyLiquid ports Level.containsAnyLiquid for a box.
func containsAnyLiquid(tx *world.Tx, box cube.BBox) bool {
	for x := int(math.Floor(box.Min()[0])); x < int(math.Ceil(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])); y < int(math.Ceil(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z < int(math.Ceil(box.Max()[2])); z++ {
				if _, ok := tx.Liquid(cube.Pos{x, y, z}); ok {
					return true
				}
			}
		}
	}
	return false
}

// entityBoxClear ports the check of Level.noCollision and an empty
// Level.getEntities for a box: no block collides with it and no entity is
// inside it.
func entityBoxClear(tx *world.Tx, box cube.BBox) bool {
	if !noCollision(tx, box, nil) {
		return false
	}
	for range tx.EntitiesWithin(box) {
		return false
	}
	return true
}

// povFluid is the ClipContext.Fluid mode of a view ray.
type povFluid uint8

const (
	// povNoFluid passes through liquids.
	povNoFluid povFluid = iota
	// povSourceFluid stops at liquid sources only.
	povSourceFluid
	// povAnyFluid stops at any liquid.
	povAnyFluid
)

// povHit ports Item.getPlayerPOVHitResult with ClipContext.Block.OUTLINE:
// the point where the user's view first meets a block outline or a liquid
// surface the fluid mode stops at, within its block interaction range of
// 4.5, or 5 in creative mode.
func povHit(tx *world.Tx, user User, fluids povFluid) (mgl64.Vec3, cube.Pos, cube.Face, bool) {
	reach := 4.5
	if g, ok := user.(interface{ GameMode() world.GameMode }); ok && g.GameMode().CreativeInventory() {
		reach = 5
	}
	eye := user.Position()
	if e, ok := user.(interface{ EyeHeight() float64 }); ok {
		eye[1] += e.EyeHeight()
	}
	end := eye.Add(user.Rotation().Vec3().Mul(reach))
	var (
		hit     mgl64.Vec3
		hitPos  cube.Pos
		hitFace cube.Face
		found   bool
	)
	trace.TraverseBlocks(eye, end, func(pos cube.Pos) bool {
		if r, ok := trace.BlockIntercept(pos, tx, tx.Block(pos), eye, end, trace.RayShape); ok {
			hit, hitPos, hitFace, found = r.Position(), pos, r.Face(), true
		}
		if l, ok := tx.Liquid(pos); ok && (fluids == povAnyFluid || fluids == povSourceFluid && l.LiquidDepth() == 8 && !l.LiquidFalling()) {
			height := float64(l.LiquidDepth()) / 9
			if above, ok := tx.Liquid(pos.Side(cube.FaceUp)); ok && above.LiquidType() == l.LiquidType() {
				height = 1
			} else if l.LiquidFalling() {
				height = 8.0 / 9
			}
			if r, ok := trace.BBoxIntercept(cube.Box(0, 0, 0, 1, height, 1).Translate(pos.Vec3()), eye, end); ok && (!found || r.Position().Sub(eye).Len() < hit.Sub(eye).Len()) {
				hit, hitPos, hitFace, found = r.Position(), pos, r.Face(), true
			}
		}
		return !found
	})
	return hit, hitPos, hitFace, found
}

// stackSpawnOpts applies what EntityType.createDefaultStackConfig takes from
// the stack an entity is made from: its custom name.
func stackSpawnOpts(user User, opts world.EntitySpawnOpts) world.EntitySpawnOpts {
	if held, _ := user.HeldItems(); held.CustomName() != "" {
		opts.NameTag = held.CustomName()
	}
	return opts
}

// wrapDegrees ports Mth.wrapDegrees for a float: the angle within
// [-180, 180).
func wrapDegrees(a float32) float32 {
	a = float32(math.Mod(float64(a), 360))
	if a >= 180 {
		a -= 360
	}
	if a < -180 {
		a += 360
	}
	return a
}

// LookAt ports Item.getPlayerPOVHitResult for blocks placed on what the
// user looks at: the block, or with sourceFluids the liquid source, the
// user's view first meets, and the face it meets.
func LookAt(tx *world.Tx, user User, sourceFluids bool) (cube.Pos, cube.Face, bool) {
	fluids := povNoFluid
	if sourceFluids {
		fluids = povSourceFluid
	}
	_, pos, face, ok := povHit(tx, user, fluids)
	return pos, face, ok
}
