package block

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// PotDecoration represents an item that can be used as a decoration on a pot.
type PotDecoration interface {
	world.Item
	PotDecoration() bool
}

// DecoratedPot is a decoration block that can be crafted from up to four pottery sherds, and bricks on the sides where
// no pattern should be displayed.
type DecoratedPot struct {
	transparent
	sourceWaterDisplacer

	// Item is the item being stored in the decorated pot.
	Item item.Stack
	// Facing is the direction the pot is facing. The first decoration will be facing opposite of this direction.
	Facing cube.Direction
	// Decorations are the four decorations displayed on the sides of the pot. If a decoration is a brick or nil,
	// the side will appear to be empty.
	Decorations [4]PotDecoration

	// lootTable is the loot table the pot is filled from when it is first
	// used or broken, as the pots of trial chambers are.
	lootTable string
	lootSeed  int64
}

// withLoot returns the pot filled from its loot table, which it is once, as
// Java's RandomizableContainer.unpackLootTable fills it. The pot is set in the
// world if it is still there.
func (p DecoratedPot) withLoot(pos cube.Pos, tx *world.Tx) DecoratedPot {
	if p.lootTable == "" {
		return p
	}
	inv := inventory.New(1, nil)
	if err := fillContainerLoot(tx, pos, inv, p.lootTable, p.lootSeed, "minecraft:decorated_pot"); err != nil {
		slog.Error("decorated pot loot table", "table", p.lootTable, "pos", pos, "error", err)
	}
	p.Item, _ = inv.Item(0)
	p.lootTable, p.lootSeed = "", 0
	if _, ok := tx.Block(pos).(DecoratedPot); ok {
		tx.SetBlockEntity(pos, p)
	}
	return p
}

func (DecoratedPot) ContainerSize() int { return 1 }

// ComparatorOutput reads the native pot's one-slot container, including its
// deferred loot initialization, without exposing a container UI.
func (p DecoratedPot) ComparatorOutput(tx *world.Tx, pos cube.Pos) uint8 {
	p = p.withLoot(pos, tx)
	return comparatorSignalFromStacks([]item.Stack{p.Item})
}

// SideClosed ...
func (p DecoratedPot) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// ProjectileHit ports DecoratedPotBlock.onProjectileHit: impact projectiles
// that may interact and break shatter the pot.
func (p DecoratedPot) ProjectileHit(pos cube.Pos, tx *world.Tx, projectile world.Entity, _ cube.Face) {
	if !projectileMayInteract(tx, projectile) || !projectileMayBreak(tx, projectile) {
		return
	}
	for _, d := range p.Decorations {
		if d == nil {
			dropItem(tx, item.NewStack(item.Brick{}, 1), pos.Vec3Centre())
			continue
		}
		dropItem(tx, item.NewStack(d, 1), pos.Vec3Centre())
	}
	breakBlockNoDrops(p, pos, tx)
}

// Pick ...
func (p DecoratedPot) Pick() item.Stack {
	return item.NewStack(DecoratedPot{Decorations: p.Decorations}, 1)
}

// ExtractItem ...
func (p DecoratedPot) ExtractItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	p = p.withLoot(pos, tx)
	if p.Item.Empty() {
		return false
	}
	if _, err := h.inventory.AddItem(p.Item.Grow(-p.Item.Count() + 1)); err != nil {
		return false
	}
	p.Item = p.Item.Grow(-1)
	tx.SetBlock(pos, p, nil)
	return true
}

// InsertItem ...
func (p DecoratedPot) InsertItem(h Hopper, pos cube.Pos, tx *world.Tx) bool {
	p = p.withLoot(pos, tx)
	for sourceSlot, sourceStack := range h.inventory.Slots() {
		if !sourceStack.Empty() && sourceStack.Comparable(p.Item) && p.Item.Count() < p.Item.MaxCount() {
			if p.Item.Empty() {
				p.Item = sourceStack.Grow(-sourceStack.Count() + 1)
			} else {
				p.Item = p.Item.Grow(1)
			}
			_ = h.inventory.SetItem(sourceSlot, sourceStack.Grow(-1))
			tx.SetBlock(pos, p, nil)
			return true
		}
	}
	return false
}

