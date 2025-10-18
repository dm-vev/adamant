package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Bed represents a bed block that players can sleep in to skip the night and set their respawn point.
type Bed struct {
	transparent
	sourceWaterDisplacer

	// Colour specifies the colour of the bed.
	//blockhash:ignore
	Colour item.Colour
	// Facing is the direction from the foot of the bed towards the head.
	Facing cube.Direction
	// Part indicates whether this block represents the foot or the head of the bed.
	Part BedPart
	// Occupied specifies if the bed is currently occupied by a sleeping player.
	Occupied bool
}

// BedPart identifies which part of a bed a block represents.
type BedPart uint8

const (
	// BedFoot represents the foot of a bed.
	BedFoot BedPart = iota
	// BedHead represents the head of a bed.
	BedHead
)

// FlammabilityInfo returns the flammability data of a bed.
func (Bed) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 60, true)
}

// Model returns the model used by beds.
func (Bed) Model() world.BlockModel {
	return model.Bed{}
}

// EncodeItem ...
func (b Bed) EncodeItem() (name string, meta int16) {
	return "minecraft:bed", int16(b.Colour.Uint8())
}

// EncodeBlock ...
func (b Bed) EncodeBlock() (string, map[string]any) {
	return "minecraft:bed", map[string]any{
		"direction":      int32(horizontalDirection(b.Facing)),
		"head_piece_bit": b.Part == BedHead,
		"occupied_bit":   b.Occupied,
	}
}

// EncodeNBT stores additional bed data such as its colour.
func (b Bed) EncodeNBT() map[string]any {
	return map[string]any{
		"id":    "Bed",
		"color": int32(b.Colour.Uint8()),
	}
}

// DecodeNBT decodes the colour of the bed from block entity data.
func (b Bed) DecodeNBT(data map[string]any) any {
	if colours := item.Colours(); len(colours) > 0 {
		if id := nbtconv.Uint8(data, "color"); int(id) < len(colours) {
			b.Colour = colours[id]
		}
	}
	return b
}

// UseOnBlock handles placing beds in the world, setting both the head and foot at once.
func (b Bed) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, b)
	if !used {
		return false
	}

	facing := user.Rotation().Direction()
	headPos := pos.Add(directionOffset(facing))

	if !replaceableWith(tx, headPos, b) {
		return false
	}

	if !supportsBed(tx, pos) || !supportsBed(tx, headPos) {
		return false
	}

	ctx.IgnoreBBox = true
	foot := Bed{Colour: b.Colour, Facing: facing, Part: BedFoot}
	head := Bed{Colour: b.Colour, Facing: facing, Part: BedHead}

	place(tx, pos, foot, user, ctx)
	place(tx, headPos, head, user, ctx)
	return placed(ctx)
}

// Activate handles a player attempting to sleep in the bed.
func (b Bed) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	sleeper, ok := u.(bedSleeper)
	if !ok {
		return false
	}

	footPos := pos
	if b.Part == BedHead {
		footPos = pos.Side(b.Facing.Opposite().Face())
		if foot, ok := tx.Block(footPos).(Bed); ok {
			b = foot
		}
	}

	if tx.World().Dimension() != world.Overworld {
		b.explode(footPos, tx)
		return true
	}

	headPos := footPos.Side(b.Facing.Face())
	head, _ := tx.Block(headPos).(Bed)
	if b.Occupied || head.Occupied {
		if msg, ok := u.(bedMessager); ok {
			msg.Message("This bed is occupied")
		}
		return true
	}

	sleeper.Sleep(footPos, tx)
	return true
}

// NeighbourUpdateTick ensures both parts of the bed remain valid.
func (b Bed) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if b.Part == BedHead {
		footPos := pos.Side(b.Facing.Opposite().Face())
		if foot, ok := tx.Block(footPos).(Bed); !ok || foot.Facing != b.Facing || foot.Part != BedFoot {
			breakBlock(b, pos, tx)
		}
		return
	}

	headPos := pos.Side(b.Facing.Face())
	if head, ok := tx.Block(headPos).(Bed); !ok || head.Facing != b.Facing || head.Part != BedHead {
		breakBlock(b, pos, tx)
		return
	}
	if !supportsBed(tx, pos) || !supportsBed(tx, headPos) {
		breakBlock(b, pos, tx)
	}
}

