package player

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// itemUseState is the item the player is using and the hook it fishes with.
type itemUseState struct {
	usingItem                bool
	blockingDelayTicks       int64
	usingSince               time.Time
	usingTicks               int64
	usingStack               item.Stack
	kineticRecent            map[*world.EntityHandle]int64
	usingSlot                uint32
	itemUseTick              int64
	itemUseCompleteTick      int64
	itemUseCompletedStack    item.Stack
	itemUseCompletedSlot     uint32
	itemUseRecentlyCompleted bool
	fishingHook              *world.EntityHandle
}

// useBegin records the start of the use of the stack held in the slot passed.
func (s *itemUseState) useBegin(stack item.Stack, slot uint32) {
	s.usingItem = true
	s.usingSince = time.Now()
	s.usingTicks = 0
	s.usingStack = stack
	if _, ok := stack.Item().(item.KineticWeapon); ok {
		if s.kineticRecent == nil {
			s.kineticRecent = make(map[*world.EntityHandle]int64)
		}
		clear(s.kineticRecent)
	}
	s.usingSlot = slot
	s.itemUseRecentlyCompleted = false
}

// useFinish ends the use of the item and forgets what was used.
func (s *itemUseState) useFinish() {
	s.usingItem = false
	s.usingTicks = 0
	s.usingStack = item.Stack{}
	clear(s.kineticRecent)
}

// useMarkCompleted remembers that the use of the stack held in the slot passed
// completed in the tick of the last use tick.
func (s *itemUseState) useMarkCompleted(held item.Stack, slot uint32) {
	s.itemUseCompleteTick = s.itemUseTick
	s.itemUseCompletedStack = held
	s.itemUseCompletedSlot = slot
	s.itemUseRecentlyCompleted = true
}

// useMatches reports whether the stack in use is the stack passed, held in the
// slot passed.
func (s *itemUseState) useMatches(slot uint32, stack item.Stack) bool {
	return s.usingSlot == slot && s.usingStack.Equal(stack)
}

// useRecentlyCompleted reports whether the use of the stack passed, held in the
// slot passed, completed within the last two ticks. A completion that no
// longer matches, or has aged, is forgotten.
func (s *itemUseState) useRecentlyCompleted(slot uint32, stack item.Stack) bool {
	if !s.itemUseRecentlyCompleted {
		return false
	}
	if s.itemUseCompletedSlot != slot || !s.itemUseCompletedStack.Equal(stack) {
		s.itemUseRecentlyCompleted = false
		return false
	}
	if ticks := s.itemUseTick - s.itemUseCompleteTick; ticks >= 0 && ticks <= 2 {
		// Ignore the completion packet that commonly follows server-side completion within 100ms.
		return true
	}
	s.itemUseRecentlyCompleted = false
	return false
}

// useElapsed returns the duration the item has been in use: the longer of the
// time and of the ticks.
func (s *itemUseState) useElapsed() time.Duration {
	return max(time.Since(s.usingSince)+time.Second/20, time.Duration(s.usingTicks)*time.Second/20)
}

// useCountTick counts a tick of the use of the item, which is the tick passed.
func (s *itemUseState) useCountTick(current int64) {
	s.itemUseTick = current
	s.usingTicks++
}

// IsFishing reports if the player currently has an active fishing hook.
func (p *Player) IsFishing() bool {
	if p.fishingHook == nil {
		return false
	}
	if _, ok := p.fishingHook.Entity(p.tx); !ok {
		p.fishingHook = nil
		return false
	}
	return true
}

