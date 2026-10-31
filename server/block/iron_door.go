package block

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

var hashIronDoor, hashIronTrapdoor = NextHash(), NextHash()

// IronDoor is a 1x2 door that only redstone can open (BlockSetType.IRON
// cannot be opened by hand).
type IronDoor struct {
	blockInteractionUpdate
	transparent
	sourceWaterDisplacer

	// Facing is the direction that the door opens towards.
	Facing cube.Direction
	// Open is whether the door is open.
	Open bool
	// Top is whether the block is the top half of the door.
	Top bool
	// Right is whether the door hinge is on the right side.
	Right bool
}

// Model ...
func (d IronDoor) Model() world.BlockModel {
	return model.Door{Facing: d.Facing, Open: d.Open, Right: d.Right, Top: d.Top}
}

// NeighbourUpdateTick breaks the door when its other half or the support
// below the lower half is gone, checking only the side the update came from.
func (d IronDoor) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	below, above := doorUpdateFrom(pos, changed)
	if d.Top {
		if _, ok := tx.Block(pos.Side(cube.FaceDown)).(IronDoor); below && !ok {
			breakBlockNoDrops(d, pos, tx)
			return
		}
	} else if support := pos.Side(cube.FaceDown); below && !tx.Block(support).Model().FaceSolid(support, cube.FaceUp, tx) {
		breakBlockNoDrops(d, pos, tx)
		dropItem(tx, item.NewStack(d, 1), pos.Vec3Centre())
		return
	} else if _, ok := tx.Block(pos.Side(cube.FaceUp)).(IronDoor); above && !ok {
		breakBlockNoDrops(d, pos, tx)
		return
	}
	tx.Redstone().ScheduleUpdate(pos)
}

// doorUpdateFrom reports whether an update from changed lets a door half at
// pos check the block below and the block above it. Like
// DoorBlock.updateShape, a door reacts only to the blocks above and below
// it; an update of the door itself checks both.
func doorUpdateFrom(pos, changed cube.Pos) (below, above bool) {
	return changed == pos || changed == pos.Side(cube.FaceDown), changed == pos || changed == pos.Side(cube.FaceUp)
}

// RedstoneEdgeUpdate opens or closes the door when the signal of either half
// switches.
func (d IronDoor) RedstoneEdgeUpdate(pos cube.Pos, tx *world.Tx, oldPower, newPower int) (world.Block, bool) {
	signal, switched := doorSignalSwitch(pos, d.Top, tx, oldPower, newPower)
	if !switched || signal == d.Open {
		return d, false
	}
	d.Open = signal
	return d, true
}

// RedstonePowerPostUpdate mirrors the new state onto the other half.
func (d IronDoor) RedstonePowerPostUpdate(pos cube.Pos, tx *world.Tx, _, after world.Block, _, _ int) {
	d = after.(IronDoor)
	otherPos := pos.Side(cube.Face(boolByte(!d.Top)))
	if door, ok := tx.Block(otherPos).(IronDoor); ok && door.Open != d.Open {
		door.Open = d.Open
		tx.SetBlock(otherPos, door, &world.SetOpts{DisableRedstoneUpdates: true})
	}
	if d.Open {
		tx.PlaySound(pos.Vec3Centre(), sound.DoorOpen{Block: d})
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.DoorClose{Block: d})
}

// UseOnBlock places both halves of the door on top of the clicked face.
func (d IronDoor) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if face != cube.FaceUp {
		return false
	}
	below := pos
	pos = pos.Side(cube.FaceUp)
	if !replaceableWith(tx, pos, d) || !replaceableWith(tx, pos.Side(cube.FaceUp), d) {
		return false
	}
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		return false
	}
	d.Facing = user.Rotation().Direction()
	left := tx.Block(pos.Side(d.Facing.RotateLeft().Face()))
	right := tx.Block(pos.Side(d.Facing.RotateRight().Face()))
	if _, ok := left.Model().(model.Door); ok {
		d.Right = true
	}
	if diffuser, ok := right.(LightDiffuser); !ok || diffuser.LightDiffusionLevel() != 0 {
		if diffuser, ok := left.(LightDiffuser); ok && diffuser.LightDiffusionLevel() == 0 {
			d.Right = true
		}
	}
	d.Open = tx.RedstonePower(pos) > 0 || tx.RedstonePower(pos.Side(cube.FaceUp)) > 0

	ctx.IgnoreBBox = true
	place(tx, pos, d, user, ctx)
	place(tx, pos.Side(cube.FaceUp), IronDoor{Facing: d.Facing, Open: d.Open, Top: true, Right: d.Right}, user, ctx)
	ctx.CountSub = 1
	return placed(ctx)
}

