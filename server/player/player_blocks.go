package player

import (
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// breakingState is the block the player is breaking.
type breakingState struct {
	breaking          bool
	breakingPos       cube.Pos
	breakingFace      cube.Face
	lastBreakDuration time.Duration
	breakCounter      uint32
}

// StartBreaking makes the player start breaking the block at the position passed using the item currently
// held in its main hand.
// If no block is present at the position, or if the block is out of range, StartBreaking will return
// immediately and the block will not be broken. StartBreaking will stop the breaking of any block that the
// player might be breaking before this method is called.
func (p *Player) StartBreaking(pos cube.Pos, face cube.Face) {
	if p.breaking && pos == p.breakingPos {
		// The client continues breaking the block it breaks, as it says
		// with ContinueDestroyBlock: the break keeps its start.
		p.breakingFace = face
		return
	}
	p.AbortBreaking()
	if _, air := p.tx.Block(pos).(block.Air); air || !p.canReachBlock(pos) || !p.Permissions().Allows(client.AbilityMine) {
		// The block was either out of range or air, or the role of the
		// player does not mine, so it can't be broken by the player.
		return
	}
	if _, ok := p.tx.Block(pos.Side(face)).(block.Fire); ok {
		ctx := NewEventContext(p.tx, p)
		if p.Handler().HandleFireExtinguish(ctx, pos); ctx.Cancelled() {
			// Resend the block because on client side that was extinguished
			p.resendNearbyBlocks(pos, face)
			return
		}

		p.tx.SetBlock(pos.Side(face), nil, nil)
		p.tx.PlaySound(pos.Vec3(), sound.FireExtinguish{})
		return
	}

	held, _ := p.HeldItems()
	if _, ok := held.Item().(item.Sword); ok && p.GameMode().CreativeInventory() {
		// Can't break blocks with a sword in creative mode.
		return
	}
	// Note: We intentionally store this regardless of whether the breaking proceeds, so that we
	// can resend the block to the client when it tries to break the block regardless.
	p.breakingPos = pos

	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleStartBreak(ctx, pos); ctx.Cancelled() {
		return
	}
	if punchable, ok := p.tx.Block(pos).(block.Punchable); ok {
		punchable.Punch(pos, face, p.tx, p)
	}

	p.breaking, p.breakingFace = true, face
	p.startBreakTime()
	p.SwingArm()

	if p.GameMode().CreativeInventory() {
		return
	}
	p.lastBreakDuration = p.breakTime(pos)
	viewers, release := p.viewers()
	for _, viewer := range viewers {
		viewer.ViewBlockAction(pos, block.StartCrackAction{BreakTime: p.lastBreakDuration})
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// breakTime returns the time needed to break a block at the position passed, taking into account the item
// held, if the player is on the ground/underwater and if the player has any effects.
func (p *Player) breakTime(pos cube.Pos) time.Duration {
	held, _ := p.HeldItems()
	return block.BreakDuration(p.tx.Block(pos), held, p.breakContext())
}

// breakContext returns the block.BreakContext describing the status effects and environment currently
// affecting how quickly the player breaks blocks.
func (p *Player) breakContext() block.BreakContext {
	_, aquaAffinity := p.Armour().Helmet().Enchantment(enchantment.AquaAffinity)
	ctx := block.BreakContext{
		Underwater:   p.insideOfWater(),
		AquaAffinity: aquaAffinity,
		Airborne:     !p.OnGround(),
		Flying:       p.Flying(),
	}
	if e, ok := p.Effect(effect.Haste); ok {
		ctx.HasteLevel = e.Level()
	}
	if e, ok := p.Effect(effect.ConduitPower); ok {
		ctx.ConduitPowerLevel = e.Level()
	}
	if e, ok := p.Effect(effect.MiningFatigue); ok {
		ctx.MiningFatigueLevel = e.Level()
	}
	return ctx
}

// FinishBreaking makes the player finish breaking the block it is currently breaking, or returns immediately
// if the player isn't breaking anything.
// FinishBreaking will stop the animation and break the block.
//
// A finish the client asks for earlier than the block takes is refused by
// BreakTime of the anti-cheat: the block is resent, or breaks once its time
// is served.
func (p *Player) FinishBreaking() {
	if !p.breaking {
		p.resendNearbyBlock(p.breakingPos)
		return
	}
	if p.finishBreakEarly() {
		return
	}
	p.AbortBreaking()
	p.BreakBlock(p.breakingPos)
}

// AbortBreaking makes the player stop breaking the block it is currently breaking, or returns immediately
// if the player isn't breaking anything.
// Unlike FinishBreaking, AbortBreaking does not stop the animation.
func (p *Player) AbortBreaking() {
	if !p.breaking {
		return
	}
	p.breaking, p.breakCounter = false, 0
	viewers, release := p.viewers()
	for _, viewer := range viewers {
		viewer.ViewBlockAction(p.breakingPos, block.StopCrackAction{})
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// ContinueBreaking makes the player continue breaking the block it started breaking after a call to
// Player.StartBreaking().
// The face passed is used to display particles on the side of the block broken.
func (p *Player) ContinueBreaking(face cube.Face) {
	if !p.breaking {
		return
	}
	if p.continueBreakTime() {
		return
	}
	pos := p.breakingPos
	b := p.tx.Block(pos)
	p.tx.AddParticle(pos.Vec3(), particle.PunchBlock{Block: b, Face: face})

	if p.breakCounter++; p.breakCounter%5 == 0 {
		p.SwingArm()

		// We send this sound only every so often. Vanilla doesn't send it every tick while breaking
		// either. Every 5 ticks seems accurate.
		p.tx.PlaySound(pos.Vec3(), sound.BlockBreaking{Block: b})
	}
	if breakTime := p.breakTime(pos); breakTime != p.lastBreakDuration {
		viewers, release := p.viewers()
		for _, viewer := range viewers {
			viewer.ViewBlockAction(pos, block.ContinueCrackAction{BreakTime: breakTime})
		}
		releaseBorrowedViewers(p.tx, viewers, release)
		p.lastBreakDuration = breakTime
	}
}

// PlaceBlock makes the player place the block passed at the position passed, granted it is within the range
// of the player.
// An item.UseContext may be passed to obtain information on if the block placement was successful. (SubCount will
// be incremented). Nil may also be passed for the context parameter.
func (p *Player) PlaceBlock(pos cube.Pos, b world.Block, ctx *item.UseContext) {
	ignoreBBox := ctx != nil && ctx.IgnoreBBox
	if !p.placeBlock(pos, b, ignoreBBox) {
		return
	}
	if ctx != nil {
		ctx.CountSub++
	}
}

// placeBlock makes the player place the block passed at the position passed, granted it is within the range
// of the player. A bool is returned indicating if a block was placed successfully.
func (p *Player) placeBlock(pos cube.Pos, b world.Block, ignoreBBox bool) bool {
	if !p.canReachBlock(pos) || !p.GameMode().AllowsEditing() || !p.Permissions().Allows(client.AbilityBuild) {
		p.resendNearbyBlocks(pos, cube.Faces()...)
		return false
	}
	if obstructed, selfOnly := p.obstructedPos(pos, b); obstructed && !ignoreBBox {
		if !selfOnly {
			// Only resend blocks if there were other entities blocking the
			// placement than the player itself. Resending blocks placed inside
			// the player itself leads to synchronisation issues.
			p.resendNearbyBlocks(pos, cube.Faces()...)
		}
		return false
	}

	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleBlockPlace(ctx, pos, b); ctx.Cancelled() {
		p.resendNearbyBlocks(pos, cube.Faces()...)
		return false
	}
	p.tx.SetBlock(pos, b, nil)
	p.tx.PlaySound(pos.Vec3(), sound.BlockPlace{Block: b})
	p.SwingArm()
	return true
}

// obstructedPos checks if the position passed is obstructed if the block
// passed is attempted to be placed. The function returns true as the first
// bool if there is an entity in the way that could prevent the block from
// being placed.
// If the only entity preventing the block from being placed is the player
// itself, the second bool returned is true too.
func (p *Player) obstructedPos(pos cube.Pos, b world.Block) (obstructed, selfOnly bool) {
	// The boxes of a model may be shared by every block of it, as those of
	// model.Solid are: translate a copy.
	models := b.Model().BBox(pos, p.tx)
	blockBoxes := make([]cube.BBox, len(models))
	for i, box := range models {
		blockBoxes[i] = box.Translate(pos.Vec3())
	}

	for e := range p.tx.EntitiesWithin(cube.Box(-3, -3, -3, 3, 3, 3).Translate(pos.Vec3())) {
		t := e.H().Type()
		switch t {
		case entity.ItemType, entity.ArrowType, entity.ExperienceOrbType:
			continue
		default:
			if cube.AnyIntersections(blockBoxes, t.BBox(e).Translate(e.Position()).Grow(-1e-4)) {
				obstructed = true
				if e.H() == p.handle {
					continue
				}
				return true, false
			}
		}
	}
	return obstructed, true
}

// BreakBlock makes the player break a block in the world at a position passed. If the player is unable to
// reach the block passed, the method returns immediately.
func (p *Player) BreakBlock(pos cube.Pos) {
	b := p.tx.Block(pos)
	if _, air := b.(block.Air); air {
		// Don't do anything if the position broken is already air.
		return
	}
	if !p.canReachBlock(pos) || !p.GameMode().AllowsEditing() || !p.Permissions().Allows(client.AbilityMine) {
		p.resendNearbyBlocks(pos)
		return
	}
	if _, breakable := b.(block.Breakable); !breakable && !p.GameMode().CreativeInventory() {
		p.resendNearbyBlocks(pos)
		return
	}
	held, _ := p.HeldItems()
	drops := p.drops(pos, held, b)
	tileDrops := p.tx.World().GameRuleBool("block_drops")
	if !tileDrops {
		drops = nil
	}
	xp := 0
	if tileDrops {
		if breakable, ok := b.(block.Breakable); ok && !p.GameMode().CreativeInventory() {
			if source, ok := b.(interface{ SourceExperience(*world.Tx) int }); ok {
				xp = source.SourceExperience(p.tx)
			} else if _, hasSilkTouch := held.Enchantment(enchantment.SilkTouch); !hasSilkTouch {
				xp = breakable.BreakInfo().XPDrops.RandomValue()
			}
		}
	}

	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleBlockBreak(ctx, pos, &drops, &xp); ctx.Cancelled() {
		p.resendNearbyBlocks(pos)
		return
	}
	if !tileDrops {
		drops, xp = nil, 0
	}
	held, left := p.HeldItems()

	p.SwingArm()
	if destroyer, ok := b.(world.PlayerBlockDestroyer); ok {
		destroyer.PlayerWillDestroy(pos, p.tx, entity.AttackDamageSource{Attacker: p})
	}
	if guardedByPiglins(b) {
		// Block.playerWillDestroy: piglins guard their gold.
		entity.AngerNearbyPiglins(p.tx, p, false)
	}
	p.tx.SetBlock(pos, nil, nil)
	p.emitGameEvent(world.GameEventBlockDestroy, pos.Vec3Centre(), b)
	p.tx.AddParticle(pos.Vec3Centre(), particle.BlockBreak{Block: b})

	if breakable, ok := b.(block.Breakable); ok {
		info := breakable.BreakInfo()
		if info.BreakHandler != nil {
			info.BreakHandler(pos, p.tx, p)
		}
		for _, orb := range entity.NewExperienceOrbs(p.tx, pos.Vec3Centre(), xp) {
			p.tx.AddEntity(orb)
		}
	}
	for _, drop := range drops {
		opts := world.EntitySpawnOpts{Position: pos.Vec3Centre(), Velocity: mgl64.Vec3{rand.Float64()*0.2 - 0.1, 0.2, rand.Float64()*0.2 - 0.1}}
		p.tx.AddEntity(entity.NewItem(opts, drop))
	}

	p.Exhaust(0.005)
	// Only blocks that naturally break instantly (zero hardness) cost no durability; a block made to break
	// within one tick by a fast tool or status effects still consumes durability. Shears wear down on
	// everything but fire, as ShearsItem.mineBlock.
	_, shears := held.Item().(item.Shears)
	if _, fire := b.(block.Fire); shears && fire || !shears && block.BreaksInstantly(b) {
		return
	}
	if durable, ok := held.Item().(item.Durable); ok {
		p.SetHeldItems(p.damageItem(held, durable.DurabilityInfo().BreakDurability), left)
	}
}

// drops returns the drops that the player can get from the block passed using the item held.
func (p *Player) drops(pos cube.Pos, held item.Stack, b world.Block) []item.Stack {
	t, ok := held.Item().(item.Tool)
	if !ok {
		t = item.ToolNone{}
	}
	var drops []item.Stack
	if creative, ok := b.(interface{ CreativeDrops() []item.Stack }); ok && p.GameMode().CreativeInventory() {
		// Block.playerWillDestroy: some blocks drop with their contents for
		// creative players.
		return creative.CreativeDrops()
	}
	if breakable, ok := b.(block.Breakable); ok && breakable.BreakInfo().Harvestable(t) {
		if !p.GameMode().CreativeInventory() {
			if source, ok := b.(interface {
				SourceDrops(cube.Pos, *world.Tx, item.Stack, world.Entity, float64) []item.Stack
			}); ok {
				drops = source.SourceDrops(pos, p.tx, held, p, 0)
			} else {
				drops = breakable.BreakInfo().Drops(t, held.Enchantments())
			}
		}
	} else if it, ok := b.(world.Item); ok && !p.GameMode().CreativeInventory() {
		drops = []item.Stack{item.NewStack(it, 1)}
	}
	return drops
}

// PickBlock makes the player pick a block in the world at a position passed. If the player is unable to
// pick the block, the method returns immediately.
func (p *Player) PickBlock(pos cube.Pos) {
	if !p.canReach(pos.Vec3()) {
		return
	}

	b := p.tx.Block(pos)

	var pickedItem item.Stack
	if pi, ok := b.(block.Pickable); ok {
		pickedItem = pi.Pick()
	} else if i, ok := b.(world.Item); ok {
		if it, ok := world.ItemByName(i.EncodeItem()); ok {
			pickedItem = item.NewStack(it, 1)
		}
	} else {
		return
	}
	if pickedItem.Empty() {
		return
	}

	slot, found := p.Inventory().First(pickedItem)
	if !found && !p.GameMode().CreativeInventory() {
		return
	}

	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleBlockPick(ctx, pos, b); ctx.Cancelled() {
		return
	}
	held, offhand := p.HeldItems()

	if found {
		foundItem, _ := p.Inventory().Item(slot)
		if slot < 9 {
			_ = p.SetHeldSlot(slot)
			return
		}
		if held.LockMode() == item.LockInSlot || foundItem.LockMode() == item.LockInSlot {
			return
		}
		heldSlot := int(atomic.LoadUint32(p.heldSlot))
		_ = p.Inventory().Swap(slot, heldSlot)
		return
	}

	firstEmpty, emptyFound := p.Inventory().FirstEmpty()
	if !emptyFound {
		if held.Locked() {
			return
		}
		p.SetHeldItems(pickedItem, offhand)
		return
	}
	if firstEmpty < 9 {
		_ = p.SetHeldSlot(firstEmpty)
		_ = p.Inventory().SetItem(firstEmpty, pickedItem)
		return
	}
	if held.LockMode() == item.LockInSlot {
		return
	}
	heldSlot := int(atomic.LoadUint32(p.heldSlot))
	_ = p.Inventory().Swap(firstEmpty, heldSlot)
	p.SetHeldItems(pickedItem, offhand)
}

// canReach checks if a player can reach a position with its current range. The range depends on if the player
// is either survival or creative mode. Blocks are reached by canReachBlock, which the anti-cheat configures.
func (p *Player) canReach(pos mgl64.Vec3) bool {
	return p.canReachPoint(pos)
}

// resendNearbyBlocks resends the block at cube.Pos and its adjacent blocks (if faces provided),
// but only if they are within the player's render distance.
func (p *Player) resendNearbyBlocks(pos cube.Pos, faces ...cube.Face) {
	if !p.hasClient() {
		return
	}
	p.resendNearbyBlock(pos)
	for _, f := range faces {
		p.resendNearbyBlock(pos.Side(f))
	}
}

// resendNearbyBlock resends a block at cube.Pos if it is within the player's render distance.
func (p *Player) resendNearbyBlock(pos cube.Pos) {
	if !p.hasClient() {
		return
	}
	if !p.withinChunkRadius(pos.Vec3()) {
		// This is a safety check. Without it, clients could request block resends for arbitrary world positions
		// (including unloaded chunks). A malicious client could repeatedly trigger such requests and force the server
		// to allocate memory for chunks, potentially exhausting RAM.
		return
	}
	b := p.tx.Block(pos)
	p.client().ViewBlockUpdate(pos, b, 0)
	if _, ok := b.(world.LiquidDisplacer); ok {
		liq, _ := p.tx.Liquid(pos)
		p.client().ViewBlockUpdate(pos, liq, 1)
	}
}

// guardedByPiglins reports if a block is in #guarded_by_piglins.
func guardedByPiglins(b world.Block) bool {
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	in, _ := gamedata.BlockTagContains("guarded_by_piglins", state.Name)
	return in
}