// StartFishing casts a new fishing hook if possible.
func (p *Player) StartFishing(tx *world.Tx, rod item.Stack) bool {
	if p.fishingHook != nil {
		if _, ok := p.fishingHook.Entity(tx); ok {
			return false
		}
		p.fishingHook = nil
	}
	create := tx.World().EntityRegistry().Config().FishingHook
	if create == nil {
		return false
	}
	// FishingHook(Player, ...): the hook starts 0.3 to the side of the eyes
	// and flies where the player looks at about 0.6, the vertical part of
	// its direction clamped to five times the horizontal.
	yaw, pitch := mgl64.DegToRad(p.Rotation().Yaw()), mgl64.DegToRad(p.Rotation().Pitch())
	yCos, ySin := math.Cos(-yaw-math.Pi), math.Sin(-yaw-math.Pi)
	xCos, xSin := -math.Cos(-pitch), math.Sin(-pitch)
	pos := mgl64.Vec3{p.Position()[0] - ySin*0.3, p.Position()[1] + p.EyeHeight(), p.Position()[2] - yCos*0.3}
	dir := mgl64.Vec3{-ySin, max(min(-(xSin/xCos), 5), -5), -yCos}
	dist := dir.Len()
	triangle := func() float64 { return 0.5 + 0.0103365*(rand.Float64()-rand.Float64()) }
	velocity := mgl64.Vec3{dir[0] * (0.6/dist + triangle()), dir[1] * (0.6/dist + triangle()), dir[2] * (0.6/dist + triangle())}
	handle := create(world.EntitySpawnOpts{Position: pos, Velocity: velocity}, p, rod)
	tx.AddEntity(handle)
	p.fishingHook = handle
	return true
}

// StopFishing stops fishing for the player and optionally reels in the
// hook, as FishingHook.retrieve. It returns the durability the rod loses.
func (p *Player) StopFishing(tx *world.Tx, reel bool) int {
	if p.fishingHook == nil {
		return 0
	}
	handle := p.fishingHook
	p.fishingHook = nil
	ent, ok := handle.Entity(tx)
	if !ok {
		_ = handle.Close()
		return 0
	}
	hook, ok := ent.(*entity.Ent)
	if !ok {
		_ = ent.Close()
		return 0
	}
	behaviour, ok := hook.Behaviour().(*entity.FishingHookBehaviour)
	if !ok || !reel {
		_ = hook.CloseIn(tx)
		return 0
	}
	damage, err := behaviour.Retrieve(hook, tx)
	if err != nil {
		p.log.Error("failed to resolve Java fishing loot", "err", err)
	}
	return damage
}

// UseItem uses the item currently held in the player's main hand in the air. Generally, nothing happens,
// unless the held item implements the item.Usable interface, in which case it will be activated.
// This generally happens for items such as throwable items like snowballs.
func (p *Player) UseItem() {
	if !p.GameMode().AllowsInteraction() {
		return
	}
	i, _ := p.HeldItems()
	ctx := NewEventContext(p.tx, p)
	if p.HasCooldown(i.Item()) {
		return
	}
	if p.Handler().HandleItemUse(ctx); ctx.Cancelled() {
		return
	}
	i, left := p.HeldItems()
	if p.recentlyCompletedItemUse(i) {
		return
	}
	if p.usingItem {
		if p.itemUseMatches(i) {
			// Use packets may be repeated while an item is held. Completion is handled by Tick.
			return
		}
		p.cancelItemUse()
	}
	it := i.Item()

	if cd, ok := it.(item.Cooldown); ok {
		if _, consumable := it.(item.Consumable); !consumable {
			p.SetCooldown(it, cd.Cooldown())
		}
	}

	if _, ok := it.(item.Releasable); ok {
		if !p.canRelease() {
			return
		}
		p.startItemUse(i)
	}
	switch usable := it.(type) {
	case item.Chargeable:
		p.useChargeable(i, usable)
	case item.KineticWeapon:
		p.useKineticWeapon(i, it)
	case item.Usable:
		p.useUsable(i, left, usable)
	case item.Consumable:
		if p.canConsume(usable) {
			p.startItemUse(i)
		}
	}
}

// useChargeable uses a chargeable item i in the air, such as a crossbow: it releases the charge it holds, or
// starts to charge.
func (p *Player) useChargeable(i item.Stack, usable item.Chargeable) {
	useCtx := p.useContext()
	if usable.ReleaseCharge(p, p.tx, useCtx) {
		p.handleUseContext(useCtx)
		p.updateState()
		return
	}
	if usable.CanCharge(p, p.tx, useCtx) {
		p.startItemUse(i)
	}
	p.handleUseContext(useCtx)
}

// useKineticWeapon starts the use of a kinetic weapon i, a spear, which plays the sound of its use.
func (p *Player) useKineticWeapon(i item.Stack, it world.Item) {
	p.startItemUse(i)
	if spear, ok := it.(item.Spear); ok {
		name := "item.spear.use"
		if spear.Tier == item.ToolTierWood {
			name = "item.spear_wood.use"
		}
		p.PlaySound(sound.Custom{Name: name, Volume: 1, Pitch: 1})
	}
}

