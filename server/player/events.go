package player

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// PreLoginEvent is emitted on the server event bus before a connecting player
// is loaded and spawned into a world. Listeners may call Cancel and set Reason
// to reject the login cleanly.
type PreLoginEvent struct {
	event.Cancellable
	Name   string
	UUID   uuid.UUID
	XUID   string
	Addr   net.Addr
	Reason string
}

// JoinEvent is emitted on the server and world event buses when a player
// finishes joining and spawns into a world.
type JoinEvent struct {
	Player *Player
	Tx     *world.Tx
}

// QuitEvent is emitted when a player disconnects and leaves a world or server.
type QuitEvent struct {
	Player *Player
	Tx     *world.Tx
}

// MoveEvent is emitted before a player moves to a new position and rotation.
// Listeners may call Cancel to prevent the movement.
type MoveEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	From   mgl64.Vec3
	To     mgl64.Vec3
	OldRot cube.Rotation
	NewRot cube.Rotation
}

// TeleportEvent is emitted before a player is teleported from From to To.
// Listeners may call Cancel to prevent the teleportation.
type TeleportEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	From   mgl64.Vec3
	To     mgl64.Vec3
}

// ChatEvent is emitted before a player sends a chat message. Listeners may
// modify Message or call Cancel to prevent it from being sent.
type ChatEvent struct {
	event.Cancellable
	Player  *Player
	Tx      *world.Tx
	Message string
}

// BlockBreakEvent is emitted before a player breaks Block at Pos. Listeners may
// modify Drops and XP, or call Cancel to prevent breaking the block.
type BlockBreakEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Pos    cube.Pos
	Block  world.Block
	Drops  []item.Stack
	XP     int
}

// BlockPlaceEvent is emitted before a player places Block at Pos. Listeners may
// call Cancel to prevent the placement.
type BlockPlaceEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Pos    cube.Pos
	Block  world.Block
}

// AttackEntityEvent is emitted before a player attacks Target. Listeners may
// modify Force, Height, and Critical, or call Cancel to abort the attack.
type AttackEntityEvent struct {
	event.Cancellable
	Player   *Player
	Tx       *world.Tx
	Target   world.Entity
	Force    float64
	Height   float64
	Critical bool
}

// HurtEvent is emitted before a player takes damage from Source. Listeners may
// modify Damage and AttackImmunity, or call Cancel to prevent the damage.
type HurtEvent struct {
	event.Cancellable
	Player         *Player
	Tx             *world.Tx
	Damage         float64
	Immune         bool
	AttackImmunity time.Duration
	Source         world.DamageSource
}

// DeathEvent is emitted when a player dies to Source. Listeners may update
// KeepInventory to control whether items and experience are retained.
type DeathEvent struct {
	Player        *Player
	Tx            *world.Tx
	Source        world.DamageSource
	KeepInventory bool
}

// ItemUseEvent is emitted before a player uses the held Item in the air.
// Listeners may call Cancel to prevent the item use.
type ItemUseEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Item   item.Stack
}

// CommandEvent is emitted before a player executes Command with Args.
// Listeners may call Cancel to prevent command execution.
type CommandEvent struct {
	event.Cancellable
	Player  *Player
	Tx      *world.Tx
	Command cmd.Command
	Args    []string
}

// DropEvent is emitted before a player drops Item on the ground. Listeners may
// call Cancel to prevent dropping the item.
type DropEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Item   item.Stack
}

// ToggleSprintEvent is emitted before a player starts or stops sprinting.
// Listeners may call Cancel to prevent the sprint state change.
type ToggleSprintEvent struct {
	event.Cancellable
	Player    *Player
	Tx        *world.Tx
	Sprinting bool
}

// ToggleSneakEvent is emitted before a player starts or stops sneaking.
// Listeners may call Cancel to prevent the sneak state change.
type ToggleSneakEvent struct {
	event.Cancellable
	Player   *Player
	Tx       *world.Tx
	Sneaking bool
}

// RespawnEvent is emitted when a dead player respawns. Listeners may update
// Position and World to override the respawn destination.
type RespawnEvent struct {
	Player   *Player
	Tx       *world.Tx
	Position mgl64.Vec3
	World    *world.World
}

// ItemUseOnBlockEvent is emitted before a player uses the held Item on a block.
// Listeners may call Cancel to prevent block activation or placement.
type ItemUseOnBlockEvent struct {
	event.Cancellable
	Player   *Player
	Tx       *world.Tx
	Pos      cube.Pos
	Face     cube.Face
	ClickPos mgl64.Vec3
	Item     item.Stack
}

