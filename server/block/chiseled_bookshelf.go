package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ChiseledBookshelf is a six-slot bookshelf whose books are visible in the
// corresponding occupied block-state properties.
type ChiseledBookshelf struct {
	solid
	bass

	Facing cube.Direction
	Items [6]item.Stack
	LastInteractedSlot int
	StoredMask uint8
}
// Hash ...
func (b ChiseledBookshelf) Hash() (uint64, uint64) {
	mask := b.StoredMask & 63
	for slot, stack := range b.Items {
		if !stack.Empty() { mask |= 1 << slot }
	}
	return hashChiseledBookshelf, uint64(b.Facing)&3 | uint64(mask)<<2
}

// NewChiseledBookshelf returns an empty chiseled bookshelf facing north.
func NewChiseledBookshelf() ChiseledBookshelf {
	return ChiseledBookshelf{Facing: cube.North, LastInteractedSlot: -1}
}
// WithStoredMask sets the six occupancy bits decoded from native block state.
func (b ChiseledBookshelf) WithStoredMask(mask uint8) ChiseledBookshelf {
	b.StoredMask = mask & 63
	return b
}

func (b ChiseledBookshelf) EncodeItem() (string, int16) { return "minecraft:chiseled_bookshelf", 0 }
func (b ChiseledBookshelf) EncodeBlock() (string, map[string]any) {
	mask := b.StoredMask & 63
	for slot, stack := range b.Items {
		if !stack.Empty() { mask |= 1 << slot }
	}
	return "minecraft:chiseled_bookshelf", map[string]any{
		"direction": int32(horizontalDirection(b.Facing)),
		"books_stored": int32(mask),
	}
}


// UseOnBlock places the bookshelf facing away from the player.
func (b ChiseledBookshelf) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, ok := firstReplaceable(tx, pos, face, b)
	if !ok { return false }
	b = NewChiseledBookshelf()
	b.Facing = user.Rotation().Direction().Opposite()
	place(tx, pos, b, user, ctx)
	return placed(ctx)
}

// ActivateAt handles the clicked shelf face and exact slot, preserving Java's
// held-book insertion and empty-hand retrieval behavior.
func (b ChiseledBookshelf) ActivateAt(pos cube.Pos, face cube.Face, hit mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if face != b.Facing.Face() { return false }
	x := hit[0]
	z := hit[2]
	var horizontal float64
	switch b.Facing {
	case cube.North: horizontal = 1 - x
	case cube.South: horizontal = x
	case cube.West: horizontal = z
	case cube.East: horizontal = 1 - z
	default: return false
	}
	column := max(0, min(2, int(horizontal*3)))
	row := max(0, min(1, int((1-hit[1])*2)))
	slot := row*3 + column
	held, _ := user.HeldItems()
	if acceptsBookshelfBook(held) && b.Items[slot].Empty() {
		b.Items[slot] = held.Grow(1-held.Count())
		b.StoredMask |= 1 << slot
		b.LastInteractedSlot = slot
		ctx.SubtractFromCount(1)
		tx.SetBlock(pos, b, nil)
		soundName := "block.chiseled_bookshelf.insert"
		if _, enchanted := held.Item().(item.EnchantedBook); enchanted {
			soundName = "block.chiseled_bookshelf.insert.enchanted"
		}
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: soundName, Volume: 1, Pitch: 1})
		notifyComparatorUpdate(pos, tx)
		return true
	}
	if b.Items[slot].Empty() {
		return !acceptsBookshelfBook(held)
	}
	book := b.Items[slot]
	b.Items[slot] = item.Stack{}
	b.StoredMask &^= 1 << slot
	b.LastInteractedSlot = slot
	tx.SetBlock(pos, b, nil)
	soundName := "block.chiseled_bookshelf.pickup"
	if _, enchanted := book.Item().(item.EnchantedBook); enchanted {
		soundName = "block.chiseled_bookshelf.pickup.enchanted"
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: soundName, Volume: 1, Pitch: 1})
	notifyComparatorUpdate(pos, tx)
	if p, ok := user.(interface{ Inventory() *inventory.Inventory }); ok {
		if _, err := p.Inventory().AddItem(book); err != nil { dropItem(tx, book, pos.Vec3Centre()) }
	} else { dropItem(tx, book, pos.Vec3Centre()) }
	return true
}