// useUsable uses a usable item i in the air, such as a snowball. left is the item in the off hand. An item that
// does nothing may still be consumed, as a milk bucket is.
func (p *Player) useUsable(i, left item.Stack, usable item.Usable) {
	it := i.Item()
	if _, cooldown := it.(item.Cooldown); !cooldown && p.refuseFastUse(it) {
		p.resyncInventory()
		return
	}
	useCtx := p.useContext()
	if !usable.Use(p.tx, p, useCtx) {
		// An item used in no other way may still be consumed, as a milk
		// bucket that is not pouring anything.
		if consumable, ok := it.(item.Consumable); ok && p.canConsume(consumable) {
			p.startItemUse(i)
		}
		return
	}
	if _, ok := it.(item.FishingRod); ok {
		event := world.GameEventItemInteractFinish
		if p.IsFishing() {
			event = world.GameEventItemInteractStart
		}
		p.emitGameEvent(event, p.Position(), nil)
	}
	// We only swing the player's arm if the item held actually does something. If it doesn't, there is no
	// reason to swing the arm.
	p.SwingArm()
	p.SetHeldItems(p.subtractItem(p.damageItem(i, useCtx.Damage), useCtx.CountSub), left)
	p.addNewItem(useCtx)
}

// ReleaseItem makes the Player release the item it is currently using. This is only applicable for items that
// implement the item.Releasable interface.
// If the Player is not currently using any item, ReleaseItem returns immediately.
// ReleaseItem either aborts the using of the item or finished it, depending on the time that elapsed since
// the item started being used.
func (p *Player) ReleaseItem() {
	if !p.usingItem {
		p.resyncInventory()
		return
	}
	i, _ := p.HeldItems()
	if !p.itemUseMatches(i) {
		p.cancelItemUse()
		p.resyncInventory()
		return
	}
	dur := p.useDuration()
	p.cancelItemUse()
	if _, kinetic := i.Item().(item.KineticWeapon); kinetic {
		ctx := NewEventContext(p.tx, p)
		if p.Handler().HandleItemRelease(ctx, i, dur); ctx.Cancelled() {
			p.resyncInventory()
			return
		}
		p.completeItemUse()
		return
	}
	if !p.canRelease() || !p.GameMode().AllowsInteraction() {
		p.resyncInventory()
		return
	}

	useCtx := p.useContext()
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleItemRelease(ctx, i, dur); ctx.Cancelled() {
		p.resyncInventory()
		return
	}
	i.Item().(item.Releasable).Release(p, p.tx, useCtx, dur)
	p.handleUseContext(useCtx)
	if len(useCtx.ConsumedItems) == 0 {
		// Revert the item the client predicted the release would consume.
		p.resyncInventory()
	}
}

func (p *Player) startItemUse(stack item.Stack) {
	p.useBegin(stack, atomic.LoadUint32(p.heldSlot))
	p.emitGameEvent(world.GameEventItemInteractStart, p.Position(), nil)
	p.updateState()
}

func (p *Player) cancelItemUse() {
	if !p.usingItem {
		return
	}
	p.useFinish()
	p.emitGameEvent(world.GameEventItemInteractFinish, p.Position(), nil)
	p.updateState()
}

func (p *Player) completeItemUse() {
	p.useFinish()
	p.emitGameEvent(world.GameEventItemInteractFinish, p.Position(), nil)
	held, _ := p.HeldItems()
	p.useMarkCompleted(held, atomic.LoadUint32(p.heldSlot))
	p.updateState()
}

func (p *Player) itemUseMatches(stack item.Stack) bool {
	return p.useMatches(atomic.LoadUint32(p.heldSlot), stack)
}

func (p *Player) recentlyCompletedItemUse(stack item.Stack) bool {
	return p.useRecentlyCompleted(atomic.LoadUint32(p.heldSlot), stack)
}

func (p *Player) canConsume(consumable item.Consumable) bool {
	if c, ok := consumable.(interface{ CanConsume() bool }); ok && !c.CanConsume() {
		return false
	}
	return consumable.AlwaysConsumable() || !p.GameMode().AllowsTakingDamage() || p.Food() < 20
}