// ItemUseOnEntityEvent is emitted before a player interacts with Entity.
// Listeners may call Cancel to prevent the interaction.
type ItemUseOnEntityEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Entity world.Entity
	Item   item.Stack
}

// ItemPickupEvent is emitted before a player picks up Item from the ground.
// Listeners may modify Item or call Cancel to prevent pickup.
type ItemPickupEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Item   item.Stack
}

// ItemConsumeEvent is emitted before a player finishes consuming Item.
// Listeners may call Cancel to prevent consumption.
type ItemConsumeEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Item   item.Stack
}

// HealEvent is emitted before a player regains Health from Source.
// Listeners may modify Health or call Cancel to prevent healing.
type HealEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Health float64
	Source world.HealingSource
}

// FoodLossEvent is emitted before a player's food level drops from From to To.
// Listeners may modify To or call Cancel to prevent hunger loss.
type FoodLossEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	From   int
	To     int
}

// ExperienceGainEvent is emitted before a player gains Amount experience points.
// Listeners may modify Amount or call Cancel to prevent the gain.
type ExperienceGainEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	Amount int
}

// HeldSlotChangeEvent is emitted before a player switches hotbar slots.
// Listeners may call Cancel to keep the player on From.
type HeldSlotChangeEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
	From   int
	To     int
}

// WorldChangeEvent is emitted after a player transitions from Before to After.
type WorldChangeEvent struct {
	Player *Player
	Tx     *world.Tx
	Before *world.World
	After  *world.World
}

// SignEditEvent is emitted before a player edits text on a sign at Pos.
// Listeners may call Cancel to discard the edit.
type SignEditEvent struct {
	event.Cancellable
	Player    *Player
	Tx        *world.Tx
	Pos       cube.Pos
	FrontSide bool
	OldText   string
	NewText   string
}

// PunchAirEvent is emitted before a player punches the air.
// Listeners may call Cancel to prevent the air punch.
type PunchAirEvent struct {
	event.Cancellable
	Player *Player
	Tx     *world.Tx
}

func (p *Player) txEvents() *event.Bus {
	if p.tx == nil || p.tx.Closed() {
		return nil
	}
	return p.tx.Events()
}

func hasPlayerEventListeners[E any](p *Player) (playerBus, worldBus *event.Bus, has bool) {
	pBus := p.eventBus()
	wBus := p.txEvents()
	pHas := event.HasListeners[E](pBus)
	wHas := event.HasListeners[E](wBus)
	if !pHas && !wHas {
		return nil, nil, false
	}
	if pHas {
		playerBus = pBus
	}
	if wHas {
		worldBus = wBus
	}
	return playerBus, worldBus, true
}

func emitPlayerEvent[E any](pBus, wBus *event.Bus, ev *E) bool {
	if pBus != nil {
		event.Emit(pBus, ev)
	}
	if wBus != nil {
		return event.Emit(wBus, ev)
	}
	if c, ok := any(ev).(interface{ Cancelled() bool }); ok {
		return !c.Cancelled()
	}
	return true
}

func (p *Player) dispatchQuitEvent() {
	if pBus, wBus, has := hasPlayerEventListeners[QuitEvent](p); has {
		ev := QuitEvent{Player: p, Tx: p.tx}
		emitPlayerEvent(pBus, wBus, &ev)
	}
}

func (p *Player) dispatchMoveEvent(from, to mgl64.Vec3, oldRot, newRot cube.Rotation, pBus, wBus *event.Bus, hasBus bool) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleMove(ctx, to, newRot)
		cancelled = ctx.Cancelled()
	}
	if !hasBus {
		return !cancelled
	}
	ev := MoveEvent{Player: p, Tx: p.tx, From: from, To: to, OldRot: oldRot, NewRot: newRot}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchTeleportEvent(pos mgl64.Vec3) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleTeleport(ctx, pos)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[TeleportEvent](p)
	if !has {
		return !cancelled
	}
	ev := TeleportEvent{Player: p, Tx: p.tx, From: p.Position(), To: pos}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchChatEvent(message *string) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleChat(ctx, message)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ChatEvent](p)
	if !has {
		return !cancelled
	}
	ev := ChatEvent{Player: p, Tx: p.tx, Message: *message}
	if cancelled {
		ev.Cancel()
	}
	ok := emitPlayerEvent(pBus, wBus, &ev)
	*message = ev.Message
	return ok
}

