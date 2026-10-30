package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Note is a musical block that emits sounds when powered with redstone.
type Note struct {
	solid
	bass

	// Pitch is the current pitch the note block is set to. Value ranges from 0-24.
	Pitch int
	// Powered is whether the note block was powered during its last redstone update.
	Powered bool
}

// playNote emits the configured note sound and particle at pos, and the
// note_block_play game event allays listen for, as NoteBlock.playNote. The
// source is the entity that played it, nil for redstone.
func (n Note) playNote(pos cube.Pos, tx *world.Tx, source world.Entity) {
	instrument := n.instrument(pos, tx)
	// BlockSource::blockEvent publishes the selected instrument before
	// NoteBlock::triggerEvent emits the corresponding LevelSoundEvent.
	tx.AddParticle(pos.Vec3(), particle.Note{Instrument: instrument, Pitch: n.Pitch})
	tx.PlaySound(pos.Vec3(), sound.Note{Instrument: instrument, Pitch: n.Pitch})
	tx.EmitGameEvent(world.GameEventNoteBlockPlay, pos.Vec3Centre(), world.GameEventContext{Source: source})
}

// instrument returns the note block instrument selected by the block below it.
func (n Note) instrument(pos cube.Pos, tx *world.Tx) sound.Instrument {
	if instrumentBlock, ok := tx.Block(pos.Side(cube.FaceDown)).(interface {
		Instrument() sound.Instrument
	}); ok {
		return instrumentBlock.Instrument()
	}
	return sound.Piano()
}

func (n Note) DecodeNBT(data map[string]any) any {
	n.Pitch = min(24, int(nbtconv.Uint8(data, "note")))
	n.Powered = false
	return n
}

// EncodeNBT encodes the Music block actor. Native power history is transient;
// loading an actor restores only its clamped note, not the preceding edge.
func (n Note) EncodeNBT() map[string]any {
	return map[string]any{"id": "Music", "note": byte(n.Pitch)}
}

// javaActorState returns the server-only Java state a block actor keeps
// under adamant:java, or the actor itself if it was saved before that state
// moved there.
func javaActorState(data map[string]any) map[string]any {
	if state, ok := data["adamant:java"].(map[string]any); ok {
		return state
	}
	return data
}

func (n Note) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if !n.canPlay(pos, tx) {
		return false
	}
	n.Pitch = (n.Pitch + 1) % 25
	n.playNote(pos, tx, u)
	tx.SetBlock(pos, n, &world.SetOpts{DisableBlockUpdates: true})
	return true
}

// Punch ports NoteBlock.attack: a note block plays its note when hit.
func (n Note) Punch(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User) {
	if n.canPlay(pos, tx) {
		n.playNote(pos, tx, u)
	}
}

// RedstonePowerUpdate records power changes; sound is deferred to post-update so cancellation can suppress it.
func (n Note) RedstonePowerUpdate(pos cube.Pos, tx *world.Tx, power int) (world.Block, bool) {
	powered := power > 0
	if powered == n.Powered {
		return n, false
	}
	n.Powered = powered
	return n, true
}

// RedstonePowerPostUpdate plays the note after an uncancelled rising redstone edge.
func (n Note) RedstonePowerPostUpdate(pos cube.Pos, tx *world.Tx, before, after world.Block, _, _ int) {
	beforeNote, beforeOK := before.(Note)
	afterNote, afterOK := after.(Note)
	if !beforeOK || !afterOK || beforeNote.Powered || !afterNote.Powered || !afterNote.canPlay(pos, tx) {
		return
	}
	afterNote.playNote(pos, tx, nil)
}

// canPlay reports whether the block above the note block is air.
func (n Note) canPlay(pos cube.Pos, tx *world.Tx) bool {
	_, ok := tx.Block(pos.Side(cube.FaceUp)).(Air)
	return ok
}

func (n Note) BreakInfo() BreakInfo {
	return newBreakInfo(0.8, alwaysHarvestable, axeEffective, oneOf(Note{}))
}

func (n Note) EncodeItem() (name string, meta int16) {
	return "minecraft:noteblock", 0
}

func (n Note) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:noteblock", nil
}