func (p *Player) tryConsumeItem(consumable item.Consumable, stack item.Stack) {
	if !p.canConsume(consumable) {
		p.cancelItemUse()
		return
	}
	if p.useDuration() < consumable.ConsumeDuration() {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleItemConsume(ctx, stack); ctx.Cancelled() {
		p.completeItemUse()
		return
	}
	useCtx := p.useContext()
	useCtx.CountSub, useCtx.NewItem = 1, consumable.Consume(p.tx, p)
	p.handleUseContext(useCtx)
	if cd, ok := stack.Item().(item.Cooldown); ok {
		// The use_cooldown of a consumable starts once it is consumed.
		p.SetCooldown(stack.Item(), cd.Cooldown())
	}
	gameEvent := world.GameEventEat
	switch consumed := stack.Item().(type) {
	case item.Potion:
		gameEvent = world.GameEventDrink
	case item.HoneyBottle, item.OminousBottle:
		gameEvent = world.GameEventDrink
	case item.Bucket:
		if consumed.Content.String() == "milk" {
			gameEvent = world.GameEventDrink
		}
	}
	p.emitGameEvent(gameEvent, p.Position(), nil)
	p.tx.PlaySound(p.Position().Add(mgl64.Vec3{0, 1.5}), sound.Burp{})
	p.completeItemUse()
}

func (p *Player) tryChargeItem(chargeable item.Chargeable) {
	useCtx, dur := p.useContext(), p.useDuration()
	chargeable.ContinueCharge(p, p.tx, useCtx, dur)
	completed := chargeable.Charge(p, p.tx, useCtx, dur)
	p.handleUseContext(useCtx)
	if !completed {
		return
	}
	p.client().SendChargeItemComplete()
	p.completeItemUse()
}

// canRelease returns whether the player can release the item currently held in the main hand.
func (p *Player) canRelease() bool {
	held, left := p.HeldItems()
	releasable, ok := held.Item().(item.Releasable)
	if !ok {
		return false
	}
	if p.GameMode().CreativeInventory() {
		return true
	}
	for _, req := range releasable.Requirements() {
		reqName, _ := req.Item().EncodeItem()

		if !left.Empty() {
			leftName, _ := left.Item().EncodeItem()
			if leftName == reqName {
				continue
			}
		}

		_, found := p.Inventory().FirstFunc(func(stack item.Stack) bool {
			name, _ := stack.Item().EncodeItem()
			return name == reqName
		})
		if !found {
			return false
		}
	}
	return true
}

// handleUseContext handles the item.UseContext after the item has been used.
func (p *Player) handleUseContext(ctx *item.UseContext) {
	i, left := p.HeldItems()

	p.SetHeldItems(p.subtractItem(p.damageItem(i, ctx.Damage), ctx.CountSub), left)
	p.addNewItem(ctx)
	for _, it := range ctx.ConsumedItems {
		_, offHand := p.HeldItems()
		if offHand.Comparable(it) {
			if err := p.offHand.RemoveItem(it); err == nil {
				continue
			}

			it = it.Grow(-offHand.Count())
		}

		_ = p.Inventory().RemoveItem(it)
	}
}

// useDuration returns the duration the player has been using the item in the main hand.
func (p *Player) useDuration() time.Duration {
	return p.useElapsed()
}

// UsingItem checks if the Player is currently using an item. True is returned if the Player is currently eating an
// item or using it over a longer duration such as when using a bow.
func (p *Player) UsingItem() bool {
	return p.usingItem
}

// UseItemOnBlock uses the item held in the main hand of the player on a block at the position passed. The
// player is assumed to have clicked the face passed with the relative click position clickPos.
// If the item could not be used successfully, for example when the position is out of range, the method
// returns immediately.
// UseItemOnBlock does nothing if the block at the cube.Pos passed is of the type block.Air.
func (p *Player) UseItemOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3) {
	if _, ok := p.tx.Block(pos).(block.Air); ok || !p.canReachBlock(pos) || p.refuseUseOnBlock(pos, face, clickPos) {
		// The client used its item on a block that does not exist server-side or one it couldn't reach. Stop trying
		// to use the item immediately.
		p.resendNearbyBlocks(pos, face)
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleItemUseOnBlock(ctx, pos, face, clickPos); ctx.Cancelled() {
		p.resendNearbyBlocks(pos, face)
		return
	}
	i, left := p.HeldItems()
	b := p.tx.Block(pos)
	if p.activateBlock(pos, face, clickPos, b, i, left) || i.Empty() {
		return
	}
	switch ib := i.Item().(type) {
	case item.UsableOnBlock:
		p.useOnBlock(pos, face, clickPos, i, left, ib)
	case world.Block:
		p.placeHeldBlock(pos, face, b, i, left, ib)
	}
}

// activateBlock activates the block b at pos that the player clicked with the item i of its main hand, as
// Block.useItemOn does, and returns true if it was activated. left is the item of the off hand. A sneaking
// player holding an item does not activate the block, unless it depends on the hit.
func (p *Player) activateBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, b world.Block, i, left item.Stack) bool {
	// Hit-dependent blocks are given the exact packet click position before ordinary
	// activation or item use, so they can choose a precise slot.
	hitAct, hitDependent := b.(block.HitActivatable)
	if !hitDependent && p.Sneaking() && !i.Empty() {
		return false
	}
	useCtx := p.useContext()
	activated := false
	p.tx.WithBlockInteraction(func() {
		if hitDependent {
			activated = hitAct.ActivateAt(pos, face, clickPos, p.tx, p, useCtx)
		} else if act, ok := b.(block.Activatable); ok {
			activated = act.Activate(pos, face, p.tx, p, useCtx)
		}
	})
	if activated {
		p.SwingArm()
		if useCtx.HeldItemSet {
			return true
		}
		if useCtx.ReplaceHeldItem && !useCtx.NewItem.Empty() {
			p.SetHeldItems(useCtx.NewItem, left)
			return true
		}
		p.SetHeldItems(p.subtractItem(p.damageItem(i, useCtx.Damage), useCtx.CountSub), left)
		p.addNewItem(useCtx)
		return true
	}
	if bell, ok := b.(block.Bell); ok {
		p.client().ViewBlockAction(pos, block.BellUseRejectedAction{Bell: bell})
	}
	return false
}

