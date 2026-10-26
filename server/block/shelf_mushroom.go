package block

import (
	"math"
	"sort"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ShelfMushroom is a bouncy mushroom growing out of the side of a block.
type ShelfMushroom struct {
	transparent

	// Facing is the direction the mushroom grows towards, away from its
	// support.
	Facing cube.Direction
	// Grown is set for the larger stage reached with bone meal.
	Grown bool
}

// Model ...
func (m ShelfMushroom) Model() world.BlockModel { return shelfMushroomModel{m} }

// BreakInfo ports the shelf mushroom loot table: one mushroom, two when grown.
func (m ShelfMushroom) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, func(item.Tool, []item.Enchantment) []item.Stack {
		count := 1
		if m.Grown {
			count = 2
		}
		return []item.Stack{item.NewStack(ShelfMushroom{}, count)}
	})
}

// UseOnBlock ports ShelfMushroomBlock.getStateForPlacement: the mushroom grows
// away from the first horizontal block it can attach to, in order of the
// directions the user looks.
func (m ShelfMushroom) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, m)
	if !used {
		return false
	}
	for _, d := range horizontalLookOrder(user.Rotation()) {
		if m.Facing = d.Opposite(); m.supported(pos, tx) {
			place(tx, pos, m, user, ctx)
			return placed(ctx)
		}
	}
	return false
}

// horizontalLookOrder sorts the horizontal directions by how closely the
// rotation looks along them, as Direction.orderedByNearest does.
func horizontalLookOrder(r cube.Rotation) []cube.Direction {
	yaw := mgl64.DegToRad(r.Yaw())
	look := mgl64.Vec3{-math.Sin(yaw), 0, math.Cos(yaw)}
	dirs := append([]cube.Direction(nil), cube.Directions()...)
	along := func(d cube.Direction) float64 { return look.Dot(cube.Pos{}.Side(d.Face()).Vec3()) }
	sort.SliceStable(dirs, func(i, j int) bool { return along(dirs[i]) > along(dirs[j]) })
	return dirs
}

// supported ports ShelfMushroomBlock.canSurvive.
func (m ShelfMushroom) supported(pos cube.Pos, tx *world.Tx) bool {
	support := pos.Side(m.Facing.Opposite().Face())
	return tx.Block(support).Model().FaceSolid(support, m.Facing.Face(), tx)
}

// NeighbourUpdateTick breaks the mushroom without its support.
func (m ShelfMushroom) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !m.supported(pos, tx) {
		breakBlock(m, pos, tx)
	}
}

// BoneMeal grows the mushroom once.
func (m ShelfMushroom) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if m.Grown {
		return item.BoneMealResultNone
	}
	m.Grown = true
	tx.SetBlock(pos, m, &world.SetOpts{DisableBlockUpdates: true})
	return item.BoneMealResultSmall
}

// EntityLand ports the shelf mushroom's fall distance reduction of a half and
// bounce restitution of 0.75: entities that do not sneak bounce back up.
func (m ShelfMushroom) EntityLand(pos cube.Pos, tx *world.Tx, e world.Entity, distance *float64) {
	*distance *= 0.5
	if s, ok := e.(interface{ Sneaking() bool }); ok && s.Sneaking() {
		return
	}
	v, ok := e.(velocityEntity)
	if !ok || v.Velocity()[1] >= 0 {
		return
	}
	vel := v.Velocity()
	vel[1] = -vel[1] * 0.75
	if _, living := e.(livingEntity); !living {
		vel[1] *= 0.8
	}
	v.SetVelocity(vel)
	if e.H().Type().EncodeEntity() != "minecraft:item" && e.H().Type().EncodeEntity() != "minecraft:tnt" {
		tx.PlaySound(pos.Vec3Centre(), sound.BlockBounce{Block: m})
	}
}

// EncodeItem ...
func (ShelfMushroom) EncodeItem() (name string, meta int16) {
	return "minecraft:shelf_mushroom", 0
}

// EncodeBlock ...
func (m ShelfMushroom) EncodeBlock() (string, map[string]any) {
	return "minecraft:shelf_mushroom", map[string]any{"minecraft:cardinal_direction": m.Facing.String(), "growth": int32(boolByte(m.Grown))}
}

// allShelfMushrooms returns every shelf mushroom state.
func allShelfMushrooms() (b []world.Block) {
	for _, d := range cube.Directions() {
		b = append(b, ShelfMushroom{Facing: d}, ShelfMushroom{Facing: d, Grown: true})
	}
	return
}

// shelfMushroomModel ports ShelfMushroomBlock's two-layer cap shapes.
type shelfMushroomModel struct{ m ShelfMushroom }

// BBox returns the cap and stalk, rotated from their north-facing layout
// against the south side of the block.
func (s shelfMushroomModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	// Pixel boxes for a north-facing mushroom: x0, y0, z0, x1, y1, z1.
	boxes := [][6]float64{{3, 9, 9, 13, 11, 16}, {5, 8, 12, 11, 9, 16}}
	if s.m.Grown {
		boxes = [][6]float64{{1, 8, 6, 15, 11, 16}, {4, 6, 10, 12, 8, 16}}
	}
	out := make([]cube.BBox, 0, len(boxes))
	for _, b := range boxes {
		x0, z0, x1, z1 := rotateNorthPixels(b[0], b[2], b[3], b[5], s.m.Facing)
		out = append(out, cube.Box(x0/16, b[1]/16, z0/16, x1/16, b[4]/16, z1/16))
	}
	return out
}

// FaceSolid ...
func (shelfMushroomModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }

// rotateNorthPixels rotates a horizontal pixel rectangle laid out for north
// to the direction passed, clockwise as Shapes.rotateHorizontal does.
func rotateNorthPixels(x0, z0, x1, z1 float64, d cube.Direction) (float64, float64, float64, float64) {
	switch d {
	case cube.East:
		return 16 - z1, x0, 16 - z0, x1
	case cube.South:
		return 16 - x1, 16 - z1, 16 - x0, 16 - z0
	case cube.West:
		return z0, 16 - x1, z1, 16 - x0
	}
	return x0, z0, x1, z1
}
