package block

import (
	"maps"
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
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

	// The circuit-to-actor handoff is transient. NewState in the native actor
	// stays unchanged until movement starts or an attempted extension is blocked.
	requestedState uint8
	hasRequest     bool
	previousState  uint8

	// legacyFacing is set on pistons saved with the facing field earlier
	// versions wrote, when horizontal heads were encoded the other way
	// round: their extended head loads turned back towards the piston.
	legacyFacing bool
}

// BreakInfo ...
func (p Piston) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(p))
}

// Removed completes carried blocks inside the replacement transaction, as
// native PistonBlockActor::onRemoved does on the server. The detached head is
// left to its neighbour update. A live piston permutation retains its actor.
func (p Piston) Removed(_ cube.Pos, tx *world.Tx, replacement world.Block) {
	if _, same := replacement.(Piston); same {
		return
	}
	direction := p.Facing
	if p.NewState == pistonStateRetracted || p.NewState == pistonStateRetracting {
		direction = direction.Opposite()
	}
	p.restoreMovingBlocks(direction, tx)
}

// UseOnBlock ...
func (p Piston) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, p)
	if !used {
		return false
	}
	p.Facing = placerFacing(user, pos)
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

// NeighbourChanged samples head survival at notification time. Native queues
// a one-GT check only if the changed front cell fails canSurvive at that instant.
func (p Piston) NeighbourChanged(pos, changedNeighbour cube.Pos, tx *world.Tx) {
	if changedNeighbour == pos.Side(p.Facing) && !p.canSurvive(pos, tx) {
		tx.ScheduleBlockUpdate(pos, p, gameTicks(1))
	}
	tx.Redstone().ScheduleUpdate(pos)
}

func (p Piston) canSurvive(pos cube.Pos, tx *world.Tx) bool {
	if head, ok := tx.Block(pos.Side(p.Facing)).(PistonHead); ok && head.Sticky == p.Sticky {
		return true
	}
	// The native zero-fraction progress query observes LastProgress in
	// binary32. Retraction and the initial extension step tolerate no head.
	progress := p.LastProgress + (p.Progress-p.LastProgress)*float32(0)
	return p.State == pistonStateRetracting || !(progress > float32(0.0001))
}

// ScheduledTick ...
func (p Piston) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !p.canSurvive(pos, tx) {
		breakBlock(p, pos, tx)
		return
	}
	tx.Redstone().ScheduleUpdate(pos)
}

// RedstonePowerActionUpdate leaves movement to the actor reading PistonConsumer.
func (Piston) RedstonePowerActionUpdate(cube.Pos, *world.Tx, world.RedstoneUpdate) {}

// Tick reads the native consumer on the actor clock, requests a transition,
// then starts it in the following actor pass. Progress advances in the starting
// pass too; the move finishes after the previous progress reaches its endpoint.
func (p Piston) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if p.legacyFacing {
		p.legacyFacing = false
		front := pos.Side(p.Facing)
		if head, ok := tx.Block(front).(PistonHead); ok && p.Facing.Axis() != cube.Y && head.Facing == p.Facing.Opposite() {
			tx.SetBlock(front, PistonHead{Facing: p.Facing, Sticky: head.Sticky}, nil)
		}
		tx.SetBlockEntity(pos, p)
	}
	if p.previousState != p.State {
		switch p.State {
		case pistonStateExtending:
			tx.PlaySound(pos.Vec3(), sound.PistonOut{})
			tx.EmitGameEvent(world.GameEventPistonExtend, pos.Vec3Centre(), world.GameEventContext{})
		case pistonStateRetracting:
			tx.PlaySound(pos.Vec3(), sound.PistonIn{})
			tx.EmitGameEvent(world.GameEventPistonContract, pos.Vec3Centre(), world.GameEventContext{})
		}
		p.previousState = p.State
		tx.SetBlockEntity(pos, p)
	}
	if !p.Moving {
		previousPower := p.Powered
		power, _ := tx.Redstone().ConsumerPower(pos)
		p.Powered = power != 0
		if !p.hasRequest && p.NewState != p.State {
			p.requestedState, p.hasRequest = p.NewState, true
		}
		if !p.hasRequest {
			if p.Powered && p.State == pistonStateRetracted {
				p.requestedState, p.hasRequest = pistonStateExtending, true
			} else if !p.Powered && p.State == pistonStateExtended {
				p.requestedState, p.hasRequest = pistonStateRetracting, true
			}
			if p.Powered != previousPower || p.hasRequest {
				tx.SetBlockEntity(pos, p)
			}
			return
		}
		// An extension request can disappear before the following actor pass.
		if p.requestedState == pistonStateExtending && !p.Powered {
			p.NewState = p.State
			p.hasRequest = false
			tx.SetBlockEntity(pos, p)
			return
		}
		if !p.doMove(pos, tx, p.requestedState == pistonStateExtending) {
			p.NewState = p.requestedState
			// A blocked request is saved actor state, not a client animation
			// update. Native sends no repeated PistonArm packets here.
			tx.SetBlockEntity(pos, p)
			return
		}
		p = tx.Block(pos).(Piston)
	}
	p.LastProgress = p.Progress
	if p.Extending && p.LastProgress < 1 {
		p.Progress = min(1, p.Progress+pistonMoveStep)
	} else if !p.Extending && p.LastProgress > 0 {
		p.Progress = max(0, p.Progress-pistonMoveStep)
	}

	p.moveCollidedEntities(pos, tx)
	if p.Progress == p.LastProgress {
		p.finishMove(pos, tx)
		return
	}
	tx.SetBlockEntity(pos, p)
}