// useOnBlock uses an item i that does something when used on a block, such as a hoe, on the block at pos.
func (p *Player) useOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, i, left item.Stack, ib item.UsableOnBlock) {
	useCtx := p.useContext()
	if !ib.UseOnBlock(pos, face, clickPos, p.tx, p, useCtx) {
		return
	}
	if _, ok := i.Item().(item.BoneMeal); ok {
		p.emitGameEvent(world.GameEventItemInteractFinish, p.Position(), nil)
	}
	p.SwingArm()
	p.SetHeldItems(p.subtractItem(p.damageItem(i, useCtx.Damage), useCtx.CountSub), left)
	p.addNewItem(useCtx)
}

// placeHeldBlock places the block ib, which is the item i, against the block b at pos, in the replaceable
// block there or the one at its face.
func (p *Player) placeHeldBlock(pos cube.Pos, face cube.Face, b world.Block, i, left item.Stack, ib world.Block) {
	replacedPos := pos
	if replaceable, ok := b.(block.Replaceable); !ok || !replaceable.ReplaceableBy(ib) {
		// The block clicked was either not replaceable, or not replaceable using the block passed.
		replacedPos = pos.Side(face)
	}
	if replaceable, ok := p.tx.Block(replacedPos).(block.Replaceable); !ok || !replaceable.ReplaceableBy(ib) || replacedPos.OutOfBounds(p.tx.Range()) {
		return
	}
	if p.refusePlaceRate(replacedPos) {
		p.resendNearbyBlocks(replacedPos, cube.Faces()...)
		return
	}
	if !p.placeBlock(replacedPos, ib, false) {
		return
	}
	p.emitGameEvent(world.GameEventBlockPlace, replacedPos.Vec3Centre(), p.tx.Block(replacedPos))
	if p.GameMode().CreativeInventory() {
		return
	}
	p.SetHeldItems(p.subtractItem(i, 1), left)
}