func (p *Player) dispatchBlockBreakEvent(pos cube.Pos, b world.Block, drops *[]item.Stack, xp *int) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleBlockBreak(ctx, pos, drops, xp)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[BlockBreakEvent](p)
	if !has {
		return !cancelled
	}
	ev := BlockBreakEvent{Player: p, Tx: p.tx, Pos: pos, Block: b, Drops: *drops, XP: *xp}
	if cancelled {
		ev.Cancel()
	}
	ok := emitPlayerEvent(pBus, wBus, &ev)
	*drops, *xp = ev.Drops, ev.XP
	return ok
}

func (p *Player) dispatchBlockPlaceEvent(pos cube.Pos, b world.Block) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleBlockPlace(ctx, pos, b)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[BlockPlaceEvent](p)
	if !has {
		return !cancelled
	}
	ev := BlockPlaceEvent{Player: p, Tx: p.tx, Pos: pos, Block: b}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchAttackEntityEvent(target world.Entity, force, height *float64, critical *bool) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleAttackEntity(ctx, target, force, height, critical)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[AttackEntityEvent](p)
	if !has {
		return !cancelled
	}
	ev := AttackEntityEvent{Player: p, Tx: p.tx, Target: target, Force: *force, Height: *height, Critical: *critical}
	if cancelled {
		ev.Cancel()
	}
	ok := emitPlayerEvent(pBus, wBus, &ev)
	*force, *height, *critical = ev.Force, ev.Height, ev.Critical
	return ok
}

func (p *Player) dispatchHurtEvent(damage *float64, immune bool, immunity *time.Duration, src world.DamageSource) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleHurt(ctx, damage, immune, immunity, src)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[HurtEvent](p)
	if !has {
		return !cancelled
	}
	ev := HurtEvent{Player: p, Tx: p.tx, Damage: *damage, Immune: immune, AttackImmunity: *immunity, Source: src}
	if cancelled {
		ev.Cancel()
	}
	ok := emitPlayerEvent(pBus, wBus, &ev)
	*damage, *immunity = ev.Damage, ev.AttackImmunity
	return ok
}

func (p *Player) dispatchDeathEvent(src world.DamageSource, keepInv bool) bool {
	pBus, wBus, has := hasPlayerEventListeners[DeathEvent](p)
	if !has {
		return keepInv
	}
	ev := DeathEvent{Player: p, Tx: p.tx, Source: src, KeepInventory: keepInv}
	emitPlayerEvent(pBus, wBus, &ev)
	return ev.KeepInventory
}

func (p *Player) dispatchItemUseEvent(held item.Stack) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleItemUse(ctx)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ItemUseEvent](p)
	if !has {
		return !cancelled
	}
	ev := ItemUseEvent{Player: p, Tx: p.tx, Item: held}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchCommandEvent(command cmd.Command, args []string) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleCommandExecution(ctx, command, args)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[CommandEvent](p)
	if !has {
		return !cancelled
	}
	ev := CommandEvent{Player: p, Tx: p.tx, Command: command, Args: args}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchDropEvent(s item.Stack) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleItemDrop(ctx, s)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[DropEvent](p)
	if !has {
		return !cancelled
	}
	ev := DropEvent{Player: p, Tx: p.tx, Item: s}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchToggleSprintEvent(sprinting bool) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleToggleSprint(ctx, sprinting)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ToggleSprintEvent](p)
	if !has {
		return !cancelled
	}
	ev := ToggleSprintEvent{Player: p, Tx: p.tx, Sprinting: sprinting}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchToggleSneakEvent(sneaking bool) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleToggleSneak(ctx, sneaking)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ToggleSneakEvent](p)
	if !has {
		return !cancelled
	}
	ev := ToggleSneakEvent{Player: p, Tx: p.tx, Sneaking: sneaking}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchRespawnEvent(pos *mgl64.Vec3, w **world.World) {
	p.Handler().HandleRespawn(p, pos, w)
	pBus, wBus, has := hasPlayerEventListeners[RespawnEvent](p)
	if !has {
		return
	}
	ev := RespawnEvent{Player: p, Tx: p.tx, Position: *pos, World: *w}
	emitPlayerEvent(pBus, wBus, &ev)
	*pos = ev.Position
	if ev.World != nil {
		*w = ev.World
	}
}