func (p Piston) shouldExtend(pos cube.Pos, tx *world.Tx) bool {
	return p.inputPower(pos, tx) > 0
}

func (p Piston) inputPower(pos cube.Pos, tx *world.Tx) int {
	power := 0
	for _, face := range cube.Faces() {
		if face == p.Facing {
			continue
		}
		power = max(power, tx.RedstonePowerFrom(pos, face))
	}
	return power
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
		sortPistonBlocks(attached, moveDir)
		if !p.spawnMovingBlocks(pos, attached, moveDir, extending, tx) {
			return false
		}
	}

	if extending {
		tx.SetBlock(pos.Side(p.Facing), PistonHead{Facing: p.Facing, Sticky: p.Sticky}, nil)
	} else {
		headPos := pos.Side(p.Facing)
		if moving, ok := tx.Block(headPos).(MovingBlock); !ok || moving.PistonPos != pos {
			tx.SetBlock(headPos, Air{}, nil)
		}
	}

	p.startMove(extending, attached)
	p.moveCollidedEntities(pos, tx)
	p.store(pos, tx)
	return true
}

func (p *Piston) startMove(extending bool, blocks []cube.Pos) {
	p.hasRequest = false
	p.Moving = true
	p.Extending = extending
	p.Attached = append(p.Attached[:0], blocks...)
	if extending {
		p.State = pistonStateExtending
		p.NewState = pistonStateExtending
		p.Progress, p.LastProgress = 0, 0
		return
	}
	p.State = pistonStateRetracting
	p.NewState = pistonStateRetracting
	p.Progress, p.LastProgress = 1, 1
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

	p.restoreMovingBlocks(pushDir, tx)

	if !p.Extending {
		headPos := pos.Side(p.Facing)
		if _, ok := tx.Block(headPos).(PistonHead); ok {
			tx.SetBlock(headPos, nil, nil)
		}
	}

	p.Moving = false
	p.Attached = nil
	p.store(pos, tx)
}

func (p Piston) restoreMovingBlocks(direction cube.Face, tx *world.Tx) {
	attached := append([]cube.Pos(nil), p.Attached...)
	sortPistonBlocks(attached, direction)
	for _, basePos := range attached {
		movingPos := basePos.Side(direction)
		if mb, ok := tx.Block(movingPos).(MovingBlock); ok {
			mb.restore(movingPos, tx)
		}
	}
}

func (p Piston) spawnMovingBlocks(pistonPos cube.Pos, blocks []cube.Pos, dir cube.Face, extending bool, tx *world.Tx) bool {
	// Remember the original destination cells before sources are cleared. BDS
	// retains a second actor notification for a destination that was occupied.
	occupied := make([]bool, len(blocks))
	for i, from := range blocks {
		_, air := tx.Block(from.Side(dir)).(Air)
		occupied[i] = !air
	}
	for i, from := range blocks {
		to := from.Side(dir)
		b := tx.Block(from)
		movingEntity := encodeNestedBlockEntity(from, b)
		mb := MovingBlock{Moving: b, MovingEntity: movingEntity, PistonPos: pistonPos, Expanding: extending, repeatUpdate: occupied[i]}
		tx.SetBlock(to, mb, nil)
		// Native _spawnMovingBlock invokes the new moving actor's contact
		// callback before returning, separately from the following actor step.
		motion := p
		motion.startMove(extending, blocks)
		motion.moveMovingBlockEntities(mb, to, dir, tx)
		tx.SetBlock(from, nil, nil)
	}
	return true
}