// wobble ...
func (p DecoratedPot) wobble(pos cube.Pos, tx *world.Tx, success bool) {
	viewers := tx.Viewers(pos.Vec3Centre())
	for _, v := range viewers {
		v.ViewBlockAction(pos, DecoratedPotWobbleAction{DecoratedPot: p, Success: success})
	}
	tx.ReleaseViewers(viewers)

	if success {
		tx.AddParticle(pos.Vec3Middle().Add(mgl64.Vec3{0, 1.2}), particle.DustPlume{})
		tx.PlaySound(pos.Vec3Centre(), sound.DecoratedPotInserted{Progress: float64(p.Item.Count()) / float64(p.Item.MaxCount())})
	} else {
		tx.PlaySound(pos.Vec3Centre(), sound.DecoratedPotInsertFailed{})
	}
}

// Activate ...
func (p DecoratedPot) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	p = p.withLoot(pos, tx)
	held, _ := u.HeldItems()
	if held.Empty() || !p.Item.Comparable(held) || p.Item.Count() == p.Item.MaxCount() {
		p.wobble(pos, tx, false)
		return false
	}

	if p.Item.Empty() {
		p.Item = held.Grow(-held.Count() + 1)
	} else {
		p.Item = p.Item.Grow(1)
	}
	tx.SetBlock(pos, p, nil)
	p.wobble(pos, tx, true)
	ctx.SubtractFromCount(1)
	return true
}

// BreakInfo ...
func (p DecoratedPot) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(DecoratedPot{Decorations: p.Decorations})).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		if p := p.withLoot(pos, tx); !p.Item.Empty() {
			dropItem(tx, p.Item, pos.Vec3Centre())
		}
	})
}

// EncodeItem ...
func (p DecoratedPot) EncodeItem() (name string, meta int16) {
	return "minecraft:decorated_pot", 0
}

// EncodeBlock ...
func (p DecoratedPot) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:decorated_pot", map[string]any{"direction": int32(horizontalDirection(p.Facing))}
}

// Model ...
func (p DecoratedPot) Model() world.BlockModel {
	return model.DecoratedPot{}
}

// UseOnBlock ...
func (p DecoratedPot) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, p)
	if !used {
		return
	}
	p.Facing = user.Rotation().Direction().Opposite()

	place(tx, pos, p, user, ctx)
	return placed(ctx)
}

// EncodeNBT ...
func (p DecoratedPot) EncodeNBT() map[string]any {
	var sherds []any
	for _, decoration := range p.Decorations {
		if decoration == nil {
			sherds = append(sherds, "minecraft:brick")
		} else {
			name, _ := decoration.EncodeItem()
			sherds = append(sherds, name)
		}
	}

	// BDS saves the item even when the pot is empty, as an item named "".
	m := map[string]any{
		"id":        "DecoratedPot",
		"sherds":    sherds,
		"animation": uint8(0),
		"item":      map[string]any{"Name": "", "Count": uint8(0), "Damage": int16(0), "WasPickedUp": uint8(0)},
	}
	if !p.Item.Empty() {
		m["item"] = item.WriteNBT(p.Item, true)
	}
	if p.lootTable != "" {
		m["LootTable"], m["LootTableSeed"] = loot.BedrockTablePath(p.lootTable), p.lootSeed
	}
	return m
}

// DecodeNBT ...
func (p DecoratedPot) DecodeNBT(data map[string]any) any {
	p.Item = item.MapNBT(data, "item")
	p.lootTable = loot.JavaTableKey(nbtconv.String(data, "LootTable"))
	p.lootSeed, _ = nbtconv.Integer(data, "LootTableSeed")
	p.Decorations = [4]PotDecoration{}
	if sherds := nbtconv.Slice(data, "sherds"); sherds != nil {
		// Ignore invalid or extra entries to avoid crashing on malformed NBT.
		for i, name := range sherds {
			if i >= len(p.Decorations) {
				break
			}
			name, ok := name.(string)
			if !ok || name == "" {
				continue
			}
			it, ok := world.ItemByName(name, 0)
			if !ok {
				continue
			}
			decoration, ok := it.(PotDecoration)
			if !ok {
				continue
			}
			p.Decorations[i] = decoration
		}
	}
	return p
}

// allDecoratedPots ...
func allDecoratedPots() (pots []world.Block) {
	for _, f := range cube.Directions() {
		pots = append(pots, DecoratedPot{Facing: f})
	}
	return
}