func (p *Player) dispatchItemUseOnBlockEvent(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleItemUseOnBlock(ctx, pos, face, clickPos)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ItemUseOnBlockEvent](p)
	if !has {
		return !cancelled
	}
	held, _ := p.HeldItems()
	ev := ItemUseOnBlockEvent{Player: p, Tx: p.tx, Pos: pos, Face: face, ClickPos: clickPos, Item: held}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchItemUseOnEntityEvent(e world.Entity) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleItemUseOnEntity(ctx, e)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ItemUseOnEntityEvent](p)
	if !has {
		return !cancelled
	}
	held, _ := p.HeldItems()
	ev := ItemUseOnEntityEvent{Player: p, Tx: p.tx, Entity: e, Item: held}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchItemPickupEvent(s *item.Stack) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleItemPickup(ctx, s)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ItemPickupEvent](p)
	if !has {
		return !cancelled
	}
	ev := ItemPickupEvent{Player: p, Tx: p.tx, Item: *s}
	if cancelled {
		ev.Cancel()
	}
	ok := emitPlayerEvent(pBus, wBus, &ev)
	*s = ev.Item
	return ok
}

func (p *Player) dispatchItemConsumeEvent(s item.Stack) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleItemConsume(ctx, s)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ItemConsumeEvent](p)
	if !has {
		return !cancelled
	}
	ev := ItemConsumeEvent{Player: p, Tx: p.tx, Item: s}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchHealEvent(health *float64, src world.HealingSource) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleHeal(ctx, health, src)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[HealEvent](p)
	if !has {
		return !cancelled
	}
	ev := HealEvent{Player: p, Tx: p.tx, Health: *health, Source: src}
	if cancelled {
		ev.Cancel()
	}
	ok := emitPlayerEvent(pBus, wBus, &ev)
	*health = ev.Health
	return ok
}

func (p *Player) dispatchFoodLossEvent(from int, to *int) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleFoodLoss(ctx, from, to)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[FoodLossEvent](p)
	if !has {
		return !cancelled
	}
	ev := FoodLossEvent{Player: p, Tx: p.tx, From: from, To: *to}
	if cancelled {
		ev.Cancel()
	}
	ok := emitPlayerEvent(pBus, wBus, &ev)
	*to = ev.To
	return ok
}

func (p *Player) dispatchExperienceGainEvent(amount *int) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleExperienceGain(ctx, amount)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[ExperienceGainEvent](p)
	if !has {
		return !cancelled
	}
	ev := ExperienceGainEvent{Player: p, Tx: p.tx, Amount: *amount}
	if cancelled {
		ev.Cancel()
	}
	ok := emitPlayerEvent(pBus, wBus, &ev)
	*amount = ev.Amount
	return ok
}

func (p *Player) dispatchHeldSlotChangeEvent(from, to int) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleHeldSlotChange(ctx, from, to)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[HeldSlotChangeEvent](p)
	if !has {
		return !cancelled
	}
	ev := HeldSlotChangeEvent{Player: p, Tx: p.tx, From: from, To: to}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchWorldChangeEvent(before, after *world.World) {
	p.Handler().HandleChangeWorld(p, before, after)
	pBus, wBus, has := hasPlayerEventListeners[WorldChangeEvent](p)
	if !has {
		return
	}
	ev := WorldChangeEvent{Player: p, Tx: p.tx, Before: before, After: after}
	emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchSignEditEvent(pos cube.Pos, frontSide bool, oldText, newText string) bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandleSignEdit(ctx, pos, frontSide, oldText, newText)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[SignEditEvent](p)
	if !has {
		return !cancelled
	}
	ev := SignEditEvent{Player: p, Tx: p.tx, Pos: pos, FrontSide: frontSide, OldText: oldText, NewText: newText}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}

func (p *Player) dispatchPunchAirEvent() bool {
	cancelled := false
	if !p.hasNopHandler() {
		ctx := NewEventContext(p.tx, p)
		p.Handler().HandlePunchAir(ctx)
		cancelled = ctx.Cancelled()
	}
	pBus, wBus, has := hasPlayerEventListeners[PunchAirEvent](p)
	if !has {
		return !cancelled
	}
	ev := PunchAirEvent{Player: p, Tx: p.tx}
	if cancelled {
		ev.Cancel()
	}
	return emitPlayerEvent(pBus, wBus, &ev)
}