// Native _spawnMovingBlocks stably sorts AttachedBlocks by their projection on
// the movement direction before spawning. The twelve-block limit selects its
// insertion-sort path. Compare coordinate differences as native signed int32.
func sortPistonBlocks(blocks []cube.Pos, direction cube.Face) {
	delta := (cube.Pos{}).Side(direction)
	before := func(a, b cube.Pos) bool {
		var dot float32
		for axis := range 3 {
			dot += float32(int32(a[axis])-int32(b[axis])) * float32(delta[axis])
		}
		return dot > 0
	}
	for i := 1; i < len(blocks); i++ {
		value, j := blocks[i], i
		for j > 0 && before(value, blocks[j-1]) {
			blocks[j] = blocks[j-1]
			j--
		}
		blocks[j] = value
	}
}

func (p Piston) moveCollidedEntities(pos cube.Pos, tx *world.Tx) {
	step := math.Abs(float64(p.Progress - p.LastProgress))
	if step == 0 {
		return
	}
	// Native normal-piston retraction does not move collided entities.
	if !p.Extending && !p.Sticky {
		return
	}
	pushDir := p.Facing
	if !p.Extending {
		pushDir = pushDir.Opposite()
	}
	for _, basePos := range p.Attached {
		movingPos := basePos.Side(pushDir)
		mb, ok := tx.Block(movingPos).(MovingBlock)
		if !ok {
			continue
		}
		p.moveMovingBlockEntities(mb, movingPos, pushDir, tx)
	}
	// The head follows the facing axis even when the push direction reverses.
	armBox := cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3()).Translate(faceOffset(p.Facing, float64(p.Progress)))
	p.moveEntitiesInBox(armBox, p.Facing, tx, nil, p.movementSource(pos, tx))
}

func (p Piston) moveMovingBlockEntities(mb MovingBlock, pos cube.Pos, dir cube.Face, tx *world.Tx) {
	var carried map[*world.EntityHandle]struct{}
	source := p.movementSource(mb.PistonPos, tx)
	if dir.Axis() != cube.Y {
		old := p
		old.Progress = old.LastProgress
		for _, box := range movingBlockBoxes(mb, pos, old, dir, tx) {
			lo, hi := box.Min(), box.Max()
			lo[1], hi[1] = hi[1], float64(float32(hi[1])+float32(.05))
			top := cube.Box(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2])
			for e := range tx.EntitiesWithin(top.Grow(1)) {
				body := e.H().Type().BBox(e).Translate(e.Position())
				point := e.Position()
				if !body.IntersectsWith(top) || point[0] < lo[0] || point[0] > hi[0] || point[2] < lo[2] || point[2] > hi[2] {
					continue
				}
				if pushEntityByPiston(e, dir, .5, source) {
					if carried == nil {
						carried = make(map[*world.EntityHandle]struct{})
					}
					carried[e.H()] = struct{}{}
				}
			}
		}
	}
	for _, box := range movingBlockBoxes(mb, pos, p, dir, tx) {
		p.moveEntitiesInBox(box, dir, tx, carried, source)
	}
}

func (p Piston) moveEntitiesInBox(box cube.BBox, dir cube.Face, tx *world.Tx, excluded map[*world.EntityHandle]struct{}, source world.BlockSource) {
	search := box.Grow(1)
	for e := range tx.EntitiesWithin(search) {
		if _, carried := excluded[e.H()]; carried {
			continue
		}
		body := e.H().Type().BBox(e).Translate(e.Position())
		if !body.IntersectsWith(box) {
			continue
		}
		axis := 0
		switch dir.Axis() {
		case cube.Y:
			axis = 1
		case cube.Z:
			axis = 2
		}
		direction := faceOffset(dir, 1)
		var distance float32
		if direction[axis] > 0 {
			distance = float32(box.Max()[axis]) - float32(body.Min()[axis])
		} else {
			distance = float32(body.Max()[axis]) - float32(box.Min()[axis])
		}
		if distance > 0 {
			pushEntityByPiston(e, dir, float64(distance), source)
		}
	}
}