// BreakInfo ...
func (d IronDoor) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, oneOf(IronDoor{})).withBlastResistance(5)
}

// SideClosed ...
func (IronDoor) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// EncodeItem ...
func (IronDoor) EncodeItem() (name string, meta int16) { return "minecraft:iron_door", 0 }

// EncodeBlock ...
func (d IronDoor) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:iron_door", map[string]any{"minecraft:cardinal_direction": d.Facing.RotateRight().String(), "door_hinge_bit": d.Right, "open_bit": d.Open, "upper_block_bit": d.Top}
}

// Hash ...
func (d IronDoor) Hash() (uint64, uint64) {
	return hashIronDoor, uint64(d.Facing) | uint64(boolByte(d.Open))<<2 | uint64(boolByte(d.Top))<<3 | uint64(boolByte(d.Right))<<4
}

func allIronDoors() (doors []world.Block) {
	for facing := cube.Direction(0); facing <= 3; facing++ {
		for _, open := range []bool{false, true} {
			for _, top := range []bool{false, true} {
				for _, right := range []bool{false, true} {
					doors = append(doors, IronDoor{Facing: facing, Open: open, Top: top, Right: right})
				}
			}
		}
	}
	return
}

// IronTrapdoor is a 1x1 trapdoor that only redstone can open.
type IronTrapdoor struct {
	blockInteractionUpdate
	transparent
	sourceWaterDisplacer

	// Facing is the direction the trapdoor is facing.
	Facing cube.Direction
	// Open is whether the trapdoor is open.
	Open bool
	// Top is whether the trapdoor occupies the top part of the block.
	Top bool
}

// Model ...
func (t IronTrapdoor) Model() world.BlockModel {
	return model.Trapdoor{Facing: t.Facing, Top: t.Top, Open: t.Open}
}

// UseOnBlock places the trapdoor, upside down when clicked on the upper half
// of a side or on a bottom face, already open if placed powered.
func (t IronTrapdoor) UseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	t.Facing = user.Rotation().Direction().Opposite()
	t.Top = (clickPos.Y() > 0.5 && face != cube.FaceUp) || face == cube.FaceDown
	t.Open = tx.RedstonePower(pos) > 0
	place(tx, pos, t, user, ctx)
	return placed(ctx)
}

// RedstoneEdgeUpdate opens or closes the trapdoor when its signal switches.
func (t IronTrapdoor) RedstoneEdgeUpdate(_ cube.Pos, _ *world.Tx, oldPower, newPower int) (world.Block, bool) {
	if signal := newPower > 0; signal != (oldPower > 0) && signal != t.Open {
		t.Open = signal
		return t, true
	}
	return t, false
}

// RedstonePowerPostUpdate plays the open or close sound.
func (t IronTrapdoor) RedstonePowerPostUpdate(pos cube.Pos, tx *world.Tx, _, after world.Block, _, _ int) {
	if after.(IronTrapdoor).Open {
		tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorOpen{Block: after})
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.TrapdoorClose{Block: after})
}

// BreakInfo ...
func (t IronTrapdoor) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, oneOf(IronTrapdoor{})).withBlastResistance(5)
}

// SideClosed ...
func (IronTrapdoor) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// EncodeItem ...
func (IronTrapdoor) EncodeItem() (name string, meta int16) { return "minecraft:iron_trapdoor", 0 }

// EncodeBlock ...
func (t IronTrapdoor) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:iron_trapdoor", map[string]any{"direction": int32(math.Abs(float64(t.Facing) - 3)), "open_bit": t.Open, "upside_down_bit": t.Top}
}

// Hash ...
func (t IronTrapdoor) Hash() (uint64, uint64) {
	return hashIronTrapdoor, uint64(t.Facing) | uint64(boolByte(t.Open))<<2 | uint64(boolByte(t.Top))<<3
}

func allIronTrapdoors() (trapdoors []world.Block) {
	for facing := cube.Direction(0); facing <= 3; facing++ {
		for _, open := range []bool{false, true} {
			for _, top := range []bool{false, true} {
				trapdoors = append(trapdoors, IronTrapdoor{Facing: facing, Open: open, Top: top})
			}
		}
	}
	return
}
