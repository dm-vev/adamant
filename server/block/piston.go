package block

import (
	"maps"
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	pistonStateRetracted uint8 = iota
	pistonStateExtending
	pistonStateExtended
	pistonStateRetracting

	pistonMoveStep  float32 = 0.5
	pistonMoveLimit         = 12
)

// Piston is a block that can push blocks when powered.
type Piston struct {
	solid

	Facing cube.Face
	Sticky bool

	Moving       bool
	Powered      bool
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
	if placed(ctx) {
		tx.ScheduleBlockUpdate(pos, p, 0)
	}
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (p Piston) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	tx.Redstone().ScheduleUpdate(pos)
}

// ScheduledTick ...
func (p Piston) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	tx.Redstone().ScheduleUpdate(pos)
}

// RedstonePowerActionUpdate applies directional and quasi-connectivity rules after the update event is accepted.
func (p Piston) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, _ world.RedstoneUpdate) {
	p.handleState(pos, tx)
}

// Tick ...
func (p Piston) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if !p.Moving {
		return
	}
	if p.Extending {
		p.Progress = min(1, p.Progress+pistonMoveStep)
		p.LastProgress = min(1, p.LastProgress+pistonMoveStep)
	} else {
		p.Progress = max(0, p.Progress-pistonMoveStep)
		p.LastProgress = max(0, p.LastProgress-pistonMoveStep)
	}

	p.moveCollidedEntities(pos, tx)
	if p.Progress == p.LastProgress {
		p.finishMove(pos, tx)
		return
	}
	p.store(pos, tx)
}

func (p Piston) handleState(pos cube.Pos, tx *world.Tx) {
	powered := p.shouldExtend(pos, tx)
	if p.Moving {
		if p.Powered != powered {
			p.Powered = powered
			p.store(pos, tx)
		}
		return
	}

	if powered && !p.extended(pos, tx) {
		p.Powered = true
		if p.doMove(pos, tx, true) {
			return
		}
	}
	if !powered && p.extended(pos, tx) {
		p.Powered = false
		if p.doMove(pos, tx, false) {
			return
		}
	}
	if p.Powered != powered {
		p.Powered = powered
		p.store(pos, tx)
	}
}

func (p Piston) shouldExtend(pos cube.Pos, tx *world.Tx) bool {
	for _, face := range cube.Faces() {
		if face == p.Facing {
			continue
		}
		if tx.RedstonePowerFrom(pos, face) > 0 {
			return true
		}
	}
	above := pos.Side(cube.FaceUp)
	for _, face := range cube.Faces() {
		if face == cube.FaceDown {
			continue
		}
		if tx.RedstonePowerFrom(above, face) > 0 {
			return true
		}
	}
	return false
}

func (p Piston) extended(pos cube.Pos, tx *world.Tx) bool {
	front := tx.Block(pos.Side(p.Facing))
	if head, ok := front.(PistonHead); ok {
		return head.Facing == p.Facing && head.Sticky == p.Sticky
	}
	if moving, ok := front.(MovingBlock); ok {
		return moving.PistonPos == pos
	}
	return false
}

func (p Piston) doMove(pos cube.Pos, tx *world.Tx, extending bool) bool {
	calc := newPistonMoveCalculator(pos, p, tx, extending)
	canMove := calc.canMove()
	if !canMove && extending {
		return false
	}

	attached := make([]cube.Pos, 0)
	if canMove && (p.Sticky || extending) {
		for i := len(calc.toDestroy) - 1; i >= 0; i-- {
			destroyPos := calc.toDestroy[i]
			breakBlock(tx.Block(destroyPos), destroyPos, tx)
		}
		attached = append(attached, calc.toMove...)
		moveDir := p.Facing
		if !extending {
			moveDir = moveDir.Opposite()
		}
		if !p.spawnMovingBlocks(pos, calc.toMove, moveDir, tx) {
			return false
		}
	}

	if extending {
		tx.SetBlock(pos.Side(p.Facing), PistonHead{Facing: p.Facing, Sticky: p.Sticky}, nil)
	}

	p.startMove(extending, attached)
	p.moveCollidedEntities(pos, tx)
	if extending {
		tx.PlaySound(pos.Vec3Centre(), sound.PistonOut{})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.PistonIn{})
	}
	p.store(pos, tx)
	return true
}