func movingBlockBoxes(mb MovingBlock, pos cube.Pos, piston Piston, _ cube.Face, tx *world.Tx) []cube.BBox {
	return movingBlockContactBoxes(mb, pos, &piston, tx)
}

func movingBlockContactBoxes(mb MovingBlock, pos cube.Pos, piston *Piston, source world.BlockSource) []cube.BBox {
	_, nested := mb.Moving.(MovingBlock)
	if mb.Moving == nil || nested {
		return []cube.BBox{pistonCollisionBox(cube.Box(0, 0, 0, 1, 1, 1), pos)}
	}
	// Native asks the wrapped block for its world collision AABB at floored
	// positions for LastProgress and Progress, then encloses both shapes. It
	// does not continuously translate the shape by fractional progress.
	var bounds cube.BBox
	found := false
	progresses := [2]float32{}
	if piston != nil {
		progresses = [2]float32{piston.LastProgress, piston.Progress}
	}
	for _, progress := range progresses {
		var offset mgl64.Vec3
		if piston != nil {
			if mb.Expanding {
				progress -= 1
			}
			offset = faceOffset(piston.Facing, float64(progress))
		}
		var cell cube.Pos
		for axis := range 3 {
			cell[axis] = int(math.Floor(float64(float32(pos[axis]) + float32(offset[axis]))))
		}
		for _, box := range mb.Moving.Model().BBox(cell, source) {
			box = pistonCollisionBox(box, cell)
			if !found {
				bounds, found = box, true
				continue
			}
			lo, hi := bounds.Min(), bounds.Max()
			for axis := range 3 {
				lo[axis] = min(lo[axis], box.Min()[axis])
				hi[axis] = max(hi[axis], box.Max()[axis])
			}
			bounds = cube.Box(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2])
		}
	}
	if !found {
		return nil
	}
	return []cube.BBox{bounds}
}

// Native collision-shape getters add integer cell coordinates in binary32.
func pistonCollisionBox(box cube.BBox, pos cube.Pos) cube.BBox {
	lo, hi := box.Min(), box.Max()
	for axis := range 3 {
		lo[axis] = float64(float32(pos[axis]) + float32(lo[axis]))
		hi[axis] = float64(float32(pos[axis]) + float32(hi[axis]))
	}
	return cube.Box(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2])
}

// Native moveEntityLastProgress temporarily uses LastProgress while resolving
// the actor move. Supply that view without changing the live actor or its clock.
func (p Piston) movementSource(pos cube.Pos, tx *world.Tx) world.BlockSource {
	p.Progress = p.LastProgress
	return pistonMovementSource{tx: tx, pos: pos, piston: p}
}

type pistonMovementSource struct {
	tx     *world.Tx
	pos    cube.Pos
	piston Piston
}

func (s pistonMovementSource) Block(pos cube.Pos) world.Block {
	if pos == s.pos {
		return s.piston
	}
	return s.tx.Block(pos)
}