// UseItemOnEntity uses the item held in the main hand of the player on the entity passed, provided it is
// within range of the player.
// If the item held in the main hand of the player does nothing when used on an entity, nothing will happen.
func (p *Player) UseItemOnEntity(e world.Entity) bool {
	ac, end := p.beginAction()
	defer end()
	if !p.canReachEntity(ac, e, nil) || p.roleRefusesEntity(e) {
		return false
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleItemUseOnEntity(ctx, e); ctx.Cancelled() {
		return false
	}
	i, left := p.HeldItems()
	useCtx := p.useContext()

	if usable, ok := i.Item().(item.UsableOnEntity); ok {
		if usable.UseOnEntity(e, p.tx, p, useCtx) {
			if useCtx.ReplaceHeldItem && !useCtx.NewItem.Empty() && i.Count() == 1 {
				p.SetHeldItems(useCtx.NewItem, left)
				p.emitGameEvent(world.GameEventEntityInteract, e.Position(), nil)
				return true
			}
			p.SetHeldItems(p.subtractItem(p.damageItem(i, useCtx.Damage), useCtx.CountSub), left)
			p.addNewItem(useCtx)
			p.emitGameEvent(world.GameEventEntityInteract, e.Position(), nil)
			return true
		}
	}

	used := false
	if acceptor, ok := e.(entity.ItemAcceptor); ok {
		if acceptor.AcceptItem(p, p.tx, useCtx) {
			used = true
		}
	}
	if !used {
		if interactable, ok := e.(entity.Interactable); ok {
			if interactable.Interact(p.tx, p, useCtx) {
				used = true
			}
		}
	}
	if !used {
		return true
	}
	p.emitGameEvent(world.GameEventEntityInteract, e.Position(), nil)
	p.SwingArm()
	p.SetHeldItems(p.subtractItem(p.damageItem(i, useCtx.Damage), useCtx.CountSub), left)
	p.addNewItem(useCtx)
	return true
}

func (p *Player) tickItemUse(current int64) {
	p.useCountTick(current)
	held, _ := p.HeldItems()
	if !p.itemUseMatches(held) {
		p.cancelItemUse()
		return
	}
	switch usable := held.Item().(type) {
	case item.Consumable:
		if current%4 == 0 {
			viewers, release := p.viewers()
			for _, v := range viewers {
				v.ViewEntityAction(p, entity.EatAction{})
			}
			releaseBorrowedViewers(p.tx, viewers, release)
		}
		p.tryConsumeItem(usable, held)
	case item.Chargeable:
		p.tryChargeItem(usable)
	case item.KineticWeapon:
		p.tryKineticWeapon(usable)
	case item.UseTickable:
		updated := usable.UseTick(p.tx, p, held, int(p.usingTicks))
		if !held.Equal(updated) {
			_, left := p.HeldItems()
			p.SetHeldItems(updated, left)
		}
		p.usingStack = updated
		if duration := usable.UseDurationTicks(); duration > 0 && p.usingTicks >= int64(duration) {
			p.completeItemUse()
		}
	}
}

// useContext returns an item.UseContext initialised for a Player.
func (p *Player) useContext() *item.UseContext {
	call := func(ctx *inventory.Context, slot int, it item.Stack, f func(ctx *inventory.Context, slot int, it item.Stack)) error {
		if ctx.Cancelled() {
			return fmt.Errorf("action was cancelled")
		}
		f(ctx, slot, it)
		if ctx.Cancelled() {
			return fmt.Errorf("action was cancelled")
		}
		return nil
	}
	return &item.UseContext{
		SwapHeldWithArmour: func(i int) {
			src, dst, srcInv, dstInv := int(atomic.LoadUint32(p.heldSlot)), i, p.inv, p.armour.Inventory()
			srcIt, _ := srcInv.Item(src)
			dstIt, _ := dstInv.Item(dst)
			if srcIt.LockMode() == item.LockInSlot || dstIt.LockMode() == item.LockInSlot {
				return
			}

			ctx := event.C(inventory.Holder(p))
			_ = call(ctx, src, srcIt, srcInv.Handler().HandleTake)
			_ = call(ctx, src, dstIt, srcInv.Handler().HandlePlace)
			_ = call(ctx, dst, dstIt, dstInv.Handler().HandleTake)
			if err := call(ctx, dst, srcIt, dstInv.Handler().HandlePlace); err == nil {
				_ = srcInv.SetItem(src, dstIt)
				_ = dstInv.SetItem(dst, srcIt)
				p.PlaySound(sound.EquipItem{Item: srcIt.Item()})
			}
		},
		FirstFunc: func(comparable func(item.Stack) bool) (item.Stack, bool) {
			_, left := p.HeldItems()
			if !left.Empty() && comparable(left) {
				return left, true
			}
			inv := p.Inventory()
			s, ok := inv.FirstFunc(comparable)
			if !ok {
				return item.Stack{}, false
			}
			it, _ := inv.Item(s)
			return it, ok
		},
	}
}
