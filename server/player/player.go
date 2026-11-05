package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/txguard"
	"github.com/df-mc/dragonfly/server/world"
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

func borrowViewers(tx *world.Tx, pos mgl64.Vec3) (viewers []world.Viewer, ok bool) {
	return txguard.Value(tx, func() []world.Viewer {
		return tx.Viewers(pos)
	})
}

func releaseBorrowedViewers(tx *world.Tx, viewers []world.Viewer, ok bool) {
	if !ok || tx == nil {
		return
	}
	txguard.Run(tx, func() {
		tx.ReleaseViewers(viewers)
	})
}