func pushEntityByPiston(e world.Entity, dir cube.Face, distance float64, source world.BlockSource) bool {
	delta := faceOffset(dir, distance)
	if mover, ok := e.(interface {
		MoveByPiston(mgl64.Vec3, world.BlockSource)
	}); ok {
		mover.MoveByPiston(delta, source)
		return true
	}
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

// EncodeNBT encodes the PistonArm block actor with the fields BDS writes.
// Only a retracted piston is movable. BDS writes block positions as an int
// array, or as an empty list when there are none.
func (p Piston) EncodeNBT() map[string]any {
	var attached any = []any{}
	if len(p.Attached) > 0 {
		coords := make([]int32, 0, len(p.Attached)*3)
		for _, pos := range p.Attached {
			coords = append(coords, int32(pos.X()), int32(pos.Y()), int32(pos.Z()))
		}
		attached = coords
	}
	return map[string]any{
		"id":             "PistonArm",
		"Progress":       p.Progress,
		"LastProgress":   p.LastProgress,
		"isMovable":      boolByte(p.State == pistonStateRetracted),
		"AttachedBlocks": attached,
		"BreakBlocks":    []any{},
		"Sticky":         boolByte(p.Sticky),
		"State":          p.State,
		"NewState":       p.NewState,
	}
}

// DecodeNBT decodes a PistonArm block actor. Whether the piston extends and
// is powered follows from its state. Pistons saved before the facing
// stopped being written keep the facing they were saved with.
func (p Piston) DecodeNBT(data map[string]any) any {
	if _, ok := data["facing"]; ok {
		p.Facing = cube.Face(nbtconv.Uint8(data, "facing"))
		p.legacyFacing = true
	}
	if _, ok := data["Sticky"]; ok {
		p.Sticky = nbtconv.Bool(data, "Sticky")
	}
	p.Progress = nbtconv.Float32(data, "Progress")
	p.LastProgress = nbtconv.Float32(data, "LastProgress")
	p.State = nbtconv.Uint8(data, "State")
	p.NewState = nbtconv.Uint8(data, "NewState")
	p.previousState = p.NewState
	p.Extending = p.State == pistonStateExtending || p.State == pistonStateExtended
	p.Powered = p.Extending
	p.Moving = p.State == pistonStateExtending || p.State == pistonStateRetracting
	p.hasRequest = !p.Moving && p.NewState != p.State
	p.requestedState = p.NewState
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

// Model exposes the three native collision parts at the parent actor's
// current animation position. A head without a piston actor has no collision.
func (p PistonHead) Model() world.BlockModel { return pistonHeadModel{head: p} }

type pistonHeadModel struct{ head PistonHead }

func (m pistonHeadModel) BBox(pos cube.Pos, source world.BlockSource) []cube.BBox {
	if source == nil {
		return nil
	}
	back := pos.Side(m.head.Facing.Opposite())
	p, ok := source.Block(back).(Piston)
	if !ok {
		return nil
	}
	progress := p.LastProgress + (p.Progress - p.LastProgress)
	direction := faceOffset(m.head.Facing, 1)
	var anchor mgl64.Vec3
	for axis := range 3 {
		anchor[axis] = float64(float32(back[axis]) + float32(direction[axis])*progress)
	}
	boxes := make([]cube.BBox, 0, 3)
	for _, shape := range pistonHeadCollisionParts(m.head.Facing) {
		lo, hi := shape.Min(), shape.Max()
		for axis := range 3 {
			lo[axis] = float64(float32(anchor[axis]) + float32(lo[axis]))
			hi[axis] = float64(float32(anchor[axis]) + float32(hi[axis]))
		}
		if lo[0] < hi[0] && lo[1] < hi[1] && lo[2] < hi[2] {
			boxes = append(boxes, cube.Box(lo[0], lo[1], lo[2], hi[0], hi[1], hi[2]).Translate(pos.Vec3().Mul(-1)))
		}
	}
	return boxes
}

func (pistonHeadModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return true }

func pistonHeadCollisionParts(face cube.Face) [3]cube.BBox {
	axis := 0
	switch face.Axis() {
	case cube.Y:
		axis = 1
	case cube.Z:
		axis = 2
	}
	tailLo, tailHi := mgl64.Vec3{5.0 / 16, 5.0 / 16, 5.0 / 16}, mgl64.Vec3{11.0 / 16, 11.0 / 16, 11.0 / 16}
	rodLo, rodHi := mgl64.Vec3{3.0 / 8, 3.0 / 8, 3.0 / 8}, mgl64.Vec3{5.0 / 8, 5.0 / 8, 5.0 / 8}
	plateLo, plateHi := mgl64.Vec3{}, mgl64.Vec3{1, 1, 1}
	rodLo[axis], rodHi[axis] = .25, .75
	if faceOffset(face, 1)[axis] > 0 {
		tailLo[axis], tailHi[axis], plateLo[axis] = -.25, .25, .75
	} else {
		tailLo[axis], tailHi[axis], plateHi[axis] = .75, 1.25, .25
	}
	return [3]cube.BBox{
		cube.Box(tailLo[0], tailLo[1], tailLo[2], tailHi[0], tailHi[1], tailHi[2]),
		cube.Box(rodLo[0], rodLo[1], rodLo[2], rodHi[0], rodHi[1], rodHi[2]),
		cube.Box(plateLo[0], plateLo[1], plateLo[2], plateHi[0], plateHi[1], plateHi[2]),
	}
}

// BreakInfo ...
func (p PistonHead) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, simpleDrops())
}

// PlayerWillDestroy removes the base before the head's destruction particles
// when a creative player breaks the head. Survival uses the queued survival
// check, so its head particles precede the base's particles instead.
func (p PistonHead) PlayerWillDestroy(pos cube.Pos, tx *world.Tx, source world.DamageSource) {
	damage, ok := source.(world.EntityDamageSource)
	if !ok {
		return
	}
	player, ok := damage.SourceEntity().(interface{ GameMode() world.GameMode })
	if !ok || !player.GameMode().CreativeInventory() {
		return
	}
	back := pos.Side(p.Facing.Opposite())
	if base, ok := tx.Block(back).(Piston); ok {
		tx.AddParticle(back.Vec3Centre(), particle.BlockBreak{Block: base})
		tx.SetBlock(back, nil, nil)
	}
}