func (p *Piston) startMove(extending bool, blocks []cube.Pos) {
	p.Moving = true
	p.Extending = extending
	p.Attached = append(p.Attached[:0], blocks...)
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

func (p *Piston) finishMove(pos cube.Pos, tx *world.Tx) {
	pushDir := p.Facing
	if p.Extending {
		p.State = pistonStateExtended
		p.NewState = pistonStateExtended
	} else {
		p.State = pistonStateRetracted
		p.NewState = pistonStateRetracted
		p.Extending = false
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
				movingEntity := maps.Clone(mb.MovingEntity)
				movingEntity["x"] = int32(movingPos.X())
				movingEntity["y"] = int32(movingPos.Y())
				movingEntity["z"] = int32(movingPos.Z())
				if decoded, ok := world.DecodeNBT(nbtBlock, movingEntity, tx.World().BlockRegistry()).(world.Block); ok {
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
	p.store(pos, tx)
	tx.ScheduleBlockUpdate(pos, *p, redstoneTicks(1))
}

func (p Piston) spawnMovingBlocks(pistonPos cube.Pos, blocks []cube.Pos, dir cube.Face, tx *world.Tx) bool {
	for i := len(blocks) - 1; i >= 0; i-- {
		from := blocks[i]
		to := from.Side(dir)
		b := tx.Block(from)
		movingEntity := encodeNestedBlockEntity(from, b)
		tx.SetBlock(to, MovingBlock{Moving: b, MovingEntity: movingEntity, PistonPos: pistonPos}, nil)
		tx.SetBlock(from, nil, nil)
	}
	return true
}

func (p Piston) moveCollidedEntities(pos cube.Pos, tx *world.Tx) {
	step := math.Abs(float64(p.Progress - p.LastProgress))
	if step == 0 {
		return
	}
	pushDir := p.Facing
	if !p.Extending {
		pushDir = pushDir.Opposite()
	}
	moved := make(map[*world.EntityHandle]struct{}, 8)
	for _, basePos := range p.Attached {
		movingPos := basePos.Side(pushDir)
		mb, ok := tx.Block(movingPos).(MovingBlock)
		if !ok {
			continue
		}
		for _, box := range movingBlockBoxes(mb, movingPos, p, pushDir, tx) {
			p.moveEntitiesInBox(box, pushDir, step, tx, moved)
		}
	}
	armBox := cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3()).Translate(faceOffset(pushDir, float64(p.Progress)))
	p.moveEntitiesInBox(armBox, pushDir, step, tx, moved)
}

func (p Piston) moveEntitiesInBox(box cube.BBox, dir cube.Face, distance float64, tx *world.Tx, moved map[*world.EntityHandle]struct{}) {
	search := box.Grow(1)
	for e := range tx.EntitiesWithin(search) {
		if _, ok := moved[e.H()]; ok {
			continue
		}
		if !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		if pushEntityByPiston(e, dir, distance) {
			moved[e.H()] = struct{}{}
		}
	}
}

func movingBlockBoxes(mb MovingBlock, pos cube.Pos, piston Piston, dir cube.Face, tx *world.Tx) []cube.BBox {
	if mb.Moving == nil {
		return nil
	}
	boxes := mb.Moving.Model().BBox(pos, tx)
	if len(boxes) == 0 {
		return nil
	}
	translated := make([]cube.BBox, 0, len(boxes))
	offset := faceOffset(dir, float64(piston.Progress)).Sub(faceOffset(dir, 1))
	for _, box := range boxes {
		translated = append(translated, box.Translate(pos.Vec3()).Translate(offset))
	}
	return translated
}

func pushEntityByPiston(e world.Entity, dir cube.Face, distance float64) bool {
	delta := faceOffset(dir, distance)
	if mover, ok := e.(interface {
		Move(deltaPos mgl64.Vec3, deltaYaw, deltaPitch float64)
	}); ok {
		mover.Move(delta, 0, 0)
		return true
	}
	if traveller, ok := e.(interface {
		Teleport(pos mgl64.Vec3)
	}); ok {
		traveller.Teleport(e.Position().Add(delta))
		return true
	}
	if mover, ok := e.(interface {
		SetVelocity(v mgl64.Vec3)
		Velocity() mgl64.Vec3
	}); ok {
		mover.SetVelocity(mover.Velocity().Add(delta))
		return true
	}
	return false
}

func faceOffset(face cube.Face, distance float64) mgl64.Vec3 {
	switch face {
	case cube.FaceDown:
		return mgl64.Vec3{0, -distance}
	case cube.FaceUp:
		return mgl64.Vec3{0, distance}
	case cube.FaceNorth:
		return mgl64.Vec3{0, 0, -distance}
	case cube.FaceSouth:
		return mgl64.Vec3{0, 0, distance}
	case cube.FaceWest:
		return mgl64.Vec3{-distance}
	case cube.FaceEast:
		return mgl64.Vec3{distance}
	default:
		return mgl64.Vec3{}
	}
}

func encodeNestedBlockEntity(pos cube.Pos, b world.Block) map[string]any {
	nbter, ok := b.(world.NBTer)
	if !ok {
		return nil
	}
	data := nbter.EncodeNBT()
	if data == nil {
		data = map[string]any{}
	} else {
		data = maps.Clone(data)
	}
	data["x"] = int32(pos.X())
	data["y"] = int32(pos.Y())
	data["z"] = int32(pos.Z())
	return data
}

func (p Piston) store(pos cube.Pos, tx *world.Tx) {
	tx.SetBlock(pos, p, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
}

// EncodeNBT ...
func (p Piston) EncodeNBT() map[string]any {
	attached := make([]int32, 0, len(p.Attached)*3)
	for _, pos := range p.Attached {
		attached = append(attached, int32(pos.X()), int32(pos.Y()), int32(pos.Z()))
	}
	return map[string]any{
		"id":             "PistonArm",
		"Progress":       p.Progress,
		"LastProgress":   p.LastProgress,
		"isMovable":      !p.Moving,
		"facing":         uint8(pistonFacingDirection(p.Facing)),
		"Extending":      boolByte(p.Extending),
		"powered":        boolByte(p.Powered),
		"AttachedBlocks": attached,
		"BreakBlocks":    []int32{},
		"Sticky":         boolByte(p.Sticky),
		"State":          p.State,
		"NewState":       p.NewState,
	}
}

// DecodeNBT ...
func (p Piston) DecodeNBT(data map[string]any) any {
	if _, ok := data["facing"]; ok {
		p.Facing = cube.Face(nbtconv.Uint8(data, "facing"))
	}
	if _, ok := data["Sticky"]; ok {
		p.Sticky = nbtconv.Bool(data, "Sticky")
	}
	p.Progress = nbtconv.Float32(data, "Progress")
	p.LastProgress = nbtconv.Float32(data, "LastProgress")
	p.Powered = nbtconv.Bool(data, "powered")
	p.State = nbtconv.Uint8(data, "State")
	p.NewState = nbtconv.Uint8(data, "NewState")
	p.Extending = nbtconv.Bool(data, "Extending")
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
		return "minecraft:sticky_piston", map[string]any{"facing_direction": pistonFacingDirection(p.Facing)}
	}
	return "minecraft:piston", map[string]any{"facing_direction": pistonFacingDirection(p.Facing)}
}

// PistonHead is the collision block placed in front of an extended piston.
type PistonHead struct {
	solid
	transparent

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
		return "minecraft:sticky_piston_arm_collision", map[string]any{"facing_direction": pistonFacingDirection(p.Facing)}
	}
	return "minecraft:piston_arm_collision", map[string]any{"facing_direction": pistonFacingDirection(p.Facing)}
}

func pistonFacingDirection(face cube.Face) int32 {
	switch face {
	case cube.FaceDown:
		return 0
	case cube.FaceUp:
		return 1
	case cube.FaceNorth:
		return 2
	case cube.FaceSouth:
		return 3
	case cube.FaceWest:
		return 4
	case cube.FaceEast:
		return 5
	default:
		return int32(face)
	}
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

func canPushBlock(pos cube.Pos, b world.Block, face cube.Face, destroyBlocks, extending bool, tx *world.Tx, pistonPos cube.Pos) bool {
	if pos.Y() < tx.Range()[0] || pos.Y() > tx.Range()[1] {
		return false
	}
	if face == cube.FaceDown && pos.Y() == tx.Range()[0] {
		return false
	}
	if face == cube.FaceUp && pos.Y() == tx.Range()[1] {
		return false
	}
	if pos == pistonPos {
		return false
	}
	if isEmptyOrReplaceable(tx, pos) {
		return true
	}
	if isPistonImmovable(b) {
		return false
	}
	if breaksWhenMoved(b) {
		return destroyBlocks || sticksToPiston(b)
	}
	return true
}

func breaksWhenMoved(b world.Block) bool {
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:redstone_wire",
		"minecraft:redstone_torch",
		"minecraft:unlit_redstone_torch",
		"minecraft:torch",
		"minecraft:soul_torch",
		"minecraft:lever",
		"minecraft:trip_wire",
		"minecraft:tripwire",
		"minecraft:tripwire_hook",
		"minecraft:ladder",
		"minecraft:vine",
		"minecraft:weeping_vines",
		"minecraft:twisting_vines",
		"minecraft:deadbush",
		"minecraft:short_grass",
		"minecraft:tallgrass",
		"minecraft:fern",
		"minecraft:double_plant",
		"minecraft:waterlily",
		"minecraft:banner",
		"minecraft:standing_banner",
		"minecraft:wall_banner",
		"minecraft:standing_sign",
		"minecraft:wall_sign",
		"minecraft:skull",
		"minecraft:item_frame",
		"minecraft:glow_frame",
		"minecraft:snow_layer",
		"minecraft:candle",
		"minecraft:flower_pot",
		"minecraft:rail",
		"minecraft:golden_rail",
		"minecraft:detector_rail",
		"minecraft:activator_rail":
		return true
	default:
		return false
	}
}

func sticksToPiston(b world.Block) bool {
	name, _ := b.EncodeBlock()
	switch name {
	case "minecraft:slime", "minecraft:slime_block", "minecraft:honey_block":
		return true
	default:
		return false
	}
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

type pistonMoveCalculator struct {
	pistonPos      cube.Pos
	armPos         cube.Pos
	blockToMove    cube.Pos
	hasBlockToMove bool
	moveDirection  cube.Face
	extending      bool
	sticky         bool
	tx             *world.Tx
	toMove         []cube.Pos
	toDestroy      []cube.Pos
}

func newPistonMoveCalculator(pos cube.Pos, piston Piston, tx *world.Tx, extending bool) pistonMoveCalculator {
	calc := pistonMoveCalculator{
		pistonPos: pos,
		extending: extending,
		sticky:    piston.Sticky,
		tx:        tx,
		toMove:    make([]cube.Pos, 0, pistonMoveLimit),
		toDestroy: make([]cube.Pos, 0, 1),
	}
	face := piston.Facing
	if extending {
		calc.moveDirection = face
		calc.blockToMove = pos.Side(face)
		calc.hasBlockToMove = true
		return calc
	}
	calc.armPos = pos.Side(face)
	calc.moveDirection = face.Opposite()
	if piston.Sticky {
		calc.blockToMove = stepPos(pos, face, 2)
		calc.hasBlockToMove = true
	}
	return calc
}

func (c *pistonMoveCalculator) canMove() bool {
	if !c.sticky && !c.extending {
		return true
	}
	c.toMove = c.toMove[:0]
	c.toDestroy = c.toDestroy[:0]
	if !c.hasBlockToMove {
		return true
	}

	block := c.tx.Block(c.blockToMove)
	if !canPushBlock(c.blockToMove, block, c.moveDirection, true, c.extending, c.tx, c.pistonPos) {
		return false
	}
	if breaksWhenMoved(block) {
		if c.extending || sticksToPiston(block) {
			c.toDestroy = append(c.toDestroy, c.blockToMove)
		}
		return true
	}
	if !c.addBlockLine(c.blockToMove, c.moveDirection) {
		return false
	}
	for _, pos := range append([]cube.Pos(nil), c.toMove...) {
		if sticksToPiston(c.tx.Block(pos)) && !c.addBranchingBlocks(pos) {
			return false
		}
	}
	return true
}

func (c *pistonMoveCalculator) addBlockLine(origin cube.Pos, _ cube.Face) bool {
	block := c.tx.Block(origin)
	if isEmptyOrReplaceable(c.tx, origin) {
		return true
	}
	if !canPushBlock(origin, block, c.moveDirection, false, c.extending, c.tx, c.pistonPos) {
		return true
	}
	if origin == c.pistonPos || containsPos(c.toMove, origin) {
		return true
	}
	if len(c.toMove) >= pistonMoveLimit {
		return false
	}
	c.toMove = append(c.toMove, origin)

	count := 1
	stuck := make([]cube.Pos, 0, 4)
	for sticksToPiston(block) {
		nextPos := stepPos(origin, c.moveDirection.Opposite(), count)
		next := c.tx.Block(nextPos)
		if isEmptyOrReplaceable(c.tx, nextPos) || !canPushBlock(nextPos, next, c.moveDirection, false, c.extending, c.tx, c.pistonPos) || nextPos == c.pistonPos {
			break
		}
		if breaksWhenMoved(next) {
			if sticksToPiston(next) {
				c.toDestroy = append(c.toDestroy, nextPos)
			}
			break
		}
		count++
		if count+len(c.toMove) > pistonMoveLimit {
			return false
		}
		stuck = append(stuck, nextPos)
	}
	if len(stuck) > 0 {
		for i := len(stuck) - 1; i >= 0; i-- {
			c.toMove = append(c.toMove, stuck[i])
		}
	}

	stuckCount := len(stuck)
	step := 1
	for {
		nextPos := stepPos(origin, c.moveDirection, step)
		if index := indexPos(c.toMove, nextPos); index > -1 {
			c.reorderListAtCollision(stuckCount, index)
			for i := 0; i <= index+stuckCount; i++ {
				if sticksToPiston(c.tx.Block(c.toMove[i])) && !c.addBranchingBlocks(c.toMove[i]) {
					return false
				}
			}
			return true
		}
		if isEmptyOrReplaceable(c.tx, nextPos) || nextPos == c.armPos {
			return true
		}
		next := c.tx.Block(nextPos)
		if !canPushBlock(nextPos, next, c.moveDirection, true, c.extending, c.tx, c.pistonPos) || nextPos == c.pistonPos {
			return false
		}
		if breaksWhenMoved(next) {
			c.toDestroy = append(c.toDestroy, nextPos)
			return true
		}
		if len(c.toMove) >= pistonMoveLimit {
			return false
		}
		c.toMove = append(c.toMove, nextPos)
		stuckCount++
		step++
	}
}

func (c *pistonMoveCalculator) reorderListAtCollision(count, index int) {
	list := append([]cube.Pos(nil), c.toMove[:index]...)
	list1 := append([]cube.Pos(nil), c.toMove[len(c.toMove)-count:]...)
	list2 := append([]cube.Pos(nil), c.toMove[index:len(c.toMove)-count]...)
	c.toMove = append(list, list1...)
	c.toMove = append(c.toMove, list2...)
}

func (c *pistonMoveCalculator) addBranchingBlocks(pos cube.Pos) bool {
	for _, face := range cube.Faces() {
		if face.Axis() == c.moveDirection.Axis() {
			continue
		}
		if !c.addBlockLine(pos.Side(face), face) {
			return false
		}
	}
	return true
}

func stepPos(pos cube.Pos, face cube.Face, count int) cube.Pos {
	for i := 0; i < count; i++ {
		pos = pos.Side(face)
	}
	return pos
}

func containsPos(positions []cube.Pos, pos cube.Pos) bool {
	return indexPos(positions, pos) != -1
}

func indexPos(positions []cube.Pos, pos cube.Pos) int {
	for i, current := range positions {
		if current == pos {
			return i
		}
	}
	return -1
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
