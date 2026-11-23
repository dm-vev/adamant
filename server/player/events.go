package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

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
