package block

import (
	"math/rand/v2"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// RedstoneTorch is a torch that emits redstone power when lit.
type RedstoneTorch struct {
	transparent
	empty

	Facing cube.Face
	Lit    bool
}

type redstoneTorchToggle struct {
	pos  cube.Pos
	tick int64
}

var (
	redstoneTorchToggleMu sync.Mutex
	redstoneTorchToggles  = map[*world.World][]redstoneTorchToggle{}
)

// BreakInfo ...
func (t RedstoneTorch) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(t))
}

// LightEmissionLevel ...
func (t RedstoneTorch) LightEmissionLevel() uint8 {
	if !t.Lit {
		return 0
	}
	return 7
}

// UseOnBlock ...
func (t RedstoneTorch) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, t)
	if !used {
		return false
	}
	if face == cube.FaceDown {
		return false
	}
	if !tx.Block(pos.Side(face.Opposite())).Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		found := false
		for _, i := range []cube.Face{cube.FaceSouth, cube.FaceWest, cube.FaceNorth, cube.FaceEast, cube.FaceDown} {
			if tx.Block(pos.Side(i)).Model().FaceSolid(pos.Side(i), i.Opposite(), tx) {
				found = true
				face = i.Opposite()
				break
			}
		}
		if !found {
			return false
		}
	}
	t.Facing = face.Opposite()
	t.Lit = true
	place(tx, pos, t, user, ctx)
	tx.DoBlockUpdatesAround(pos)
	tx.DoBlockUpdatesAround(pos.Side(t.Facing))
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (t RedstoneTorch) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(t.Facing)).Model().FaceSolid(pos.Side(t.Facing), t.Facing.Opposite(), tx) {
		breakBlock(t, pos, tx)
		return
	}
	tx.ScheduleBlockUpdate(pos, t, redstoneTicks(2))
}

// ScheduledTick ...
func (t RedstoneTorch) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	shouldOff := t.shouldTurnOff(pos, tx)
	w := tx.World()
	now := w.CurrentTick()

	if t.Lit {
		if shouldOff {
			t.Lit = false
			tx.SetBlock(pos, t, nil)
			tx.DoBlockUpdatesAround(pos)
			tx.DoBlockUpdatesAround(pos.Side(t.Facing))
			if redstoneTorchBurnedOut(w, pos, true, now) {
				tx.ScheduleBlockUpdate(pos, t, redstoneTicks(160))
			}
		}
		return
	}

	if !shouldOff && !redstoneTorchBurnedOut(w, pos, false, now) {
		t.Lit = true
		tx.SetBlock(pos, t, nil)
		tx.DoBlockUpdatesAround(pos)
		tx.DoBlockUpdatesAround(pos.Side(t.Facing))
	}
}

func (t RedstoneTorch) shouldTurnOff(pos cube.Pos, tx *world.Tx) bool {
	attachedPos := pos.Side(t.Facing)
	attachedFace := t.Facing
	return world.RedstonePowerAt(tx, attachedPos, attachedFace) > 0
}

func redstoneTorchBurnedOut(w *world.World, pos cube.Pos, turnOff bool, now int64) bool {
	if w == nil {
		return false
	}

	redstoneTorchToggleMu.Lock()
	defer redstoneTorchToggleMu.Unlock()

	list := redstoneTorchToggles[w]
	if len(list) > 0 {
		prune := 0
		for prune < len(list) && now-list[prune].tick > 60 {
			prune++
		}
		if prune > 0 {
			list = append([]redstoneTorchToggle(nil), list[prune:]...)
		}
	}

	if turnOff {
		list = append(list, redstoneTorchToggle{pos: pos, tick: now})
	}

	count := 0
	for _, toggle := range list {
		if toggle.pos == pos {
			count++
			if count >= 8 {
				redstoneTorchToggles[w] = list
				return true
			}
		}
	}

	if len(list) == 0 {
		delete(redstoneTorchToggles, w)
	} else {
		redstoneTorchToggles[w] = list
	}
	return false
}

// RedstoneWeakPower ...
func (t RedstoneTorch) RedstoneWeakPower(face cube.Face) uint8 {
	if !t.Lit {
		return 0
	}
	if face == t.Facing {
		return 0
	}
	return 15
}

// RedstoneStrongPower ...
func (t RedstoneTorch) RedstoneStrongPower(face cube.Face) uint8 {
	if !t.Lit {
		return 0
	}
	if face == cube.FaceDown {
		return 15
	}
	return 0
}

// EncodeItem ...
func (t RedstoneTorch) EncodeItem() (name string, meta int16) {
	return "minecraft:redstone_torch", 0
}

// EncodeBlock ...
func (t RedstoneTorch) EncodeBlock() (name string, properties map[string]any) {
	var face string
	if t.Facing == cube.FaceDown {
		face = "top"
	} else if t.Facing == unknownFace {
		face = "unknown"
	} else {
		face = t.Facing.String()
	}
	if t.Lit {
		return "minecraft:redstone_torch", map[string]any{"torch_facing_direction": face}
	}
	return "minecraft:unlit_redstone_torch", map[string]any{"torch_facing_direction": face}
}

func allRedstoneTorches() (torches []world.Block) {
	for _, face := range cube.Faces() {
		if face == cube.FaceUp {
			face = unknownFace
		}
		torches = append(torches, RedstoneTorch{Facing: face, Lit: true})
		torches = append(torches, RedstoneTorch{Facing: face, Lit: false})
	}
	return
}
