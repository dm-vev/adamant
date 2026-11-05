package player

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// MoveItemsToInventory moves items kept in 'temporary' slots, such as the
// crafting grid of slots in an enchantment table, to the player's inventory.
// If no space is left for these items, the leftover items are dropped.
func (p *Player) MoveItemsToInventory() {
	for _, i := range p.ui.Clear() {
		if n, err := p.inv.AddItem(i); err != nil {
			// We couldn't add the item to the main inventory (probably because
			// it was full), so we drop it instead.
			p.Drop(i.Grow(-n))
		}
	}
}

// Inventory returns the inventory of the player. This inventory holds the items stored in the normal part of
// the inventory and the hotbar. It also includes the item in the main hand as returned by Player.HeldItems().
func (p *Player) Inventory() *inventory.Inventory {
	return p.inv
}

// Armour returns the armour inventory of the player. This inventory yields 4 slots, for the helmet,
// chestplate, leggings and boots respectively.
func (p *Player) Armour() *inventory.Armour {
	return p.armour
}

// HeldItems returns the items currently held in the hands of the player. The first item stack returned is the
// one held in the main hand, the second is held in the off-hand.
// If no item was held in a hand, the stack returned has a count of 0. Stack.Empty() may be used to check if
// the hand held anything.
func (p *Player) HeldItems() (mainHand, offHand item.Stack) {
	offHand, _ = p.offHand.Item(0)
	heldSlot := int(atomic.LoadUint32(p.heldSlot))
	mainHand, _ = p.inv.Item(heldSlot)
	return mainHand, offHand
}

// SetHeldItems sets items to the main hand and the off-hand of the player. The Stacks passed may be empty
// (Stack.Empty()) to clear the held item.
func (p *Player) SetHeldItems(mainHand, offHand item.Stack) {
	heldSlot := int(atomic.LoadUint32(p.heldSlot))
	_ = p.inv.SetItem(heldSlot, mainHand)
	_ = p.offHand.SetItem(0, offHand)
}

// SetHeldSlot updates the held slot of the player to the slot provided. The
// slot must be between 0 and 8.
func (p *Player) SetHeldSlot(to int) error {
	// The slot that the player might have selected must be within the hotbar:
	// The held item cannot be in a different place in the inventory.
	if to < 0 || to > 8 {
		return fmt.Errorf("held slot exceeds hotbar range 0-8: slot is %v", to)
	}
	from := int(atomic.LoadUint32(p.heldSlot))
	if from == to {
		// Old slot was the same as new slot, so don't do anything.
		return nil
	}

	ctx := NewEventContext(p.tx, p)
	p.Handler().HandleHeldSlotChange(ctx, from, to)
	if ctx.Cancelled() {
		// The slot change was cancelled, resend held slot.
		p.client().SendHeldSlot(from, p, true)
		return nil
	}
	atomic.StoreUint32(p.heldSlot, uint32(to))
	p.cancelItemUse()
	p.itemUseRecentlyCompleted = false

	viewers, release := p.viewers()
	for _, viewer := range viewers {
		viewer.ViewEntityItems(p)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
	p.client().SendHeldSlot(to, p, false)
	return nil
}

// EnderChestInventory returns the player's ender chest inventory. Its accessed by the player when opening
// ender chests anywhere.
func (p *Player) EnderChestInventory() *inventory.Inventory {
	return p.enderChest
}

// HasCooldown returns true if the item passed has an active cooldown, meaning it currently cannot be used again. If the
// world.Item passed is nil, HasCooldown always returns false.
func (p *Player) HasCooldown(item world.Item) bool {
	if item == nil {
		return false
	}
	name, _ := item.EncodeItem()
	now := time.Now()
	p.cooldownsMu.Lock()
	defer p.cooldownsMu.Unlock()

	otherTime, ok := p.cooldowns[name]
	if !ok {
		return false
	}
	if now.After(otherTime) {
		delete(p.cooldowns, name)
		return false
	}
	return true
}

// SetCooldown sets a cooldown for an item. If the world.Item passed is nil, nothing happens.
func (p *Player) SetCooldown(item world.Item, cooldown time.Duration) {
	if item == nil {
		return
	}
	name, _ := item.EncodeItem()
	p.cooldownsMu.Lock()
	p.cooldowns[name] = time.Now().Add(cooldown)
	p.cooldownsMu.Unlock()
	p.client().ViewItemCooldown(item, cooldown)
}

// resyncInventory corrects a client-side release prediction that did not consume an item.
func (p *Player) resyncInventory() {
	p.client().ResyncInventory()
}

// Collect makes the player collect the item stack passed, adding it to the inventory. The amount of items that could
// be added is returned.
func (p *Player) Collect(s item.Stack) (int, bool) {
	if p.Dead() || !p.GameMode().AllowsInteraction() {
		return 0, false
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleItemPickup(ctx, &s); ctx.Cancelled() {
		return 0, false
	}
	var added int
	if _, offHand := p.HeldItems(); !offHand.Empty() && offHand.Comparable(s) {
		added, _ = p.offHand.AddItem(s)
	}
	if s.Count() != added {
		n, _ := p.Inventory().AddItem(s.Grow(-added))
		added += n
	}
	return added, true
}

// Drop makes the player drop the item.Stack passed as an entity.Item, so that it may be picked up from the
// ground.
// The dropped item entity has a pickup delay of 2 seconds.
// The number of items that was dropped in the end is returned. It is generally the count of the stack passed
// or 0 if dropping the item.Stack was cancelled.
func (p *Player) Drop(s item.Stack) int {
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleItemDrop(ctx, s); ctx.Cancelled() {
		return 0
	}
	opts := world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, 1.4}), Velocity: p.Rotation().Vec3().Mul(0.4)}
	p.tx.AddEntity(entity.NewItemPickupDelay(opts, s, time.Second*2))
	return s.Count()
}