// BreakInfo ...
func (b Bed) BreakInfo() BreakInfo {
	drops := simpleDrops()
	if b.Part == BedFoot {
		drops = oneOf(b)
	}
	return newBreakInfo(0.2, alwaysHarvestable, axeEffective, drops).withBreakHandler(func(pos cube.Pos, tx *world.Tx, _ item.User) {
		footPos := pos
		bed := b
		if bed.Part == BedHead {
			footPos = pos.Side(bed.Facing.Opposite().Face())
			if foot, ok := tx.Block(footPos).(Bed); ok {
				bed = foot
			}
		}

		headPos := footPos.Side(bed.Facing.Face())
		if _, ok := tx.Block(headPos).(Bed); ok {
			tx.SetBlock(headPos, nil, nil)
		}
		if footPos != pos {
			tx.SetBlock(footPos, nil, nil)
		}
		wakeSleepingAt(tx, footPos)
	})
}

// SpawnPosition finds a safe spawn location around the bed's foot.
func (b Bed) SpawnPosition(pos cube.Pos, tx *world.Tx) (cube.Pos, bool) {
	for _, offset := range bedSpawnOffsets(b.Facing) {
		candidate := pos.Add(offset)
		if bedSafe(candidate, tx) {
			return candidate, true
		}
	}
	return cube.Pos{}, false
}

func (b Bed) explode(pos cube.Pos, tx *world.Tx) {
	wakeSleepingAt(tx, pos)
	headPos := pos.Side(b.Facing.Face())
	tx.SetBlock(pos, nil, nil)
	tx.SetBlock(headPos, nil, nil)
	ExplosionConfig{Size: 5, SpawnFire: true}.Explode(tx, pos.Vec3Centre())
}

type bedSleeper interface {
	item.User
	Sleep(pos cube.Pos, tx *world.Tx) bool
}

type bedMessager interface {
	Message(a ...any)
}

func wakeSleepingAt(tx *world.Tx, bedPos cube.Pos) {
	sleepers := tx.World().SleepingPlayers()
	if len(sleepers) == 0 {
		return
	}
	var toWake []uuid.UUID
	for id, pos := range sleepers {
		if pos == bedPos {
			toWake = append(toWake, id)
		}
	}
	if len(toWake) == 0 {
		return
	}
	tx.Players()(func(e world.Entity) bool {
		for i := 0; i < len(toWake); i++ {
			if e.H().UUID() == toWake[i] {
				if s, ok := e.(interface{ StopSleeping(*world.Tx) }); ok {
					s.StopSleeping(tx)
				}
				toWake = append(toWake[:i], toWake[i+1:]...)
				i--
			}
		}
		return len(toWake) > 0
	})
}

func supportsBed(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	return tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

func directionOffset(dir cube.Direction) cube.Pos {
	switch dir {
	case cube.North:
		return cube.Pos{0, 0, -1}
	case cube.South:
		return cube.Pos{0, 0, 1}
	case cube.East:
		return cube.Pos{1, 0, 0}
	case cube.West:
		return cube.Pos{-1, 0, 0}
	}
	return cube.Pos{}
}

func bedSpawnOffsets(facing cube.Direction) []cube.Pos {
	forward := directionOffset(facing)
	right := directionOffset(facing.RotateRight())
	left := directionOffset(facing.RotateLeft())

	doubleForward := scalePos(forward, 2)
	backward := scalePos(forward, -1)

	offsets := []cube.Pos{
		forward,
		forward.Add(left),
		forward.Add(right),
		left,
		right,
		cube.Pos{},
		doubleForward,
		doubleForward.Add(left),
		doubleForward.Add(right),
		backward,
		backward.Add(left),
		backward.Add(right),
	}
	unique := make([]cube.Pos, 0, len(offsets))
	seen := make(map[[3]int]struct{}, len(offsets))
	for _, off := range offsets {
		key := [3]int{off[0], off[1], off[2]}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, off)
	}
	return unique
}

func bedSafe(pos cube.Pos, tx *world.Tx) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	head := pos.Add(cube.Pos{0, 1})
	if head.OutOfBounds(tx.Range()) {
		return false
	}
	if len(tx.Block(pos).Model().BBox(pos, tx)) != 0 {
		return false
	}
	if len(tx.Block(head).Model().BBox(head, tx)) != 0 {
		return false
	}
	if _, ok := tx.Liquid(pos); ok {
		return false
	}
	if _, ok := tx.Liquid(head); ok {
		return false
	}
	below := pos.Side(cube.FaceDown)
	if below.OutOfBounds(tx.Range()) {
		return false
	}
	return tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

func scalePos(p cube.Pos, factor int) cube.Pos {
	return cube.Pos{p[0] * factor, p[1] * factor, p[2] * factor}
}

func allBeds() (blocks []world.Block) {
	for _, dir := range cube.Directions() {
		blocks = append(blocks, Bed{Facing: dir, Part: BedFoot})
		blocks = append(blocks, Bed{Facing: dir, Part: BedHead})
		blocks = append(blocks, Bed{Facing: dir, Part: BedFoot, Occupied: true})
		blocks = append(blocks, Bed{Facing: dir, Part: BedHead, Occupied: true})
	}
	return
}