func acceptsBookshelfBook(stack item.Stack) bool {
	if stack.Empty() { return false }
	name, _ := stack.Item().EncodeItem()
	switch name {
	case "minecraft:book", "minecraft:enchanted_book", "minecraft:writable_book", "minecraft:written_book":
		return true
	}
	return false
}

// ComparatorOutput exposes the slot most recently interacted with, as in Java.
func (b ChiseledBookshelf) ComparatorOutput(*world.Tx, cube.Pos) uint8 {
	if b.LastInteractedSlot < 0 || b.LastInteractedSlot >= len(b.Items) { return 0 }
	return uint8(b.LastInteractedSlot + 1)
}

// InsertItem accepts the first item from a hopper that fits the first empty slot.
func (b ChiseledBookshelf) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for sourceSlot, stack := range h.inventory.Slots() {
		if !acceptsBookshelfBook(stack) { continue }
		for slot := range b.Items {
			if !b.Items[slot].Empty() { continue }
			b.Items[slot] = stack.Grow(1-stack.Count())
			b.StoredMask |= 1 << slot
			b.LastInteractedSlot = slot
			_ = h.inventory.SetItem(sourceSlot, stack.Grow(-1))
			tx.SetBlock(pos, b, nil)
			notifyComparatorUpdate(pos, tx)
			return true
		}
	}
	return false
}

// ExtractItem transfers the first occupied slot to a hopper.
func (b ChiseledBookshelf) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	for slot := range b.Items {
		if b.Items[slot].Empty() { continue }
		if _, err := h.inventory.AddItem(b.Items[slot]); err != nil { return false }
		b.Items[slot] = item.Stack{}
		b.StoredMask &^= 1 << slot
		b.LastInteractedSlot = slot
		tx.SetBlock(pos, b, nil)
		notifyComparatorUpdate(pos, tx)
		return true
	}
	return false
}

func (ChiseledBookshelf) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, axeEffective, silkTouchOnlyDrop(ChiseledBookshelf{}))
}

// EncodeNBT stores the six independent stacks and comparator memory in the block actor.
func (b ChiseledBookshelf) EncodeNBT() map[string]any {
	data := map[string]any{
		"id": "ChiseledBookshelf",
		"BlockEntityVersion": int32(0),
	}
	hasItems := false
	for _, stack := range b.Items {
		if !stack.Empty() { hasItems = true; break }
	}
	if hasItems {
		items := make([]any, len(b.Items))
		for slot, stack := range b.Items {
			entry := map[string]any{"Name": "", "Count": byte(0), "Damage": int16(0), "WasPickedUp": byte(0)}
			if !stack.Empty() {
				entry = item.WriteNBT(stack, true)
				entry["WasPickedUp"] = byte(0)
			}
			items[slot] = entry
		}
		data["Items"] = items
	}
	if b.LastInteractedSlot >= 0 && b.LastInteractedSlot < 6 {
		data["LastInteractedSlot"] = int32(b.LastInteractedSlot + 1)
	}
	return data
}

// DecodeNBT restores actor contents and last-interacted comparator state.
func (b ChiseledBookshelf) DecodeNBT(data map[string]any) any {
	b.StoredMask = 0
	items := nbtconv.Slice(data, "Items")
	registry := world.BlockRegistryFromNBT(data)
	for slot := range b.Items {
		b.Items[slot] = item.Stack{}
		if slot >= len(items) { continue }
		entry, _ := items[slot].(map[string]any)
		stack := item.ReadNBT(entry, nil, registry)
		if stack.Empty() { continue }
		b.Items[slot] = stack.Grow(1-stack.Count())
		b.StoredMask |= 1 << slot
	}
	last, ok := data["LastInteractedSlot"]
	if ok {
		if value, valid := last.(int32); valid {
			b.LastInteractedSlot = int(value) - 1
		} else {
			b.LastInteractedSlot = -1
		}
	} else if _, ok := data["last_interacted_slot"]; ok {
		b.LastInteractedSlot = int(nbtconv.Int32(data, "last_interacted_slot"))
	} else {
		b.LastInteractedSlot = -1
	}
	if b.LastInteractedSlot < -1 || b.LastInteractedSlot > 5 { b.LastInteractedSlot = -1 }
	return b
}
