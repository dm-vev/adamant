package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Piston is a block that can push blocks when powered.
type Piston struct {
	solid

	Facing cube.Face
	Sticky bool
}

// BreakInfo ...
func (p Piston) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(p))
}

// UseOnBlock ...
func (p Piston) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, p)
	if !used {
		return false
	}
	p.Facing = calculateFace(user, pos)
	place(tx, pos, p, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (p Piston) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	shouldExtend := p.shouldExtend(pos, tx)
	if shouldExtend && !p.extended(pos, tx) {
		p.extend(pos, tx)
		return
	}
	if !shouldExtend && p.extended(pos, tx) {
		p.retract(pos, tx)
	}
}

func (p Piston) shouldExtend(pos cube.Pos, tx *world.Tx) bool {
	for _, face := range cube.Faces() {
		if world.RedstonePowerFromSide(tx, pos, face) > 0 {
			return true
		}
	}
	return false
}

func (p Piston) extended(pos cube.Pos, tx *world.Tx) bool {
	_, ok := tx.Block(pos.Side(p.Facing)).(PistonHead)
	return ok
}

func (p Piston) extend(pos cube.Pos, tx *world.Tx) {
	dir := p.Facing
	headPos := pos.Side(dir)
	blocks := make([]cube.Pos, 0, 12)
	cur := headPos

	for i := 0; i < 12; i++ {
		if isEmptyOrReplaceable(tx, cur) {
			break
		}
		if isPistonImmovable(tx.Block(cur)) {
			return
		}
		blocks = append(blocks, cur)
		cur = cur.Side(dir)
	}

	if !isEmptyOrReplaceable(tx, cur) {
		return
	}

	for i := len(blocks) - 1; i >= 0; i-- {
		from := blocks[i]
		to := from.Side(dir)
		b := tx.Block(from)
		tx.SetBlock(to, b, nil)
		tx.SetBlock(from, nil, nil)
	}

	tx.SetBlock(headPos, PistonHead{Facing: dir, Sticky: p.Sticky}, nil)
}

func (p Piston) retract(pos cube.Pos, tx *world.Tx) {
	dir := p.Facing
	headPos := pos.Side(dir)
	if _, ok := tx.Block(headPos).(PistonHead); ok {
		tx.SetBlock(headPos, nil, nil)
	}
	if !p.Sticky {
		return
	}
	pullPos := headPos.Side(dir)
	if isEmptyOrReplaceable(tx, pullPos) {
		return
	}
	if isPistonImmovable(tx.Block(pullPos)) {
		return
	}
	b := tx.Block(pullPos)
	tx.SetBlock(headPos, b, nil)
	tx.SetBlock(pullPos, nil, nil)
}

// EncodeItem ...
func (p Piston) EncodeItem() (name string, meta int16) {
	if p.Sticky {
		return "minecraft:sticky_piston", 0
	}
	return "minecraft:piston", 0
}

// EncodeBlock ...
func (p Piston) EncodeBlock() (string, map[string]any) {
	if p.Sticky {
		return "minecraft:sticky_piston", map[string]any{"facing_direction": int32(p.Facing)}
	}
	return "minecraft:piston", map[string]any{"facing_direction": int32(p.Facing)}
}

// PistonHead is the collision block placed in front of an extended piston.
type PistonHead struct {
	solid

	Facing cube.Face
	Sticky bool
}

// BreakInfo ...
func (p PistonHead) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, simpleDrops())
}

// NeighbourUpdateTick ...
func (p PistonHead) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	back := pos.Side(p.Facing.Opposite())
	base, ok := tx.Block(back).(Piston)
	if !ok || base.Facing != p.Facing || base.Sticky != p.Sticky {
		tx.SetBlock(pos, nil, nil)
	}
}

// EncodeBlock ...
func (p PistonHead) EncodeBlock() (string, map[string]any) {
	if p.Sticky {
		return "minecraft:sticky_piston_arm_collision", map[string]any{"facing_direction": int32(p.Facing)}
	}
	return "minecraft:piston_arm_collision", map[string]any{"facing_direction": int32(p.Facing)}
}

func isEmptyOrReplaceable(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	if _, ok := b.(Air); ok {
		return true
	}
	if r, ok := b.(Replaceable); ok {
		return r.ReplaceableBy(Air{})
	}
	return false
}

func isPistonImmovable(b world.Block) bool {
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:bedrock",
		"minecraft:obsidian",
		"minecraft:crying_obsidian",
		"minecraft:reinforced_deepslate",
		"minecraft:end_portal",
		"minecraft:end_gateway",
		"minecraft:end_portal_frame",
		"minecraft:nether_portal",
		"minecraft:moving_block",
		"minecraft:piston_arm_collision",
		"minecraft:sticky_piston_arm_collision":
		return true
	default:
		return false
	}
}

func allPistons() (pistons []world.Block) {
	for _, face := range cube.Faces() {
		pistons = append(pistons, Piston{Facing: face, Sticky: false})
		pistons = append(pistons, Piston{Facing: face, Sticky: true})
	}
	return
}

func allPistonHeads() (heads []world.Block) {
	for _, face := range cube.Faces() {
		heads = append(heads, PistonHead{Facing: face, Sticky: false})
		heads = append(heads, PistonHead{Facing: face, Sticky: true})
	}
	return
}
