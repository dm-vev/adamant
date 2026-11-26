package player

import (
	"sync/atomic"
	"time"

	"github.com/dm-vev/adamant/server/client"
	"github.com/dm-vev/adamant/server/ecs"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/event"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// playerData is the state of a player, by concern: the hot state of the
// ticks first, in the structs declared in the files of the concerns, then
// the managers.
type playerData struct {
	movementState
	fallState
	airState
	itemUseState
	combatState
	abilityState
	identityState
	tagState
	lifecycleState
	acFields
	raidState
	carriedState
	javaRuntime
	respawnState
	sleepState
	breakingState
	cooldownState
	inventoryState

	health       *entity.HealthManager
	experience   *entity.ExperienceManager
	effects      *entity.EffectManager
	lastXPPickup *time.Time
	enchantSeed  int64
	hunger       *hungerManager
	events       atomic.Pointer[event.Bus]
}

// Player is an implementation of a player entity. It has methods that implement the behaviour that players
// need to play in the world.
type Player struct {
	tx     *world.Tx
	handle *world.EntityHandle
	data   *world.EntityData
	*playerData
}

func (p *Player) H() *world.EntityHandle {
	return p.handle
}

func (p *Player) Tx() *world.Tx {
	return p.tx
}

func (p *Player) bindTx(tx *world.Tx) {
	p.tx = tx
}

func (p *Player) emitGameEvent(event world.GameEvent, position mgl64.Vec3, affected world.Block) {
	if p.tx == nil {
		return
	}
	p.tx.EmitGameEvent(event, position, world.GameEventContext{
		Source: p, AffectedState: affected, Sneaking: p.Sneaking(), Spectator: p.GameMode() == world.GameModeSpectator,
	})
}

// BindTransaction allows external callers operating inside a world transaction to
// synchronise the player with that transaction before invoking sleeper-specific
// operations such as Sleep or Wake. This prevents those methods from falling back
// to asynchronously reacquiring a transaction when the caller already has one
// open.
func (p *Player) BindTransaction(tx *world.Tx) {
	p.bindTx(tx)
}

// client returns the client of the player, client.Nop if it has none.
func (p *Player) client() client.Client {
	if c := p.c; c != nil {
		return c
	}
	return client.Nop{}
}

// hasClient reports whether a remote client plays the player.
func (p *Player) hasClient() bool { return p.c != nil }

// simulated reports whether the server moves the player: one without a
// client has none to move it.
func (p *Player) simulated() bool { return p.c == nil }

// Handler returns the Handler of the player.
func (p *Player) Handler() Handler {
	return p.h
}

// Components returns the ECS component bag attached to the player.
func (p *Player) Components() *ecs.Bag {
	if p == nil || p.data == nil {
		return nil
	}
	return &p.data.Components
}

// Events returns the player's prioritized event bus, lazily initializing it on
// first call so players without listeners incur zero heap allocations.
func (p *Player) Events() *event.Bus {
	if p == nil || p.playerData == nil {
		return nil
	}
	if bus := p.events.Load(); bus != nil {
		return bus
	}
	bus := event.NewBus()
	if p.events.CompareAndSwap(nil, bus) {
		return bus
	}
	return p.events.Load()
}

// eventBus returns the player's event bus without allocating if unused.
func (p *Player) eventBus() *event.Bus {
	if p == nil || p.playerData == nil {
		return nil
	}
	return p.events.Load()
}

// hasNopHandler reports whether the player uses the default no-op Handler.
func (p *Player) hasNopHandler() bool {
	_, ok := p.h.(NopHandler)
	return ok
}

func borrowViewers(tx *world.Tx, pos mgl64.Vec3) (viewers []world.Viewer, ok bool) {
	if tx == nil || tx.Closed() {
		return nil, false
	}
	return tx.Viewers(pos), true
}

func releaseBorrowedViewers(tx *world.Tx, viewers []world.Viewer, ok bool) {
	if !ok || tx == nil || tx.Closed() {
		return
	}
	tx.ReleaseViewers(viewers)
}
