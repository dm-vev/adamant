package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math/rand/v2"
)

const (
	pistonStateRetracted uint8 = iota
	pistonStateExtending
	pistonStateExtended
	pistonStateRetracting

	pistonMoveStep float32 = 0.5
)

// Piston is a block that can push blocks when powered.
type Piston struct {
	solid

	Facing cube.Face
	Sticky bool

	Moving       bool
	Extending    bool
	Progress     float32
	LastProgress float32
	State        uint8
	NewState     uint8
	Attached     []cube.Pos
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
	if p.Moving {
		return
	}
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
		if face == p.Facing {
			continue
		}
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
	blocks, ok := p.collectMoveBlocks(headPos, dir, tx)
	if !ok {
		return
	}
	if !p.spawnMovingBlocks(pos, blocks, dir, tx) {
		return
	}
	tx.SetBlock(headPos, PistonHead{Facing: dir, Sticky: p.Sticky}, nil)
	p.startMove(true, blocks)
	tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
}

func (p Piston) retract(pos cube.Pos, tx *world.Tx) {
	dir := p.Facing
	headPos := pos.Side(dir)
	blocks := make([]cube.Pos, 0, 1)
	if p.Sticky {
		pullPos := headPos.Side(dir)
		if !isEmptyOrReplaceable(tx, pullPos) && !isPistonImmovable(tx.Block(pullPos)) {
			blocks = append(blocks, pullPos)
		}
	}
	if !p.spawnMovingBlocks(pos, blocks, dir.Opposite(), tx) {
		return
	}
	p.startMove(false, blocks)
	tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
}

// Tick ...
func (p Piston) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	if !p.Moving {
		return
	}
	if p.Extending {
		p.Progress += pistonMoveStep
		if p.Progress > 1 {
			p.Progress = 1
		}
		p.LastProgress += pistonMoveStep
		if p.LastProgress > 1 {
			p.LastProgress = 1
		}
	} else {
		p.Progress -= pistonMoveStep
		if p.Progress < 0 {
			p.Progress = 0
		}
		p.LastProgress -= pistonMoveStep
		if p.LastProgress < 0 {
			p.LastProgress = 0
		}
	}

	if p.Progress == p.LastProgress {
		p.finishMove(pos, tx)
		return
	}
	tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
}

// ScheduledTick ...
func (p Piston) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if p.Moving {
		return
	}
	shouldExtend := p.shouldExtend(pos, tx)
	if shouldExtend && !p.extended(pos, tx) {
		p.extend(pos, tx)
		return
	}
	if !shouldExtend && p.extended(pos, tx) {
		p.retract(pos, tx)
	}
}

// EncodeNBT ...
func (p Piston) EncodeNBT() map[string]any {
	attached := make([]int32, 0, len(p.Attached)*3)
	for _, pos := range p.Attached {
		attached = append(attached, int32(pos.X()), int32(pos.Y()), int32(pos.Z()))
	}
	return map[string]any{
		"id":             "PistonArm",
		"Progress":       float32(p.Progress),
		"LastProgress":   float32(p.LastProgress),
		"isMovable":      !p.Moving,
		"AttachedBlocks": attached,
		"BreakBlocks":    []int32{},
		"Sticky":         p.Sticky,
		"State":          byte(p.State),
		"NewState":       byte(p.NewState),
	}
}

// DecodeNBT ...
func (p Piston) DecodeNBT(data map[string]any) any {
	p.Progress = nbtconv.Float32(data, "Progress")
	p.LastProgress = nbtconv.Float32(data, "LastProgress")
	p.State = nbtconv.Uint8(data, "State")
	p.NewState = nbtconv.Uint8(data, "NewState")
	p.Extending = p.State == pistonStateExtending || p.State == pistonStateExtended
	p.Moving = p.State == pistonStateExtending || p.State == pistonStateRetracting
	p.Attached = decodePistonAttachedBlocks(data)
	return p
}