// NeighbourChanged queues a survival recheck while the current actor state is
// still observable. A later repair may make the queued check harmless.
func (p PistonHead) NeighbourChanged(pos, _ cube.Pos, tx *world.Tx) {
	if !p.canSurvive(pos, tx) {
		tx.ScheduleBlockUpdate(pos, p, gameTicks(1))
	}
}

func (p PistonHead) canSurvive(pos cube.Pos, tx *world.Tx) bool {
	back := pos.Side(p.Facing.Opposite())
	base, ok := tx.Block(back).(Piston)
	return ok && base.Facing == p.Facing && (base.State == pistonStateExtending ||
		base.State == pistonStateExtended && base.NewState == pistonStateExtended)
}

// ScheduledTick removes an unsupported head without destruction particles.
func (p PistonHead) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !p.canSurvive(pos, tx) {
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

// pistonFacingDirection returns the facing_direction state Bedrock gives a
// piston whose head points towards face. Bedrock numbers horizontal piston
// directions opposite to every other block: facing_direction 4, west for
// other blocks, is a piston pushing east.
func pistonFacingDirection(face cube.Face) int32 {
	if face.Axis() == cube.Y {
		return int32(face)
	}
	return int32(face.Opposite())
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
	return nativePistonMovement(name) == pistonPopped
}

func sticksToPiston(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return nativePistonSticky(name)
}

func isPistonImmovable(b world.Block) bool {
	if piston, ok := b.(Piston); ok && piston.State != pistonStateRetracted {
		// The native walker also checks the actor's movable flag. A piston
		// has PushPull component data but is movable only while retracted.
		return true
	}
	name, _ := b.EncodeBlock()
	return nativePistonMovement(name) == pistonImmovable
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

	return c.walk(c.blockToMove, c.moveDirection)
}

// walk follows native _attachedBlockWalker's depth-first discovery. Adhesive
// branches explore every face except their incoming edge; ordinary payloads
// continue along movement. A lateral immovable neighbour does not prevent a
// move, whereas an obstruction along the movement direction does.
func (c *pistonMoveCalculator) walk(pos cube.Pos, incoming cube.Face) bool {
	if containsPos(c.toMove, pos) || (!c.extending && pos == c.armPos) {
		return true
	}
	if isEmptyOrReplaceable(c.tx, pos) {
		return true
	}
	b := c.tx.Block(pos)
	name, _ := b.EncodeBlock()
	movement := nativePistonMovement(name)
	if !c.extending && pos == c.blockToMove && (movement == pistonPush || movement == pistonPopped) {
		// Neither Push-only nor Popped is pulled by the sticky head. Native's
		// initial incoming edge differs from the retraction's movement edge.
		return true
	}
	if movement == pistonPush && incoming != c.moveDirection {
		// Native Push accepts a forced forward push, without side adhesion.
		return true
	}
	if pos == c.pistonPos || !canPushBlock(pos, b, c.moveDirection, true, c.extending, c.tx, c.pistonPos) {
		return incoming != c.moveDirection
	}
	if breaksWhenMoved(b) {
		if incoming == c.moveDirection {
			c.toDestroy = append(c.toDestroy, pos)
		}
		return true
	}
	c.toMove = append(c.toMove, pos)
	if len(c.toMove) > pistonMoveLimit {
		return false
	}
	if !sticksToPiston(b) {
		return c.walk(pos.Side(c.moveDirection), c.moveDirection)
	}
	// The native neighbour table uses piston-facing directions; horizontal
	// indices convert to South/North/East/West in physical block coordinates.
	for _, face := range [...]cube.Face{cube.FaceUp, cube.FaceDown, cube.FaceSouth, cube.FaceNorth, cube.FaceEast, cube.FaceWest} {
		if face == incoming.Opposite() {
			continue
		}
		next := pos.Side(face)
		other := c.tx.Block(next)
		if face != c.moveDirection && sticksToPiston(other) {
			id, _ := b.EncodeBlock()
			otherID, _ := other.EncodeBlock()
			if id != otherID {
				continue
			}
		}
		if !c.walk(next, face) {
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
