package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var (
	_ world.RedstonePowerSource        = RedstoneTorch{}
	_ world.RedstonePowerContextAction = RedstoneTorch{}
	_ world.ScheduledTicker            = RedstoneTorch{}
	_ world.RedstoneCircuitComponent   = RedstoneTorch{}
	_ world.NeighbourChangeListener    = RedstoneTorch{}
)

// RedstoneTorch is a non-solid block that emits light and provides a full-strength redstone signal when lit.
type RedstoneTorch struct {
	transparent
	empty

	// Facing is the direction from the torch to the block it is attached to.
	Facing cube.Face
	// Lit indicates whether the redstone torch is currently lit and emitting power.
	Lit bool
}

func (RedstoneTorch) HasLiquidDrops() bool {
	return true
}

func (t RedstoneTorch) LightEmissionLevel() uint8 {
	if t.Lit {
		return 7
	}
	return 0
}

func (t RedstoneTorch) facing() cube.Face {
	if t.Facing == unknownFace {
		return cube.FaceDown
	}
	return t.Facing
}

func (t RedstoneTorch) circuitFacing(pos cube.Pos, tx *world.Tx) cube.Face {
	if tx != nil {
		if facing, ok := tx.Redstone().CircuitDirection(pos); ok {
			return facing
		}
	}
	return t.facing()
}

func (t RedstoneTorch) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

func (t RedstoneTorch) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	if _, ok := tx.Block(pos).(world.Liquid); ok {
		return false
	}
	// Torches cannot hang from a ceiling: like BDS, a click on a block's
	// bottom looks for another support as a click on an unsupported side does.
	if face == cube.FaceDown || !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		fallbackFace, ok := findTorchPlacementFace(pos, tx)
		if !ok {
			return false
		}
		face = fallbackFace
	}
	t.Facing = face.Opposite()
	// TorchBlock::getPlacementBlock selects the item's lit default variant
	// and changes only its facing. Inversion belongs to the next shared
	// capacitor step, even when the attachment is already powered.
	t.Lit = true

	place(tx, pos, t, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick checks support; the world-owned capacitor handles inversion.
func (t RedstoneTorch) NeighbourUpdateTick(pos, changed cube.Pos, tx *world.Tx) {
	t.NeighbourChanged(pos, changed, tx)
}

// NeighbourChanged resets native burnout only once mCanReignite is set, then
// checks support at the instant of the notification.
func (t RedstoneTorch) NeighbourChanged(pos, changed cube.Pos, tx *world.Tx) {
	tx.Redstone().NotifyCircuitNeighbour(pos, changed)
	facing := t.facing()
	if !tx.Block(pos.Side(facing)).Model().FaceSolid(pos.Side(facing), facing.Opposite(), tx) {
		breakBlock(t, pos, tx)
	}
}

// ScheduledTick reconciles callbacks saved by older worlds. It does not start
// an independent inversion clock or restore the receiver's stale permutation.
func (RedstoneTorch) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if tx != nil {
		tx.Redstone().ScheduleUpdate(pos)
	}
}

// RedstonePower emits power from every side except the attached block while lit.
func (t RedstoneTorch) RedstonePower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if t.Lit && face != t.circuitFacing(pos, tx) {
		return 15
	}
	return 0
}

// RedstoneStrongPower strongly powers the block above the torch while lit.
func (t RedstoneTorch) RedstoneStrongPower(pos cube.Pos, tx *world.Tx, face cube.Face) int {
	if face == cube.FaceUp {
		return t.RedstonePower(pos, tx, face)
	}
	return 0
}

// RedstonePowerActionUpdate invalidates the capacitor after explicit callers'
// input changes. Inversion itself runs on the shared circuit clock.
func (RedstoneTorch) RedstonePowerActionUpdate(pos cube.Pos, tx *world.Tx, _ world.RedstoneUpdate) {
	if tx != nil {
		tx.Redstone().ScheduleUpdate(pos)
	}
}

// attachmentPowered reports whether the block the torch is attached to is powered.
func (t RedstoneTorch) attachmentPowered(pos cube.Pos, tx *world.Tx) bool {
	if tx == nil {
		return false
	}
	attached := pos.Side(t.facing())
	attachedBlock := tx.Block(attached)
	if source, ok := attachedBlock.(world.RedstonePowerSource); ok {
		for _, face := range cube.Faces() {
			if world.ClampRedstonePower(source.RedstonePower(attached, tx, face)) > 0 {
				return true
			}
		}
	}
	return world.RedstoneFullPowerConductor(attached, attachedBlock, tx) && tx.RedstoneConductivePower(attached) > 0
}

func (RedstoneTorch) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone_torch", 0
}

func (t RedstoneTorch) EncodeBlock() (name string, properties map[string]any) {
	name = "minecraft:unlit_redstone_torch"
	if t.Lit {
		name = "minecraft:redstone_torch"
	}
	var direction string
	switch t.Facing {
	case cube.FaceDown:
		direction = "top"
	case unknownFace:
		direction = "unknown"
	default:
		direction = t.Facing.String()
	}
	return name, map[string]any{"torch_facing_direction": direction}
}

// redstoneTorchFallbackSides lists the faces to check for placing a redstone torch on a non-solid block.
var redstoneTorchFallbackSides = [...]cube.Face{
	cube.FaceSouth,
	cube.FaceWest,
	cube.FaceNorth,
	cube.FaceEast,
	cube.FaceDown,
}

// findTorchPlacementFace finds a valid face for placing a redstone torch on a non-solid block.
func findTorchPlacementFace(pos cube.Pos, tx *world.Tx) (cube.Face, bool) {
	for _, side := range redstoneTorchFallbackSides {
		if tx.Block(pos.Side(side)).Model().FaceSolid(pos.Side(side), side.Opposite(), tx) {
			return side.Opposite(), true
		}
	}
	return 0, false
}

func allRedstoneTorches() (all []world.Block) {
	for _, face := range cube.Faces() {
		if face == cube.FaceUp {
			face = unknownFace
		}
		all = append(all, RedstoneTorch{Facing: face}, RedstoneTorch{Facing: face, Lit: true})
	}
	return
}