func decodePistonAttachedBlocks(data map[string]any) []cube.Pos {
	raw, ok := data["AttachedBlocks"]
	if !ok {
		return nil
	}
	coords := make([]int32, 0)
	switch v := raw.(type) {
	case []int32:
		coords = append(coords, v...)
	case []any:
		for _, val := range v {
			switch n := val.(type) {
			case int32:
				coords = append(coords, n)
			case int64:
				coords = append(coords, int32(n))
			case int:
				coords = append(coords, int32(n))
			}
		}
	}
	if len(coords) < 3 {
		return nil
	}
	positions := make([]cube.Pos, 0, len(coords)/3)
	for i := 0; i+2 < len(coords); i += 3 {
		positions = append(positions, cube.Pos{int(coords[i]), int(coords[i+1]), int(coords[i+2])})
	}
	return positions
}

func (p *Piston) startMove(extending bool, blocks []cube.Pos) {
	p.Moving = true
	p.Extending = extending
	p.Attached = blocks
	if extending {
		p.State = pistonStateExtending
		p.NewState = pistonStateExtending
		p.Progress = 0
		p.LastProgress = -pistonMoveStep
		return
	}
	p.State = pistonStateRetracting
	p.NewState = pistonStateRetracting
	p.Progress = 1
	p.LastProgress = 1 + pistonMoveStep
}

func (p Piston) collectMoveBlocks(headPos cube.Pos, dir cube.Face, tx *world.Tx) ([]cube.Pos, bool) {
	blocks := make([]cube.Pos, 0, 12)
	cur := headPos

	for i := 0; i < 12; i++ {
		if isEmptyOrReplaceable(tx, cur) {
			return blocks, true
		}
		if isPistonImmovable(tx.Block(cur)) {
			return nil, false
		}
		blocks = append(blocks, cur)
		cur = cur.Side(dir)
	}

	if !isEmptyOrReplaceable(tx, cur) {
		return nil, false
	}
	return blocks, true
}

func (p Piston) spawnMovingBlocks(pistonPos cube.Pos, blocks []cube.Pos, dir cube.Face, tx *world.Tx) bool {
	if len(blocks) == 0 {
		return true
	}
	for i := len(blocks) - 1; i >= 0; i-- {
		from := blocks[i]
		to := from.Side(dir)
		b := tx.Block(from)
		var movingEntity map[string]any
		if nbt, ok := b.(world.NBTer); ok {
			movingEntity = nbt.EncodeNBT()
		}
		tx.SetBlock(to, MovingBlock{Moving: b, MovingEntity: movingEntity, PistonPos: pistonPos}, nil)
		tx.SetBlock(from, nil, nil)
	}
	return true
}

func (p *Piston) finishMove(pos cube.Pos, tx *world.Tx) {
	if p.Extending {
		p.State = pistonStateExtended
		p.NewState = pistonStateExtended
	} else {
		p.State = pistonStateRetracted
		p.NewState = pistonStateRetracted
	}

	pushDir := p.Facing
	if !p.Extending {
		pushDir = p.Facing.Opposite()
	}

	for _, basePos := range p.Attached {
		movingPos := basePos.Side(pushDir)
		mb, ok := tx.Block(movingPos).(MovingBlock)
		if !ok {
			continue
		}
		moved := mb.Moving
		if moved == nil {
			tx.SetBlock(movingPos, nil, nil)
			continue
		}
		if mb.MovingEntity != nil {
			if nbtBlock, ok := moved.(world.NBTer); ok {
				if decoded, ok := nbtBlock.DecodeNBT(mb.MovingEntity).(world.Block); ok {
					moved = decoded
				}
			}
		}
		tx.SetBlock(movingPos, moved, nil)
	}

	if !p.Extending {
		headPos := pos.Side(p.Facing)
		if _, ok := tx.Block(headPos).(PistonHead); ok {
			tx.SetBlock(headPos, nil, nil)
		}
	}

	p.Moving = false
	p.Attached = nil
	tx.SetBlock(pos, *p, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
	tx.ScheduleBlockUpdate(pos, *p, redstoneTicks(1))
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

// RedstoneConnectsTo ...
func (Piston) RedstoneConnectsTo(cube.Face) bool {
	return true
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