// DropItemAt spawns an item stack at an explicit position and velocity without treating it as a player drop.
func (p *Player) DropItemAt(pos mgl64.Vec3, stack item.Stack, velocity mgl64.Vec3) {
	p.tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos, Velocity: velocity}, stack))
}

// OpenBlockContainer opens the menu of the block at the position passed: its
// container, such as that of a chest, or its screen, such as that of a
// crafting table, which the client of the player is shown. A menu the player
// had open closes first, unless it is open for that position already.
// OpenBlockContainer does nothing if the player has no client.
func (p *Player) OpenBlockContainer(pos cube.Pos, tx *world.Tx) {
	switch tx.Block(pos).(type) {
	case block.Chest, block.TrappedChest, block.CopperChest, block.EnderChest, block.Barrel, block.ShulkerBox:
		// The blocks of these containers anger the piglins that see a
		// player open them, as PiglinAi.angerNearbyPiglins.
		entity.AngerNearbyPiglins(tx, p, true)
	}
	if !p.hasClient() {
		return
	}
	if m := p.Menu(); m != nil && m.Pos() == pos && p.menuWorld == tx.World() {
		p.logMenuIgnored("block", pos)
		return
	}
	p.closeMenu(tx, false)
	m := menu.OpenBlock(tx, pos, p.c, p.enderChest)
	if m.Kind() != menu.Lectern {
		// The client sends no close for a lectern: the player keeps no
		// menu open for it.
		p.menu.Store(m)
		p.menuWorld = tx.World()
	}
	p.c.OpenMenu(m, tx)
}

// OpenEntityContainer opens inv, the inventory of the entity e, such as a
// chest minecart, as a container of the kind passed. A menu the player had
// open closes first, unless it is open for e already. It does nothing if
// the player has no client.
func (p *Player) OpenEntityContainer(e world.Entity, inv *inventory.Inventory, kind entity.ContainerKind, tx *world.Tx) {
	if _, mount := e.(*entity.Mount); !mount {
		// ContainerEntity.interactWithContainerVehicle angers piglins.
		entity.AngerNearbyPiglins(tx, p, true)
	}
	if !p.hasClient() {
		return
	}
	if m := p.Menu(); m != nil && m.Entity() == e.H() && p.menuWorld == tx.World() {
		p.logMenuIgnored("entity", e.Position())
		return
	}
	p.closeMenu(tx, false)
	m := menu.OpenEntity(e, inv, kind, p.c)
	p.menu.Store(m)
	p.menuWorld = tx.World()
	p.c.OpenMenu(m, tx)
}

// OpenMerchant opens the trades of merchant, the entity e, and shows the
// client of the player its offers. A menu the player had open closes first,
// unless it is open for e already. It does nothing if the player has no
// client, if the offers cannot be had, or if the merchant does not agree to
// trade.
func (p *Player) OpenMerchant(e world.Entity, merchant entity.Merchant, tx *world.Tx) {
	if !p.hasClient() {
		return
	}
	if m := p.Menu(); m != nil && m.Entity() == e.H() && p.menuWorld == tx.World() {
		p.logMenuIgnored("merchant", e.Position())
		return
	}
	offers, err := merchant.MerchantOffersFor(p)
	if err != nil {
		p.log.Error("failed to load merchant offers", "err", err)
		p.Message("Merchant offers are temporarily unavailable.")
		return
	}
	p.closeMenu(tx, false)
	m, ok := menu.OpenMerchant(e, merchant, p.H())
	if !ok {
		return
	}
	p.menu.Store(m)
	p.menuWorld = tx.World()
	p.c.OpenMenu(m, tx)
	p.c.ViewTrades(m, merchant, offers)
}

// Menu returns the menu the player has open, nil if none. It may be called
// from any goroutine.
func (p *Player) Menu() *menu.Menu {
	return p.menu.Load()
}

