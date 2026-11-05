package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/txguard"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type playerData struct {
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

	// frostWalkPos is the block Frost Walker last ran in.
	frostWalkPos     cube.Pos
	frostWalkPosSet  bool
	absorptionHealth float64
	scale            float64
	gameMode         world.GameMode
	sneaking, sprinting, swimming, gliding, crawling, flying,
	invisible, immobile, onGround, usingItem bool
	// moved marks a player whose viewers are yet to be shown where it moved,
	// which they are with the next tick, and movedByServer one that the
	// server moved, whose own client is shown the move too.
	moved, movedByServer                 bool
	blockingDelayTicks                   int64
	usingSince                           time.Time
	usingTicks                           int64
	usingStack                           item.Stack
	kineticRecent                        map[*world.EntityHandle]int64
	usingSlot                            uint32
	itemUseTick                          int64
	itemUseCompleteTick                  int64
	itemUseCompletedStack                item.Stack
	itemUseCompletedSlot                 uint32
	itemUseRecentlyCompleted             bool
	glideTicks                           int64
	fireTicks                            int64
	fallDistance                         float64
	moveEventDistance, nextMoveEventStep float32
	movementSupport                      cube.Pos
	hasMovementSupport, onGroundNoBlocks bool
	breathing                            bool
	airSupplyTicks                       int
	maxAirSupplyTicks                    int
	// attackStrengthTicks counts the ticks since the player last attacked,
	// punched the air or came to hold another item in its main hand, as
	// Java's Player.attackStrengthTicker (tickAttackStrength), and
	// lastMainHand is that item, by name and meta.
	attackStrengthTicks int
	lastMainHand        struct {
		name string
		meta int16
	}
	speed               float64
	flightSpeed         float64
	verticalFlightSpeed float64
	health              *entity.HealthManager
	experience          *entity.ExperienceManager
	effects             *entity.EffectManager
	lastXPPickup        *time.Time
	lastDamage          float64
	immuneUntil         time.Time
	// The combat memory of the LivingEntity: the living entity that last
	// hurt the player, the damage it last took and the living entity it
	// last hurt, with the game ticks they happened at.
	lastHurtBy, lastHurtMob                  *world.EntityHandle
	lastHurtByTick, lastHurtMobTick          int64
	lastDamageSource                         world.DamageSource
	lastDamageTick                           int64
	enchantSeed                              int64
	mc                                       *entity.MovementComputer
	portalTravel                             *entity.PortalTravelComputer
	collidedVertically, collidedHorizontally bool
	hunger                                   *hungerManager
	prevWorld                                *world.World
	fishingHook                              *world.EntityHandle
	// spectating is the entity the camera of the player follows, as Java's
	// ServerPlayer.camera when it is not the player itself.
	spectating *world.EntityHandle
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
