package block

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"time"
)

// Jukebox is a block used to play music discs.
type Jukebox struct {
	solid
	bass

	// Item is the music disc played by the jukebox.
	Item item.Stack
}

// InsertItem ...
func (j Jukebox) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	if !j.Item.Empty() {
		return false
	}

	for sourceSlot, sourceStack := range h.inventory.Slots() {
		if sourceStack.Empty() {
			continue
		}

		if m, ok := sourceStack.Item().(item.MusicDisc); ok {
			j.Item = sourceStack
			tx.SetBlock(pos, j, nil)
			notifyComparatorUpdate(pos, tx)
			_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
			tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscPlay{DiscType: m.DiscType})
			return true
		}
	}

	return false
}

// ExtractItem ...
func (j Jukebox) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	if j.Item.Empty() {
		return false
	}
	_, err := h.inventory.AddItem(j.Item.Grow(-j.Item.Count() + 1))
	if err != nil {
		return false
	}
	j.Item = item.Stack{}
	tx.SetBlock(pos, j, nil)
	notifyComparatorUpdate(pos, tx)
	tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscEnd{})
	return true
}

// FuelInfo ...
func (j Jukebox) FuelInfo() item.FuelInfo {
	return newFuelInfo(time.Second * 15)
}

// BreakInfo ...
func (j Jukebox) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(Jukebox{})).withBlastResistance(6).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if _, hasDisc := j.Disc(); hasDisc {
			dropItem(tx, j.Item, pos.Vec3())
			tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscEnd{})
		}
	})
}

// jukeboxUser represents an item.User that can use a jukebox.
type jukeboxUser interface {
	item.User
	// SendJukeboxPopup sends a jukebox popup to the item.User.
	SendJukeboxPopup(a ...any)
}

// Activate ...
func (j Jukebox) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if _, hasDisc := j.Disc(); hasDisc {
		dropItem(tx, j.Item, pos.Side(cube.FaceUp).Vec3Middle())

		j.Item = item.Stack{}
		tx.SetBlock(pos, j, nil)
		notifyComparatorUpdate(pos, tx)
		tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscEnd{})
	} else if held, _ := u.HeldItems(); !held.Empty() {
		if m, ok := held.Item().(item.MusicDisc); ok {
			j.Item = held

			tx.SetBlock(pos, j, nil)
			notifyComparatorUpdate(pos, tx)
			tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscEnd{})
			ctx.SubtractFromCount(1)

			tx.PlaySound(pos.Vec3Centre(), sound.MusicDiscPlay{DiscType: m.DiscType})
			if u, ok := u.(jukeboxUser); ok {
				u.SendJukeboxPopup(fmt.Sprintf("Now playing: %v - %v", m.DiscType.Author(), m.DiscType.DisplayName()))
			}
		}
	}
	return true
}

// Disc returns the currently playing music disc
func (j Jukebox) Disc() (sound.DiscType, bool) {
	if !j.Item.Empty() {
		if m, ok := j.Item.Item().(item.MusicDisc); ok {
			return m.DiscType, true
		}
	}
	return sound.DiscType{}, false
}

// ComparatorOutput returns the redstone signal output for a comparator.
func (j Jukebox) ComparatorOutput(*world.Tx, cube.Pos) uint8 {
	d, ok := j.Disc()
	if !ok {
		return 0
	}
	switch d.Uint8() {
	case sound.Disc13().Uint8():
		return 1
	case sound.DiscCat().Uint8():
		return 2
	case sound.DiscBlocks().Uint8():
		return 3
	case sound.DiscChirp().Uint8():
		return 4
	case sound.DiscFar().Uint8():
		return 5
	case sound.DiscMall().Uint8():
		return 6
	case sound.DiscMellohi().Uint8():
		return 7
	case sound.DiscStal().Uint8():
		return 8
	case sound.DiscStrad().Uint8():
		return 9
	case sound.DiscWard().Uint8():
		return 10
	case sound.Disc11().Uint8():
		return 11
	case sound.DiscWait().Uint8():
		return 12
	case sound.DiscPigstep().Uint8():
		return 13
	case sound.DiscOtherside().Uint8():
		return 14
	case sound.Disc5().Uint8(), sound.DiscRelic().Uint8():
		return 15
	}
	return 0
}

// EncodeNBT ...
func (j Jukebox) EncodeNBT() map[string]any {
	m := map[string]any{"id": "Jukebox"}
	if _, hasDisc := j.Disc(); hasDisc {
		m["RecordItem"] = nbtconv.WriteItem(j.Item, true)
	}
	return m
}

// DecodeNBT ...
func (j Jukebox) DecodeNBT(data map[string]any) any {
	s := nbtconv.MapItem(data, "RecordItem")
	if _, ok := s.Item().(item.MusicDisc); ok {
		j.Item = s
	}
	return j
}

// EncodeItem ...
func (Jukebox) EncodeItem() (name string, meta int16) {
	return "minecraft:jukebox", 0
}

// EncodeBlock ...
func (Jukebox) EncodeBlock() (string, map[string]any) {
	return "minecraft:jukebox", nil
}
