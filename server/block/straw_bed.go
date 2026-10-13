package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// StrawBed is a disposable sleeping mat. It never sets the player's respawn point.
type StrawBed struct {
	transparent
	sourceWaterDisplacer
	Facing         cube.Direction
	Head, Occupied bool
	Sleeper        *world.EntityHandle
}

var hashStrawBed = NextHash()

func (b StrawBed) Hash() (uint64, uint64) {
	return hashStrawBed, uint64(b.Facing) | uint64(boolByte(b.Head))<<2 | uint64(boolByte(b.Occupied))<<3
}
func (StrawBed) MaxCount() int               { return 16 }
func (StrawBed) CompostChance() float64      { return .65 }
func (StrawBed) EncodeItem() (string, int16) { return "minecraft:straw_bed", 0 }
func (b StrawBed) EncodeBlock() (string, map[string]any) {
	return "minecraft:straw_bed", map[string]any{"minecraft:cardinal_direction": b.Facing.String(), "head_piece_bit": boolByte(b.Head), "occupied_bit": boolByte(b.Occupied)}
}
func (b StrawBed) Model() world.BlockModel                     { return model.StrawBed{Facing: b.Facing, Head: b.Head} }
func (StrawBed) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

func (b StrawBed) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	var used bool
	pos, _, used = firstReplaceable(tx, pos, face, b)
	if !used {
		return false
	}
	b.Facing, b.Head, b.Occupied, b.Sleeper = user.Rotation().Direction(), false, false, nil
	headPos := pos.Side(b.Facing.Face())
	if !replaceableWith(tx, headPos, b) || headPos.OutOfBounds(tx.Range()) || !tx.World().WithinBorder(headPos) {
		return false
	}
	// Source placement does not require a supporting block below either half.
	ctx.IgnoreBBox = true
	place(tx, pos, b, user, ctx)
	head := b
	head.Head = true
	place(tx, headPos, head, user, ctx)
	return placed(ctx)
}

func (b StrawBed) counterpart(pos cube.Pos, tx *world.Tx) (StrawBed, cube.Pos, bool) {
	direction := b.Facing
	if b.Head {
		direction = direction.Opposite()
	}
	otherPos := pos.Side(direction.Face())
	other, ok := tx.Block(otherPos).(StrawBed)
	return other, otherPos, ok && other.Head != b.Head
}

// SleepHeight validates the foot half before exposing Source's sleep elevation.
func (b StrawBed) SleepHeight(pos cube.Pos, tx *world.Tx) (float64, bool) {
	if b.Head {
		other, _, ok := b.counterpart(pos, tx)
		if !ok || other.Head {
			return 0, false
		}
	}
	return .25, true
}

func (b StrawBed) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, user item.User, _ *item.UseContext) bool {
	other, otherPos, ok := b.counterpart(pos, tx)
	if !ok {
		return true
	}
	if !b.Head {
		b, pos = other, otherPos
	}
	if tx.World().Dimension() != world.Overworld {
		b.destroy(pos, tx)
		return true
	}
	sleeper, ok := user.(world.Sleeper)
	if !ok {
		return false
	}
	if b.Occupied {
		sleeper.Messaget(chat.MessageBedIsOccupied)
		return true
	}
	if _, sleeping := sleeper.Sleeping(); sleeping {
		return true
	}
	if living, ok := user.(interface{ Dead() bool }); ok && living.Dead() {
		return true
	}
	footPos := pos.Side(b.Facing.Opposite().Face())
	reachable := func(p cube.Pos) bool {
		v := user.Position().Sub(p.Vec3Centre().Sub(mgl64.Vec3{0, .5, 0}))
		return math.Abs(v[0]) <= 3 && math.Abs(v[1]) <= 2 && math.Abs(v[2]) <= 3
	}
	if !reachable(pos) && !reachable(footPos) {
		sleeper.Messaget(chat.MessageBedTooFar)
		return true
	}
	for _, p := range [...]cube.Pos{pos.Side(cube.FaceUp), footPos.Side(cube.FaceUp)} {
		if bedBlockSuffocates(p, tx) {
			sleeper.Messaget(chat.MessageBedObstructed)
			return true
		}
	}
	if !tx.IsDarkOutside() {
		sleeper.Messaget(chat.MessageNoSleep)
		return true
	}
	if bedRestUnsafe(pos, tx, sleeper) {
		sleeper.Messaget(chat.MessageNoSleepMonsters)
		return true
	}
	if binder, ok := sleeper.(interface{ BindTransaction(*world.Tx) }); ok {
		binder.BindTransaction(tx)
	}
	sleeper.Sleep(pos)
	return true
}

// StopSleeping follows the live dimension rule: Overworld mats break on leave.
func (b StrawBed) StopSleeping(pos cube.Pos, tx *world.Tx) {
	if tx.World().Dimension() == world.Overworld {
		b.destroy(pos, tx)
		return
	}
	b.Sleeper, b.Occupied = nil, false
	tx.SetBlock(pos, b, nil)
}
func (b StrawBed) destroy(pos cube.Pos, tx *world.Tx) {
	tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "straw_bed.break_leave", EntityType: ":", ExtraData: -1})
	other, otherPos, connected := b.counterpart(pos, tx)
	tx.SetBlock(pos, nil, nil)
	// Source applies the partner's shape update before the sleeper searches for
	// a stand-up position. The deferred native neighbor callback is idempotent.
	if connected {
		other.NeighbourUpdateTick(otherPos, pos, tx)
	}
}
func (b StrawBed) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	other, otherPos, ok := b.counterpart(pos, tx)
	if changed != otherPos {
		return
	}
	if !ok {
		breakBlock(b, pos, tx)
		return
	}
	if b.Occupied != other.Occupied {
		b.Occupied = other.Occupied
		tx.SetBlock(pos, b, nil)
	}
}
func (b StrawBed) BreakInfo() BreakInfo {
	drops := simpleDrops()
	if b.Head {
		drops = oneOf(StrawBed{})
	}
	return newBreakInfo(.2, alwaysHarvestable, nothingEffective, drops).withBreakHandler(func(pos cube.Pos, tx *world.Tx, user item.User) {
		if b.Sleeper != nil {
			if e, ok := b.Sleeper.Entity(tx); ok {
				if sleeper, ok := e.(world.Sleeper); ok {
					sleeper.Wake()
				}
			}
		}
		if !b.Head {
			if mode, ok := user.(interface{ GameMode() world.GameMode }); ok && mode.GameMode().CreativeInventory() {
				if head, headPos, ok := b.counterpart(pos, tx); ok && head.Head {
					breakBlockNoDrops(head, headPos, tx)
				}
			}
		}
	})
}

func allStrawBeds() []world.Block {
	blocks := make([]world.Block, 0, 16)
	for _, direction := range cube.Directions() {
		for _, head := range [...]bool{false, true} {
			for _, occupied := range [...]bool{false, true} {
				blocks = append(blocks, StrawBed{Facing: direction, Head: head, Occupied: occupied})
			}
		}
	}
	return blocks
}