// A Player is the owner of the batches of its menus.
var _ menu.Owner = (*Player)(nil)

// BeginMenu starts a batch of operations on the inventories of the player and
// its open menu, whose slots n names as the caller's protocol does. The
// batch runs within the transaction of the player and ends with its Commit
// or Rollback.
func (p *Player) BeginMenu(n menu.Namer) *menu.Tx {
	p.menuTx.Store(true)
	return menu.Begin(menu.Opts{
		Tx:     p.tx,
		Owner:  p,
		Inv:    menu.Inventories{Main: p.inv, OffHand: p.offHand, EnderChest: p.enderChest, UI: p.ui, Armour: p.armour},
		Menu:   p.Menu(),
		Viewer: p.client(),
		Name:   n,
		Log:    p.log,
		Done:   func() { p.menuTx.Store(false) },
		Refuse: p.menuRefusal(),
	})
}

// CloseMenu closes the menu the player has open, if any: as its client
// closed it, or, when clientRequested is false, as the server closes it,
// which the client is shown.
func (p *Player) CloseMenu(clientRequested bool) {
	p.closeMenu(p.tx, clientRequested)
}

// closeMenu closes the menu the player has open in tx, if any: the client
// is shown the close first, and the menu then releases what it held, such
// as the viewer of a chest or the trade of a villager.
func (p *Player) closeMenu(tx *world.Tx, clientRequested bool) {
	m := p.menu.Swap(nil)
	if m == nil {
		return
	}
	p.client().CloseMenu(m, clientRequested)
	m.Close(tx, p.H())
}

// expireCooldowns removes the cooldowns of items that have run out.
func (p *Player) expireCooldowns() {
	now := time.Now()
	p.cooldownsMu.Lock()
	defer p.cooldownsMu.Unlock()
	for it, ti := range p.cooldowns {
		if now.After(ti) {
			delete(p.cooldowns, it)
		}
	}
}

// damageItem damages the item stack passed with the damage passed and returns the new stack. If the item
// broke, a breaking sound is played.
// If the player is not survival, the original stack is returned.
func (p *Player) damageItem(s item.Stack, d int) item.Stack {
	if p.GameMode().CreativeInventory() || d == 0 || s.MaxDurability() == -1 {
		return s
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleItemDamage(ctx, s, &d); ctx.Cancelled() || d <= 0 {
		return s
	}
	if e, ok := s.Enchantment(enchantment.Unbreaking); ok {
		d = enchantment.Unbreaking.Reduce(s.Item(), e.Level(), d)
	}
	if s = s.Damage(d); s.Empty() {
		p.tx.PlaySound(p.Position(), sound.ItemBreak{})
	}
	return s
}

// subtractItem subtracts d from the count of the item stack passed and returns it, if the player is in
// survival or adventure mode.
func (p *Player) subtractItem(s item.Stack, d int) item.Stack {
	if !p.GameMode().CreativeInventory() && d != 0 {
		return s.Grow(-d)
	}
	return s
}

// addNewItem adds the new item of the context passed to the inventory.
func (p *Player) addNewItem(ctx *item.UseContext) {
	if (ctx.NewItemSurvivalOnly && p.GameMode().CreativeInventory()) || ctx.NewItem.Empty() {
		return
	}
	held, left := p.HeldItems()
	if held.Empty() {
		p.SetHeldItems(ctx.NewItem, left)
		return
	}
	n, err := p.Inventory().AddItem(ctx.NewItem)
	if err != nil {
		// Not all items could be added to the inventory, so drop the rest.
		p.Drop(ctx.NewItem.Grow(-n))
	}
	if p.Dead() {
		p.dropItems()
	}
}

// broadcastItems broadcasts the items held to viewers.
func (p *Player) broadcastItems(int, item.Stack, item.Stack) {
	viewers, release := p.viewers()
	// broadcastItems is called for every hotbar mutation. Recycling the borrowed viewer slice avoids flooding the
	// garbage collector during inventory spam.
	for _, viewer := range viewers {
		viewer.ViewEntityItems(p)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// broadcastArmour broadcasts the armour equipped to viewers.
func (p *Player) broadcastArmour(_ int, before, after item.Stack) {
	if before.Comparable(after) && before.Empty() == after.Empty() {
		// Only send armour if the type of the armour changed.
		return
	}
	if p.tx != nil && p.GameMode() != world.GameModeSpectator {
		event := world.GameEventEquip
		if after.Empty() {
			event = world.GameEventUnequip
		}
		p.emitGameEvent(event, p.Position(), nil)
	}
	viewers, release := p.viewers()
	// Armour changes are similarly noisy in PvP encounters; reusing the pooled slice keeps repeated swaps cheap.
	for _, viewer := range viewers {
		viewer.ViewEntityArmour(p)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}
