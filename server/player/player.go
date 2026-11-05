package player

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/event"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/txguard"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

type playerData struct {
	// dataUUID is the UUID the record and the spawn points of the player
	// are kept by.
	dataUUID uuid.UUID
	// frostWalkPos is the block Frost Walker last ran in.
	frostWalkPos                cube.Pos
	frostWalkPosSet             bool
	unlockedRecipes             map[string]struct{}
	raidOmenPosition            *cube.Pos
	shoulderLeft, shoulderRight map[string]any
	// shoulderSatTick is ServerPlayer.timeEntitySatOnShoulder, the tick an
	// entity last sat on a shoulder.
	shoulderSatTick    int64
	javaState          map[string]any
	wardenSpawnTracker *world.WardenSpawnTracker
	// sidebar is the last scoreboard sidebar sent to the client.
	sidebar           string
	xuid              string
	locale            language.Tag
	nameTag, scoreTag string
	alwaysShowNameTag bool
	absorptionHealth  float64
	scale             float64

	gameMode world.GameMode
	skin     skin.Skin
	c        client.Client // The client of the player, nil if it has none.
	// menu is the menu the player has open, nil if none. It is atomic as
	// the slot functions of the inventories, which may run off the
	// transaction, read it.
	menu atomic.Pointer[menu.Menu]
	// menuTx is set while a batch of BeginMenu runs.
	menuTx atomic.Bool
	// menuIgnoredAt is when a request to open a menu open already was last
	// logged, and menuIgnoredCount how many were left out since.
	menuIgnoredAt    time.Time
	menuIgnoredCount int
	// menuWorld is the world the menu of the player was opened in.
	menuWorld *world.World
	h         Handler
	// log is the logger of the player, never nil.
	log *slog.Logger

	inv, offHand, enderChest, ui *inventory.Inventory
	armour                       *inventory.Armour
	heldSlot                     *uint32

	sneaking, sprinting, swimming, gliding, crawling, flying,
	invisible, immobile, onGround, usingItem bool
	// moved marks a player whose viewers are yet to be shown where it moved,
	// which they are with the next tick, and movedByServer one that the
	// server moved, whose own client is shown the move too.
	moved, movedByServer bool

	blockingDelayTicks int64

	sleeping      bool
	sleepPos      cube.Pos
	timeSinceRest int32

	// tickCount is Entity.tickCount, the ticks the player has existed in
	// this session.
	tickCount int64
	// sentFood is the food state last sent to the client.
	sentFood hungerSnapshot

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

	glideTicks                           int64
	fireTicks                            int64
	fallDistance                         float64
	moveEventDistance, nextMoveEventStep float32
	movementSupport                      cube.Pos
	hasMovementSupport, onGroundNoBlocks bool

	breathing         bool
	airSupplyTicks    int
	maxAirSupplyTicks int

	cooldowns map[string]time.Time
	// cooldownsMu guards the cooldowns map, which is accessed from gameplay and
	// network goroutines.
	cooldownsMu sync.Mutex
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

	health     *entity.HealthManager
	experience *entity.ExperienceManager
	effects    *entity.EffectManager

	lastXPPickup *time.Time

	lastDamage  float64
	immuneUntil time.Time
	// The combat memory of the LivingEntity: the living entity that last
	// hurt the player, the damage it last took and the living entity it
	// last hurt, with the game ticks they happened at.
	lastHurtBy, lastHurtMob         *world.EntityHandle
	lastHurtByTick, lastHurtMobTick int64
	lastDamageSource                world.DamageSource
	lastDamageTick                  int64
	sleepTimer                      int

	deathPos       *mgl64.Vec3
	deathDimension world.Dimension
	// deathEffects are the effects the player died with. Like a Java
	// ServerPlayer removed as killed, their on-death behaviour runs when
	// the player respawns.
	deathEffects     []effect.Effect
	respawnDimension world.Dimension
	respawnPosition  cube.Pos
	respawnRotation  cube.Rotation
	hasRespawnPoint  bool

	enchantSeed int64

	mc           *entity.MovementComputer
	portalTravel *entity.PortalTravelComputer

	collidedVertically, collidedHorizontally bool

	breaking          bool
	breakingPos       cube.Pos
	breakingFace      cube.Face
	lastBreakDuration time.Duration

	breakCounter uint32

	hunger *hungerManager

	closing atomic.Bool
	// left is set once the player left with its client (leave).
	left atomic.Bool
	// leftHook and quitMessage are the Left and QuitMessage of the Config
	// of the player.
	leftHook    func(tx *world.Tx, p *Player)
	quitMessage chat.Translation
	// acConf and acExempt are the AntiCheat and AntiCheatExempt of the
	// Config of the player.
	acConf   *anticheat.Config
	acExempt func(p *Player, c anticheat.Check) bool
	// permissions is the Permissions of the Config of the player.
	permissions func(p *Player) client.Permissions
	// acState is what the anti-cheat keeps of the player, made as it is
	// first needed; see anticheat.
	acState *acState

	prevWorld *world.World

	fishingHook *world.EntityHandle
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

// SetSpeed sets the speed of the player. The value passed is the blocks/tick speed that the player will then
// obtain.
func (p *Player) SetSpeed(speed float64) {
	p.speed = speed
	p.client().SendSpeed(speed)
}

// Speed returns the speed of the player, returning a value that indicates the blocks/tick speed. The default
// speed of a player is 0.1.
func (p *Player) Speed() float64 {
	return p.speed
}

// SetFlightSpeed sets the flight speed of the player. The value passed represents the base speed, which is
// multiplied by 10 to obtain the actual blocks/tick speed that the player will then obtain while flying.
func (p *Player) SetFlightSpeed(flightSpeed float64) {
	p.flightSpeed = flightSpeed
	p.client().SendAbilities(p)
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.GameMode }))
}

// FlightSpeed returns the flight speed of the player, with the value representing the base speed. The actual
// blocks/tick speed is this value multiplied by 10. The default flight speed of a player is 0.05, which
// corresponds to 0.5 blocks/tick.
func (p *Player) FlightSpeed() float64 {
	return p.flightSpeed
}

// SetVerticalFlightSpeed sets the flight speed of the player on the Y axis. The value passed represents the
// base speed, which is the blocks/tick speed that the player will obtain while flying.
func (p *Player) SetVerticalFlightSpeed(flightSpeed float64) {
	p.verticalFlightSpeed = flightSpeed
	p.client().SendAbilities(p)
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.GameMode }))
}

// VerticalFlightSpeed returns the flight speed of the player on the Y axis, with the value representing the
// base speed. The default vertical flight speed of a player is 1.0, which corresponds to 1 block/tick.
func (p *Player) VerticalFlightSpeed() float64 {
	return p.verticalFlightSpeed
}

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

// StartSprinting makes a player start sprinting, increasing the speed of the player by 30% and making
// particles show up under the feet. The player will only start sprinting if its food level is high enough.
// If the player is sneaking when calling StartSprinting, it is stopped from sneaking.
func (p *Player) StartSprinting() {
	if !p.hunger.canSprint() && p.GameMode().AllowsTakingDamage() || p.crawling || p.sprinting {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleToggleSprint(ctx, true); ctx.Cancelled() {
		return
	}
	p.StopSneaking()
	p.sprinting = true
	p.SetSpeed(p.speed * 1.3)
	p.updateState()
}

// Sprinting checks if the player is currently sprinting.
func (p *Player) Sprinting() bool {
	return p.sprinting
}

// StopSprinting makes a player stop sprinting, setting back the speed of the player to its original value.
func (p *Player) StopSprinting() {
	if !p.sprinting {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleToggleSprint(ctx, false); ctx.Cancelled() {
		return
	}
	p.sprinting = false
	p.SetSpeed(p.speed / 1.3)
	p.updateState()
}

// StartSneaking makes a player start sneaking. If the player is already sneaking, StartSneaking will not do
// anything.
// If the player is sprinting while StartSneaking is called, the sprinting is stopped.
func (p *Player) StartSneaking() {
	if p.sneaking {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleToggleSneak(ctx, true); ctx.Cancelled() {
		return
	}
	if p.sneaking {
		return
	}
	if !p.Flying() {
		p.StopSprinting()
	}
	p.sneaking = true
	p.updateState()
}

// Sneaking checks if the player is currently sneaking.
func (p *Player) Sneaking() bool {
	return p.sneaking
}

// StopSneaking makes a player stop sneaking if it currently is. If the player is not sneaking, StopSneaking
// will not do anything.
func (p *Player) StopSneaking() {
	if !p.sneaking {
		return
	}
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleToggleSneak(ctx, false); ctx.Cancelled() {
		return
	}
	p.sneaking = false
	p.updateState()
}

// StartSwimming makes the player start swimming if it is not currently doing so. If the player is sneaking
// while StartSwimming is called, the sneaking is stopped.
func (p *Player) StartSwimming() {
	if p.swimming {
		return
	}
	p.StopSneaking()
	p.swimming = true
	p.updateState()
}

// Swimming checks if the player is currently swimming.
func (p *Player) Swimming() bool {
	return p.swimming
}

// StopSwimming makes the player stop swimming if it is currently doing so.
func (p *Player) StopSwimming() {
	if !p.swimming {
		return
	}
	p.swimming = false
	p.updateState()
}

// StartCrawling makes the player start crawling if it is not currently doing so. If the player is sneaking
// while StartCrawling is called, the sneaking is stopped.
func (p *Player) StartCrawling() {
	if p.crawling {
		return
	}
	for _, corner := range p.H().Type().BBox(p).Translate(p.Position()).Corners() {
		if _, isAir := p.tx.Block(cube.PosFromVec3(corner).Add(cube.Pos{0, 1, 0})).(block.Air); !isAir {
			p.crawling = true
			break
		}
	}
	p.StopSneaking()
	p.updateState()
}

// StopCrawling makes the player stop crawling if it is currently doing so.
func (p *Player) StopCrawling() {
	if !p.crawling {
		return
	}
	p.crawling = false
	p.updateState()
}

// Crawling checks if the player is currently crawling.
func (p *Player) Crawling() bool {
	return p.crawling
}

// StartGliding makes the player start gliding if it is not currently doing so.
func (p *Player) StartGliding() {
	if p.gliding {
		return
	}
	chest := p.Armour().Chestplate()
	if _, ok := chest.Item().(item.Elytra); !ok || chest.Durability() < 2 {
		return
	}
	p.gliding = true
	p.updateState()
}

// Gliding checks if the player is currently gliding.
func (p *Player) Gliding() bool {
	return p.gliding
}

// StopGliding makes the player stop gliding if it is currently doing so.
func (p *Player) StopGliding() {
	if !p.gliding {
		return
	}
	p.gliding = false
	p.glideTicks = 0
	p.updateState()
}

// StartFlying makes the player start flying if they aren't already. It requires the player to be in a gamemode which
// allows flying. A client that asks to fly in a game mode without flight is shown its abilities again.
func (p *Player) StartFlying() {
	if !p.GameMode().AllowsFlying() {
		if p.clientInput() {
			p.log.Debug("client asked to fly in a game mode without flight")
			p.refuseFlightToggle()
			p.client().SendAbilities(p)
		}
		return
	}
	if p.Flying() {
		return
	}
	p.flying = true
	p.client().SendGameMode(p)
}

// Flying checks if the player is currently flying.
func (p *Player) Flying() bool {
	return p.flying
}

// StopFlying makes the player stop flying if it currently is. A client that asks to stop in a game mode without
// collision, which flies for good, is shown its abilities again.
func (p *Player) StopFlying() {
	if p.clientInput() && !p.GameMode().HasCollision() {
		p.client().SendAbilities(p)
		return
	}
	if !p.flying {
		return
	}
	p.flying = false
	p.client().SendGameMode(p)
}

// Jump makes the player jump if they are on ground. It exhausts the player by 0.05 food points, an additional 0.15
// is exhausted if the player is sprint jumping.
func (p *Player) Jump() {
	if p.Dead() {
		return
	}

	p.Handler().HandleJump(p)
	if p.OnGround() {
		jumpVel := 0.42
		if e, ok := p.Effect(effect.JumpBoost); ok {
			jumpVel = float64(e.Level()) / 10
		}
		p.data.Vel = mgl64.Vec3{0, jumpVel}
	}
	if p.Sprinting() {
		p.Exhaust(0.2)
	} else {
		p.Exhaust(0.05)
	}
}

// Sleep makes the player sleep at the given position. If the position does not map to a bed (specifically the head side),
// the player will not sleep.
func (p *Player) Sleep(pos cube.Pos) {
	if p.sleeping {
		// The player is already sleeping.
		return
	}

	if ok := txguard.Run(p.tx, func() {
		p.sleepInTx(p.tx, pos)
	}); ok {
		return
	}

	p.Do(func(tx *world.Tx, sleeper *Player) {
		if sleeper.sleeping {
			return
		}
		sleeper.sleepInTx(tx, pos)
	})
}

func (p *Player) sleepInTx(tx *world.Tx, pos cube.Pos) {
	b := tx.Block(pos)
	height := 0.5625
	switch bed := b.(type) {
	case block.Bed:
		if bed.Occupied {
			return
		}
	case block.StrawBed:
		var ok bool
		height, ok = bed.SleepHeight(pos, tx)
		if bed.Occupied || !ok {
			return
		}
	default:
		return
	}

	ctx, sendReminder := NewEventContext(p.tx, p), true
	if p.Handler().HandleSleep(ctx, &sendReminder); ctx.Cancelled() {
		return
	}
	entity.DismountRider(p, tx)

	switch bed := b.(type) {
	case block.Bed:
		bed.Sleeper, bed.Occupied = p.H(), true
		b = bed
	case block.StrawBed:
		bed.Sleeper, bed.Occupied = p.H(), true
		b = bed
	}

	tx.World().SetRequiredSleepDuration(time.Millisecond * 5050)

	p.setPosition(pos.Vec3Middle().Add(mgl64.Vec3{0, height + 0.125}))
	tx.SetBlock(pos, b, nil)
	p.sleeping = true
	p.timeSinceRest = 0
	p.sleepTimer = 0
	p.sleepPos = pos
	p.SetVelocity(mgl64.Vec3{})
	p.data.Vel = mgl64.Vec3{}

	p.client().SendPlayerSpawn(pos.Vec3())

	if sendReminder {
		tx.BroadcastSleepingReminder(p)
	}

	tx.BroadcastSleepingIndicator()
	p.updateState()
}

// Wake forces the player out of bed if they are sleeping.
func (p *Player) Wake() {
	if !p.sleeping {
		return
	}

	if ok := txguard.Run(p.tx, func() {
		p.wakeInTx(p.tx)
	}); ok {
		return
	}

	p.Do(func(tx *world.Tx, sleeper *Player) {
		if !sleeper.sleeping {
			return
		}
		sleeper.wakeInTx(tx)
	})
}

func (p *Player) wakeInTx(tx *world.Tx) {
	if !p.sleeping {
		return
	}
	pos := p.sleepPos
	switch bed := tx.Block(pos).(type) {
	case block.Bed:
		bed.Sleeper, bed.Occupied = nil, false
		tx.SetBlock(pos, bed, nil)
		p.standUpFromBed(tx, pos, bed.Facing)
	case block.StrawBed:
		bed.Sleeper, bed.Occupied = nil, false
		tx.SetBlock(pos, bed, nil)
		bed.StopSleeping(pos, tx)
		p.standUpFromBed(tx, pos, bed.Facing)
	}
	p.sleeping = false
	p.sleepPos = cube.Pos{}

	tx.BroadcastSleepingIndicator()

	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityWake(p)
	}
	releaseBorrowedViewers(tx, viewers, release)
	p.updateState()

}

// Sleeping returns true if the player is currently sleeping, along with the position of the bed the player is sleeping
// on.
func (p *Player) Sleeping() (cube.Pos, bool) {
	if !p.sleeping {
		return cube.Pos{}, false
	}
	return p.sleepPos, true
}

// SleepTimer returns the ticks the player slept in its last sleep, up to
// 100, as Player.getSleepTimer.
func (p *Player) SleepTimer() int { return p.sleepTimer }

// TimeSinceRest returns awake ticks since the player last started sleeping.
func (p *Player) TimeSinceRest() int32 {
	return p.timeSinceRest
}

// SetInvisible sets the player invisible, so that other players will not be able to see it.
func (p *Player) SetInvisible() {
	if p.Invisible() {
		return
	}
	p.invisible = true
	p.updateState()
}

// SetVisible sets the player visible again, so that other players can see it again. If the player was already
// visible, or if the player is in spectator mode, nothing happens.
func (p *Player) SetVisible() {
	if _, ok := p.Effect(effect.Invisibility); ok || !p.GameMode().Visible() || !p.invisible {
		return
	}
	p.invisible = false
	p.updateState()
}

// Invisible checks if the Player is currently invisible.
func (p *Player) Invisible() bool {
	return p.invisible
}

// SetImmobile prevents the player from moving around, but still allows them to look around.
func (p *Player) SetImmobile() {
	if p.Immobile() {
		return
	}
	p.immobile = true
	p.updateState()
}

// SetMobile allows the player to freely move around again after being immobile.
func (p *Player) SetMobile() {
	if !p.Immobile() {
		return
	}
	p.immobile = false
	p.updateState()
}

// Immobile checks if the Player is currently immobile.
func (p *Player) Immobile() bool {
	return p.immobile
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

// SetGameMode sets the game mode of a player. The game mode specifies the way that the player can interact
// with the world that it is in.
func (p *Player) SetGameMode(mode world.GameMode) {
	previous := p.GameMode()
	p.gameMode = mode
	if !mode.AllowsInteraction() {
		p.cancelItemUse()
	}

	if !mode.AllowsFlying() {
		p.StopFlying()
	}
	if !mode.Visible() {
		p.SetInvisible()
	} else if !previous.Visible() {
		p.SetVisible()
	}

	p.client().SendGameMode(p)
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityGameMode(p)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
	if mode.AllowsTakingDamage() {
		p.client().SendHealth(p.Health(), p.MaxHealth(), p.absorptionHealth)
	}
	if mode != world.GameModeSpectator {
		p.Spectate(nil)
	}
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.GameMode }))
}

// Spectate makes the camera of the player follow an entity, ported from
// ServerPlayer.setCamera: the player moves to the entity and follows it until
// it sneaks, leaves spectator mode or the entity is gone. A nil entity
// returns the camera to the player.
func (p *Player) Spectate(e world.Entity) {
	var h *world.EntityHandle
	if e != nil {
		h = e.H()
	}
	if h == p.spectating {
		return
	}
	p.spectating = h
	if e != nil {
		p.Teleport(e.Position())
	}
	p.client().ViewSpectatedEntity(e)
}

// Spectating returns the entity the camera of the player follows, if any.
func (p *Player) Spectating() (world.Entity, bool) {
	if p.spectating == nil {
		return nil, false
	}
	return p.spectating.Entity(p.tx)
}

// tickSpectating ports the camera part of ServerPlayer.tick: the player keeps
// up with the entity it spectates and stops when it sneaks or the entity is
// gone.
func (p *Player) tickSpectating() {
	if p.spectating == nil {
		return
	}
	e, ok := p.spectating.Entity(p.tx)
	if mortal, isMortal := e.(interface{ Dead() bool }); !ok || isMortal && mortal.Dead() || p.Sneaking() {
		p.Spectate(nil)
		return
	}
	if e.Position().Sub(p.Position()).LenSqr() > 1e-4 {
		p.Teleport(e.Position())
	}
}

// GameMode returns the current game mode assigned to the player. If not changed, the game mode returned will
// be the same as that of the world that the player spawns in.
// The game mode may be changed using Player.SetGameMode().
func (p *Player) GameMode() world.GameMode {
	return p.gameMode
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
	case item.KineticWeapon:
		p.startItemUse(i)
		if spear, ok := it.(item.Spear); ok {
			name := "item.spear.use"
			if spear.Tier == item.ToolTierWood {
				name = "item.spear_wood.use"
			}
			p.PlaySound(sound.Custom{Name: name, Volume: 1, Pitch: 1})
		}
	case item.Usable:
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
	case item.Consumable:
		if p.canConsume(usable) {
			p.startItemUse(i)
		}
	}
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

// resyncInventory corrects a client-side release prediction that did not consume an item.
func (p *Player) resyncInventory() {
	p.client().ResyncInventory()
}

func (p *Player) startItemUse(stack item.Stack) {
	p.usingItem = true
	p.usingSince = time.Now()
	p.usingTicks = 0
	p.usingStack = stack
	if _, ok := stack.Item().(item.KineticWeapon); ok {
		if p.kineticRecent == nil {
			p.kineticRecent = make(map[*world.EntityHandle]int64)
		}
		clear(p.kineticRecent)
	}
	p.usingSlot = atomic.LoadUint32(p.heldSlot)
	p.itemUseRecentlyCompleted = false
	p.emitGameEvent(world.GameEventItemInteractStart, p.Position(), nil)
	p.updateState()
}

func (p *Player) cancelItemUse() {
	if !p.usingItem {
		return
	}
	p.usingItem = false
	p.usingTicks = 0
	p.usingStack = item.Stack{}
	clear(p.kineticRecent)
	p.emitGameEvent(world.GameEventItemInteractFinish, p.Position(), nil)
	p.updateState()
}
func (p *Player) completeItemUse() {
	p.usingItem = false
	p.usingTicks = 0
	p.usingStack = item.Stack{}
	clear(p.kineticRecent)
	p.emitGameEvent(world.GameEventItemInteractFinish, p.Position(), nil)
	p.itemUseCompleteTick = p.itemUseTick
	p.itemUseCompletedStack, _ = p.HeldItems()
	p.itemUseCompletedSlot = atomic.LoadUint32(p.heldSlot)
	p.itemUseRecentlyCompleted = true
	p.updateState()
}

func (p *Player) itemUseMatches(stack item.Stack) bool {
	return p.usingSlot == atomic.LoadUint32(p.heldSlot) && p.usingStack.Equal(stack)
}

func (p *Player) recentlyCompletedItemUse(stack item.Stack) bool {
	if !p.itemUseRecentlyCompleted {
		return false
	}
	if p.itemUseCompletedSlot != atomic.LoadUint32(p.heldSlot) || !p.itemUseCompletedStack.Equal(stack) {
		p.itemUseRecentlyCompleted = false
		return false
	}
	if ticks := p.itemUseTick - p.itemUseCompleteTick; ticks >= 0 && ticks <= 2 {
		// Ignore the completion packet that commonly follows server-side completion within 100ms.
		return true
	}
	p.itemUseRecentlyCompleted = false
	return false
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
	return max(time.Since(p.usingSince)+time.Second/20, time.Duration(p.usingTicks)*time.Second/20)
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
	// Hit-dependent blocks are given the exact packet click position before ordinary
	// activation or item use, so they can choose a precise slot.
	hitAct, hitDependent := b.(block.HitActivatable)
	if hitDependent || !p.Sneaking() || i.Empty() {
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
				return
			}
			if useCtx.ReplaceHeldItem && !useCtx.NewItem.Empty() {
				p.SetHeldItems(useCtx.NewItem, left)
				return
			}
			p.SetHeldItems(p.subtractItem(p.damageItem(i, useCtx.Damage), useCtx.CountSub), left)
			p.addNewItem(useCtx)
			return
		}
		if bell, ok := b.(block.Bell); ok {
			p.client().ViewBlockAction(pos, block.BellUseRejectedAction{Bell: bell})
		}
	}
	if i.Empty() {
		return
	}
	switch ib := i.Item().(type) {
	case item.UsableOnBlock:
		// The item does something when used on a block.
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
	case world.Block:
		// The item IS a block, meaning it is being placed.
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

// Teleport teleports the player to a target position in the world. Unlike Move, it immediately changes the
// position of the player, rather than showing an animation.
func (p *Player) Teleport(pos mgl64.Vec3) {
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleTeleport(ctx, pos); ctx.Cancelled() {
		return
	}
	p.StopFishing(p.tx, false)
	p.forceTeleport(pos)
}

// TeleportToWorld transfers the player to destination and places them at pos,
// as a teleport command run in another dimension does. The caller must own
// the player's current world transaction. If the destination is closed, the
// player stays where they were. While a checkpoint holds transfers out of
// the player's world, the player is moved a tick later.
func (p *Player) TeleportToWorld(tx *world.Tx, destination *world.World, pos mgl64.Vec3) {
	if destination == nil || destination == tx.World() {
		p.Teleport(pos)
		return
	}
	transfer, ok := world.BeginTransfer(tx)
	if !ok {
		p.H().DoAfter(transferRetryDelay, func(tx *world.Tx, e world.Entity) {
			if p, ok := e.(*Player); ok {
				p.TeleportToWorld(tx, destination, pos)
			}
		})
		return
	}
	// A menu stays in the world it was opened in.
	p.closeMenu(tx, false)
	handle := transfer.Remove(tx, p)
	task := destination.Do(func(destinationTx *world.Tx) {
		np := destinationTx.AddEntity(handle).(*Player)
		np.Teleport(pos)
		np.client().SendRespawn(pos, p)
		np.SetVisible()
	})
	if errors.Is(task.Err(), world.ErrWorldClosed) {
		tx.AddEntity(handle)
	}
}

// forceTeleport teleports the player without calling the Handler.
func (p *Player) forceTeleport(pos mgl64.Vec3) {
	p.Wake()
	p.teleport(pos)
}

// teleport teleports the player to a target position in the world. It does not call the Handler of the
// player.
func (p *Player) teleport(pos mgl64.Vec3) {
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityTeleport(p, pos)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
	p.setPosition(pos)
	p.data.Vel = mgl64.Vec3{}
	p.ResetFallDistance()
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.Teleport }))
}

// setPosition sets the position of the player and tells its world, so that
// mobs finding the players near them find it there.
func (p *Player) setPosition(pos mgl64.Vec3) {
	p.data.Pos = pos
	p.tx.PlayerMoved()
}

// showMove shows the viewers of the player where it moved since the last tick, if that changed what they see:
// one absolute move, encoded once for all of them. Its own session is shown it only if the server moved it.
func (p *Player) showMove() {
	if !p.moved {
		return
	}
	byServer := p.movedByServer
	p.moved, p.movedByServer = false, false
	pos, rot := p.Position(), p.Rotation()
	move, teleport := p.data.Moves.Move(pos, rot, rot.Yaw(), mgl64.Vec3{}, p.OnGround(), true, 0)
	visible := move != nil || teleport
	if !visible && !byServer {
		return
	}
	shown := &world.EntityMove{Pos: pos, Rot: rot, OnGround: p.OnGround(), Absolute: true}
	viewers, release := p.viewers()
	for _, v := range viewers {
		// Its own client is shown only where the server put it, the other
		// viewers only a move they can see.
		if self := p.c != nil && v == world.Viewer(p.c); self && !byServer || !self && !visible {
			continue
		}
		v.ViewEntityMove(p, shown)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// Move moves the player from one position to another in the world, by adding the delta passed to the current
// position of the player.
// Move also rotates the player, adding deltaYaw and deltaPitch to the respective values.
func (p *Player) Move(deltaPos mgl64.Vec3, deltaYaw, deltaPitch float64) {
	if p.Dead() || (deltaPos.ApproxEqual(mgl64.Vec3{}) && mgl64.FloatEqual(deltaYaw, 0) && mgl64.FloatEqual(deltaPitch, 0)) {
		p.onGround = true
		p.updateFallState(deltaPos.Y())
		return
	}
	if p.immobile {
		if mgl64.FloatEqual(deltaYaw, 0) && mgl64.FloatEqual(deltaPitch, 0) {
			// If only the position was changed, don't continue with the movement when immobile.
			return
		}
		// Still update rotation if it was changed.
		deltaPos = mgl64.Vec3{}
	}
	var (
		pos         = p.Position()
		res, resRot = pos.Add(deltaPos), p.Rotation().Add(cube.Rotation{deltaYaw, deltaPitch})
	)
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleMove(ctx, res, resRot); ctx.Cancelled() {
		if p.hasClient() && pos.ApproxEqual(p.Position()) {
			// The position of the player was changed and the event cancelled. This means we still need to notify the
			// player of this movement change.
			p.teleport(pos)
		}
		return
	}
	// The viewers are shown the move with the next tick, as Java's
	// ServerEntity shows that of a player, so that the moves of a crowd go
	// out in one batch a tick rather than in one for every move.
	p.moved = true
	if !p.client().ClientMoving() {
		p.movedByServer = true
	}
	if ac := p.anticheat(); ac != nil && !ac.clientMove {
		// The server moved a player with a client: its client moves on from
		// where it had it until it sees the move.
		p.startGrace(ac.conf.Grace.Teleport)
	}

	p.setPosition(res)
	p.data.Rot = resRot
	if deltaPos.Len() <= 3 {
		// Only update velocity if the player is not moving too fast to prevent potential OOMs.
		p.data.Vel = deltaPos
		p.checkBlockCollisions(deltaPos)
	}

	horizontalVel := deltaPos
	horizontalVel[1] = 0
	if p.Gliding() {
		if deltaPos.Y() >= -0.5 {
			p.fallDistance = 1.0
		}
		if p.collidedHorizontally {
			if force := horizontalVel.Len()*10.0 - 3.0; force > 0.0 {
				p.tx.PlaySound(p.Position(), sound.Fall{Distance: force})
				p.Hurt(force, entity.GlideDamageSource{})
			}
		}
	}

	beforePos := cube.PosFromVec3(pos.Add(mgl64.Vec3{0, p.EyeHeight()}))
	afterPos := cube.PosFromVec3(res.Add(mgl64.Vec3{0, p.EyeHeight()}))
	if p.movementChunkReady(beforePos) && p.movementChunkReady(afterPos) {
		_, submergedBefore := p.tx.Liquid(beforePos)
		_, submergedAfter := p.tx.Liquid(afterPos)
		if submergedBefore != submergedAfter {
			// Player wasn't either breathing before and no longer isn't, or wasn't breathing before and now is,
			// so send the updated metadata.
			p.client().ViewEntityState(p)
		}
	}

	p.onGround = p.checkOnGround(deltaPos)
	p.emitMovementGameEvents(deltaPos)
	p.updateFallState(deltaPos.Y())

	p.checkMovementStatistics(deltaPos)
}

// checkMovementStatistics ports the exhaustion of
// ServerPlayer.checkMovementStatistics: 0.01 per metre swimming, under water
// or wading, 0.1 per metre sprinting on the ground and none otherwise.
func (p *Player) checkMovementStatistics(delta mgl64.Vec3) {
	if p.Riding() != nil || delta == (mgl64.Vec3{}) {
		return
	}
	full := float32(math.Round(float64(float32(delta.Len()) * 100)))
	horizontal := float32(math.Round(float64(float32(math.Hypot(delta[0], delta[2])) * 100)))
	switch {
	case p.Swimming() || p.insideOfWater():
		p.Exhaust(float64(0.01 * full * 0.01))
	case p.touchingWater():
		p.Exhaust(float64(0.01 * horizontal * 0.01))
	case p.onClimbable():
	case p.onGround && p.Sprinting():
		p.Exhaust(float64(0.1 * horizontal * 0.01))
	}
}

// onClimbable ports LivingEntity.onClimbable: the player is in a climbable
// block, or an open trapdoor above a ladder facing the same way.
func (p *Player) onClimbable() bool {
	if p.GameMode() == world.GameModeSpectator {
		return false
	}
	pos := cube.PosFromVec3(p.Position())
	b, loaded := p.tx.BlockLoaded(pos)
	if !loaded {
		return false
	}
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	name := strings.TrimPrefix(state.Name, "minecraft:")
	if p.Gliding() && bedStateTag(name, "minecraft:can_glide_through") {
		return false
	}
	if bedStateTag(name, "minecraft:climbable") {
		return true
	}
	if !strings.HasSuffix(name, "_trapdoor") || state.Properties["open"] != "true" {
		return false
	}
	below, loaded := p.tx.BlockLoaded(pos.Side(cube.FaceDown))
	if !loaded {
		return false
	}
	ladder, err := world.CanonicalStateOf(below)
	return err == nil && strings.TrimPrefix(ladder.Name, "minecraft:") == "ladder" && ladder.Properties["facing"] == state.Properties["facing"]
}

// Displace moves the player by a server-authoritative relative delta, clipped against block collision boxes.
func (p *Player) Displace(deltaPos mgl64.Vec3) {
	if p.Dead() || deltaPos.ApproxEqual(mgl64.Vec3{}) {
		return
	}
	pos := p.Position()
	deltaPos, velocity := p.mc.CheckCollision(p.tx, p, pos, deltaPos)
	if deltaPos.ApproxEqual(mgl64.Vec3{}) {
		return
	}
	res := pos.Add(deltaPos)
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityTeleport(p, res)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
	p.data.Pos, p.data.Vel = res, velocity
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.Teleport }))
	p.checkBlockCollisions(deltaPos)
	p.onGround = p.checkOnGround(deltaPos)
	p.emitMovementGameEvents(deltaPos)
	p.updateFallState(deltaPos[1])
}

// Position returns the current position of the player. It may be changed as the player moves or is moved
// around the world.
func (p *Player) Position() mgl64.Vec3 {
	return p.data.Pos
}

// Velocity returns the players current velocity. If there is an attached session, this will be empty.
func (p *Player) Velocity() mgl64.Vec3 {
	return p.data.Vel
}

// SetVelocity updates the player's velocity. If there is an attached session, this will just send
// the velocity to the player session for the player to update.
func (p *Player) SetVelocity(velocity mgl64.Vec3) {
	if p.simulated() {
		p.data.Vel = velocity
		return
	}
	// The client applies the velocity within a round trip: the anti-cheat
	// lets its moves carry it until then.
	p.recordImpulse(velocity)
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityVelocity(p, velocity)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// Rotation returns the yaw and pitch of the player in degrees. Yaw is horizontal rotation (rotation around the
// vertical axis, 0 when facing forward), pitch is vertical rotation (rotation around the horizontal axis, also 0
// when facing forward).
func (p *Player) Rotation() cube.Rotation {
	return p.data.Rot
}

// Riding returns the entity handle this player is riding, if any.
func (p *Player) Riding() *world.EntityHandle {
	return p.data.Riding
}

// SetRiding updates the entity handle this player is riding.
func (p *Player) SetRiding(r *world.EntityHandle) {
	if r == nil && p.data.Riding != nil {
		p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.Dismount }))
	}
	p.data.Riding = r
}

// RiderSeat returns the seat of the vehicle the player rides, or last rode.
func (p *Player) RiderSeat() world.RiderSeat {
	return p.data.Seat
}

// SetRiderSeat changes the seat the player rides in and shows it to viewers.
func (p *Player) SetRiderSeat(s world.RiderSeat) {
	if p.data.Seat == s {
		return
	}
	p.data.Seat = s
	p.updateState()
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

// Experience returns the amount of experience the player has.
func (p *Player) Experience() int {
	return p.experience.Experience()
}

// EnchantmentSeed is a seed used to calculate random enchantments with enchantment tables.
func (p *Player) EnchantmentSeed() int64 {
	return p.enchantSeed
}

// ResetEnchantmentSeed resets the enchantment seed to a new random value.
func (p *Player) ResetEnchantmentSeed() {
	p.enchantSeed = rand.Int64()
}

// AddExperience adds experience to the player.
func (p *Player) AddExperience(amount int) int {
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleExperienceGain(ctx, &amount); ctx.Cancelled() {
		return 0
	}
	before := p.experience.Level()
	level, _ := p.experience.Add(amount)
	if level/5 > before/5 {
		p.PlaySound(sound.LevelUp{})
	} else if amount > 0 {
		p.PlaySound(sound.Experience{})
	}
	p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
	return amount
}

// RemoveExperience removes experience from the player.
func (p *Player) RemoveExperience(amount int) {
	p.experience.Add(-amount)
	p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
}

// ExperienceLevel returns the experience level of the player.
func (p *Player) ExperienceLevel() int {
	return p.experience.Level()
}

// SetExperienceLevel sets the experience level of the player. The level must have a value between 0 and 2,147,483,647,
// otherwise the method panics.
func (p *Player) SetExperienceLevel(level int) {
	p.experience.SetLevel(level)
	p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
}

// ExperienceProgress returns the experience progress of the player.
func (p *Player) ExperienceProgress() float64 {
	return p.experience.Progress()
}

// SetExperienceProgress sets the experience progress of the player. The progress must have a value between 0.0 and 1.0, otherwise
// the method panics.
func (p *Player) SetExperienceProgress(progress float64) {
	p.experience.SetProgress(progress)
	p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
}

// CanCollectExperience checks if the player can collect experience, which is true if the player is not dead,
// is in a game mode that allows interaction and if 100ms have passed since the last experience collection.
func (p *Player) CanCollectExperience() bool {
	if p.Dead() || !p.GameMode().AllowsInteraction() {
		return false
	}
	if last := p.lastXPPickup; last != nil && time.Since(*last) < time.Millisecond*100 {
		return false
	}
	return true
}

// CollectExperience makes the player collect the experience points passed, adding it to the experience manager. A bool
// is returned indicating whether the player was able to collect the experience or not, due to the 100ms delay between
// experience collection or if the player was dead or in a game mode that doesn't allow collection.
func (p *Player) CollectExperience(value int) bool {
	if !p.CanCollectExperience() {
		return false
	}
	value = p.mendItems(value)
	now := time.Now()
	p.lastXPPickup = &now
	if value > 0 {
		return p.AddExperience(value) > 0
	}

	p.PlaySound(sound.Experience{})
	return true
}

// mendItems handles the mending enchantment when collecting experience, it then returns the leftover experience.
func (p *Player) mendItems(xp int) int {
	mendingItems := make([]item.Stack, 0, 6)
	held, offHand := p.HeldItems()
	if _, ok := offHand.Enchantment(enchantment.Mending); ok && offHand.Durability() < offHand.MaxDurability() {
		mendingItems = append(mendingItems, offHand)
	}
	if _, ok := held.Enchantment(enchantment.Mending); ok && held.Durability() < held.MaxDurability() {
		mendingItems = append(mendingItems, held)
	}
	for _, i := range p.Armour().Items() {
		if i.Durability() == i.MaxDurability() {
			continue
		}
		if _, ok := i.Enchantment(enchantment.Mending); ok {
			mendingItems = append(mendingItems, i)
		}
	}
	length := len(mendingItems)
	if length == 0 {
		return xp
	}
	foundItem := mendingItems[rand.IntN(length)]
	repairAmount := math.Min(float64(foundItem.MaxDurability()-foundItem.Durability()), float64(xp*2))
	repairedItem := foundItem.WithDurability(foundItem.Durability() + int(repairAmount))
	if repairAmount >= 2 {
		// mending removes 1 experience point for every 2 durability points. If the repaired durability is less than 2,
		// then no experience is removed.
		xp -= int(math.Ceil(repairAmount / 2))
	}
	if offHand.Equal(foundItem) {
		p.SetHeldItems(held, repairedItem)
	} else if held.Equal(foundItem) {
		p.SetHeldItems(repairedItem, offHand)
	} else if slot, ok := p.Armour().Inventory().First(foundItem); ok {
		_ = p.Armour().Inventory().SetItem(slot, repairedItem)
	}
	return xp
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

// Tick ticks the entity, performing actions such as checking if the player is still breaking a block.
func (p *Player) Tick(tx *world.Tx, current int64) {
	p.bindTx(tx)
	p.showMove()
	p.wardenSpawnTracker.Tick()
	p.itemUseTick = current
	p.nextTick()
	if p.Dead() {
		p.ReleaseShoulderParrots()
		p.cancelItemUse()
		return
	}
	if p.sleeping {
		if bed, loaded := tx.BlockLoaded(p.sleepPos); loaded {
			switch bed := bed.(type) {
			case block.StrawBed:
				if _, valid := bed.SleepHeight(p.sleepPos, tx); !valid || !tx.IsDarkOutside() {
					p.wakeInTx(tx)
				}
			case block.Bed:
				if !tx.IsDarkOutside() {
					p.wakeInTx(tx)
				}
			default:
				p.wakeInTx(tx)
			}
		}
	}
	if p.sleeping {
		p.timeSinceRest = 0
		// Player.tick counts the ticks slept up to 100.
		p.sleepTimer = min(p.sleepTimer+1, 100)
	} else if p.timeSinceRest < int32(^uint32(0)>>1) {
		p.timeSinceRest++
	}
	blockName := ""
	if b, loaded := p.tx.BlockLoaded(cube.PosFromVec3(p.Position())); loaded {
		blockName, _ = b.EncodeBlock()
	}
	if p.insideOfWater() || p.Flying() || p.sleeping || p.fallDistance > 0.5 || blockName == "minecraft:powder_snow" {
		p.ReleaseShoulderParrots()
	}
	p.tickSpectating()
	p.tickCarriedMaps(current)
	p.tickScoreboard(tx, current)
	p.tickMapFrames(current)
	liquidPos := cube.PosFromVec3(p.Position())
	if p.movementChunkReady(liquidPos) {
		if _, ok := p.tx.Liquid(liquidPos); !ok {
			p.StopSwimming()
			if _, ok := p.Armour().Helmet().Item().(item.TurtleShell); ok {
				p.AddEffect(effect.New(effect.WaterBreathing, 1, time.Second*10).WithoutParticles())
			}
		}
	}

	if _, ok := p.Armour().Chestplate().Item().(item.Elytra); ok && p.Gliding() {
		if p.glideTicks += 1; p.glideTicks%20 == 0 {
			d := p.damageItem(p.Armour().Chestplate(), 1)
			p.armour.SetChestplate(d)
			if d.Durability() < 2 {
				p.StopGliding()
			}
		}
	}

	p.checkBlockCollisions(p.data.Vel)
	if frozen := p.data.FrozenTicks; frozen > 0 || p.data.InPowderSnow || p.data.WasInPowderSnow {
		entity.TickFreezing(p.tx, p, p.data)
		if p.data.FrozenTicks != frozen {
			p.updateState()
		}
	}
	p.onGround = p.checkOnGround(mgl64.Vec3{})
	p.tickFrostWalker()
	p.checkEntitySteppers()

	p.effects.Tick(p, p.tx)

	p.tickFood()
	p.tickAirSupply()

	if p.Position()[1] < float64(p.tx.Range()[0]) {
		p.Hurt(4, entity.VoidDamageSource{})
	}
	if p.insideOfSolid() {
		p.Hurt(1, entity.SuffocationDamageSource{})
	}

	if p.OnFireDuration() > 0 {
		p.fireTicks -= 1
		if !p.GameMode().AllowsTakingDamage() || p.OnFireDuration() <= 0 || p.tx.RainingAt(cube.PosFromVec3(p.Position())) {
			p.Extinguish()
		}
		if p.OnFireDuration()%time.Second == 0 {
			p.Hurt(1, block.FireDamageSource{})
		}
	}

	if p.blockingDelayTicks > 0 {
		p.blockingDelayTicks--
		if p.blockingDelayTicks <= 0 {
			p.SetBlockingDelay(0)
		}
	}

	if p.usingItem {
		p.tickItemUse(current)
	}
	if p.breaking {
		p.ContinueBreaking(p.breakingFace)
	}
	p.checkMenuReach()

	p.expireCooldowns()
	p.tickAttackStrength()

	p.client().SendDebugShapes(tx.World().Dimension())
	p.client().SendHudUpdates()

	if p.prevWorld != tx.World() && p.prevWorld != nil {
		p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.WorldChange }))
		p.resetTimer()
		p.Handler().HandleChangeWorld(p, p.prevWorld, tx.World())
	}
	p.prevWorld = tx.World()

	// A player spectating an entity stays with it, as ServerPlayer.tick moves
	// it to its camera, and does not fall.
	if p.simulated() && !p.Immobile() && p.spectating == nil {
		m := p.mc.TickMovement(p, p.Position(), p.Velocity(), p.Rotation(), p.Rotation(), p.tx)
		m.Send()

		p.data.Vel = m.Velocity()
		p.Move(m.Position().Sub(p.Position()), 0, 0)
	} else {
		p.data.Vel = mgl64.Vec3{}
	}

	p.portalTravel.StopPortalContact()
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

// SetEntityOnShoulder ports ServerPlayer.setEntityOnShoulder: a player on
// the ground, out of water and powder snow and riding nothing seats the
// entity saved as data on its left shoulder, or else its right one.
func (p *Player) SetEntityOnShoulder(data map[string]any) bool {
	if p.Riding() != nil || !p.OnGround() || p.touchingWater() || p.data.InPowderSnow {
		return false
	}
	switch {
	case p.shoulderLeft == nil:
		p.shoulderLeft = data
	case p.shoulderRight == nil:
		p.shoulderRight = data
	default:
		return false
	}
	p.shoulderSatTick = p.tx.CurrentTick()
	return true
}

// ShoulderEntities returns the saved native entity compounds currently riding
// the player's shoulders. The returned maps must be treated as read-only.
func (p *Player) ShoulderEntities() (left, right map[string]any) {
	return p.shoulderLeft, p.shoulderRight
}

// ReleaseShoulderParrots reintroduces saved shoulder entities into the world
// above the player, as ServerPlayer.removeEntitiesOnShoulder, at least a
// second after one last sat down. It is also called automatically for source
// dismount conditions.
func (p *Player) ReleaseShoulderParrots() {
	if p.tx == nil || p.shoulderLeft == nil && p.shoulderRight == nil || p.shoulderSatTick+20 >= p.tx.CurrentTick() {
		return
	}
	reg := p.tx.World().EntityRegistry()
	spawn := reg.Config().SpawnMobFromNBT
	if spawn == nil {
		return
	}
	release := func(data map[string]any) map[string]any {
		if data == nil {
			return nil
		}
		id, _ := data["id"].(string)
		if id == "" {
			return data
		}
		opts := world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, .7}), Rotation: p.Rotation()}
		if h := spawn(reg, id, opts, world.CloneNativeEntityNBT(data)); h != nil {
			p.tx.AddEntity(h)
			return nil
		}
		return data
	}
	p.shoulderLeft = release(p.shoulderLeft)
	p.shoulderRight = release(p.shoulderRight)
}

func (p *Player) tickCarriedMaps(current int64) {
	var seen [64]int64
	seenCount := 0
	for _, inv := range [...]*inventory.Inventory{p.inv, p.offHand, p.armour.Inventory()} {
		for slot := range inv.Size() {
			stack, err := inv.Item(slot)
			if err == nil {
				p.tickCarriedMapStack(stack, current, &seen, &seenCount)
			}
		}
	}
}

func (p *Player) tickCarriedMapStack(stack item.Stack, current int64, seen *[64]int64, seenCount *int) {
	mapItem, ok := stack.Item().(item.FilledMap)
	if !ok {
		return
	}
	mapID := mapItem.NativeID
	if !mapItem.HasNativeID {
		if !mapItem.HasJavaID {
			return
		}
		var err error
		mapID, err = p.tx.World().MapIDByJavaID(mapItem.JavaID)
		if err != nil {
			return
		}
	}
	for _, existing := range seen[:*seenCount] {
		if existing == mapID {
			return
		}
	}
	if *seenCount < len(seen) {
		seen[*seenCount] = mapID
		*seenCount++
	}
	data, err := p.tx.UpdateMap(mapID, cube.PosFromVec3(p.Position()))
	if err != nil {
		return
	}
	if data.ColorsChanged || current%5 == 0 {
		if data.Tracking {
			data.TrackedObjects = p.trackedMapObjects(mapID)
		}
		_ = p.client().ViewMapData(data)
	}
}

// updatesMapFrames reports whether the player updates the map frames of
// its world for the players: the living player with the lowest UUID does.
func (p *Player) updatesMapFrames() bool {
	for e := range p.tx.Players() {
		if other, ok := e.(*Player); ok && !other.Dead() {
			otherID, ownID := other.UUID(), p.UUID()
			if bytes.Compare(otherID[:], ownID[:]) < 0 {
				return false
			}
		}
	}
	return true
}

func (p *Player) tickMapFrames(current int64) {
	if current%5 != 0 || !p.updatesMapFrames() {
		return
	}
	p.tx.ForEachMapFrame(func(pos cube.Pos, frame world.MapFrameInfo) {
		mapID, ok := resolveMapID(p.tx, frame.NativeID, frame.HasNativeID, frame.JavaID, frame.HasJavaID)
		if !ok {
			return
		}
		data, err := p.tx.World().MapData(mapID)
		dimensionID, ok := world.DimensionID(p.tx.World().Dimension())
		if err != nil || !ok || !data.Tracking || data.Dimension != int32(dimensionID) {
			return
		}
		data.TrackedObjects = p.trackedMapObjects(mapID)
		viewers := p.tx.Viewers(pos.Vec3Centre())
		for _, viewer := range viewers {
			if mv, ok := viewer.(client.MapViewer); ok {
				_ = mv.ViewMapData(data)
			}
		}
		p.tx.ReleaseViewers(viewers)
	})
}

func (p *Player) trackedMapObjects(mapID int64) []world.MapTrackedObject {
	objects := make([]world.MapTrackedObject, 0, 8)
	for entity := range p.tx.Entities() {
		carrier, ok := entity.(interface {
			Inventory() *inventory.Inventory
			Armour() *inventory.Armour
			HeldItems() (item.Stack, item.Stack)
		})
		if !ok {
			continue
		}
		if invisible, ok := entity.(interface{ Invisible() bool }); ok && invisible.Invisible() {
			continue
		}
		if playerCarriesMap(p.tx, carrier, mapID) {
			objects = append(objects, world.MapTrackedObject{Entity: entity.H()})
		}
	}
	p.tx.ForEachMapFrame(func(pos cube.Pos, frame world.MapFrameInfo) {
		if frameMapID, ok := resolveMapID(p.tx, frame.NativeID, frame.HasNativeID, frame.JavaID, frame.HasJavaID); ok && frameMapID == mapID {
			framePos := pos
			objects = append(objects, world.MapTrackedObject{Block: &framePos})
		}
	})
	return objects
}

func playerCarriesMap(tx *world.Tx, carrier interface {
	Inventory() *inventory.Inventory
	Armour() *inventory.Armour
	HeldItems() (item.Stack, item.Stack)
}, mapID int64) bool {
	main, offHand := carrier.HeldItems()
	if carriedMapMatches(tx, main, mapID) || carriedMapMatches(tx, offHand, mapID) {
		return true
	}
	for _, inv := range [...]*inventory.Inventory{carrier.Inventory(), carrier.Armour().Inventory()} {
		for slot := range inv.Size() {
			stack, err := inv.Item(slot)
			if err == nil && carriedMapMatches(tx, stack, mapID) {
				return true
			}
		}
	}
	return false
}

func carriedMapMatches(tx *world.Tx, stack item.Stack, mapID int64) bool {
	mapItem, ok := stack.Item().(item.FilledMap)
	if !ok {
		return false
	}
	id, ok := resolveMapID(tx, mapItem.NativeID, mapItem.HasNativeID, mapItem.JavaID, mapItem.HasJavaID)
	return ok && id == mapID
}

func resolveMapID(tx *world.Tx, nativeID int64, hasNativeID bool, javaID int32, hasJavaID bool) (int64, bool) {
	if hasNativeID {
		return nativeID, true
	}
	if !hasJavaID {
		return 0, false
	}
	id, err := tx.World().MapIDByJavaID(javaID)
	return id, err == nil
}

func (p *Player) tickItemUse(current int64) {
	p.itemUseTick = current
	p.usingTicks++
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

// TravelThroughPortal handles the player touching a portal block.
func (p *Player) TravelThroughPortal(tx *world.Tx, target world.Dimension) {
	if !p.GameMode().HasCollision() {
		return
	}
	p.portalTravel.EnterPortal(p, tx, target)
}

// checkCollisions checks the player's block collisions.
func (p *Player) checkBlockCollisions(vel mgl64.Vec3) {
	if p.GameMode() == world.GameModeSpectator {
		return
	}
	entityBBox := Type.BBox(p).Translate(p.Position())
	deltaX, deltaY, deltaZ := vel[0], vel[1], vel[2]

	entity.CheckEntityInsiders(p.tx, entityBBox.Grow(-0.0001), p)

	grown := entityBBox.Extend(vel).Grow(0.25)
	low, high := grown.Min(), grown.Max()
	minX, minY, minZ := int(math.Floor(low[0])), int(math.Floor(low[1])), int(math.Floor(low[2]))
	maxX, maxY, maxZ := int(math.Ceil(high[0])), int(math.Ceil(high[1])), int(math.Ceil(high[2]))

	// A prediction of one BBox per block, plus an additional 2, in case
	blocks := make([]cube.BBox, 0, (maxX-minX)*(maxY-minY)*(maxZ-minZ)+2)
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				pos := cube.Pos{x, y, z}
				b, loaded := p.tx.BlockLoaded(pos)
				// BlockLoaded never waits; unready positions have no collision shape to apply yet.
				if !loaded {
					continue
				}
				for _, box := range b.Model().BBox(pos, p.tx) {
					blocks = append(blocks, box.Translate(pos.Vec3()))
				}
			}
		}
	}

	// epsilon is the epsilon used for thresholds for change used for change in position and velocity.
	const epsilon = 0.001

	if !mgl64.FloatEqualThreshold(deltaY, 0, epsilon) {
		// First we move the entity BBox on the Y axis.
		for _, blockBBox := range blocks {
			deltaY = entityBBox.YOffset(blockBBox, deltaY)
		}
		entityBBox = entityBBox.Translate(mgl64.Vec3{0, deltaY})
	}
	if !mgl64.FloatEqualThreshold(deltaX, 0, epsilon) {
		// Then on the X axis.
		for _, blockBBox := range blocks {
			deltaX = entityBBox.XOffset(blockBBox, deltaX)
		}
		entityBBox = entityBBox.Translate(mgl64.Vec3{deltaX})
	}
	if !mgl64.FloatEqualThreshold(deltaZ, 0, epsilon) {
		// And finally on the Z axis.
		for _, blockBBox := range blocks {
			deltaZ = entityBBox.ZOffset(blockBBox, deltaZ)
		}
	}

	p.collidedHorizontally = !mgl64.FloatEqual(deltaX, vel[0]) || !mgl64.FloatEqual(deltaZ, vel[2])
	p.collidedVertically = !mgl64.FloatEqual(deltaY, vel[1])
}

// movementChunkReady reports whether a block-position read is safe without loading or waiting.
func (p *Player) movementChunkReady(pos cube.Pos) bool {
	_, ready := p.tx.ChunkState(world.ChunkPosition(pos))
	return ready
}

// checkEntitySteppers checks if the player is standing on any EntityStepper blocks.
func (p *Player) checkEntitySteppers() {
	if !p.OnGround() {
		return
	}
	low, high := p.blocksUnder()
	for x := low[0]; x <= high[0]; x++ {
		for z := low[2]; z <= high[2]; z++ {
			pos := cube.Pos{x, low[1], z}
			b, loaded := p.tx.BlockLoaded(pos)
			if !loaded {
				continue
			}
			stepper, ok := b.(block.EntityStepper)
			if !ok {
				continue
			}
			stepper.EntityStepOn(pos, p.tx, p)
			return
		}
	}
}

// checkOnGround checks if the player is currently considered to be on the ground.
// maxGroundProbe bounds the descent checkOnGround probes below a player:
// more than the 3.92 blocks a player falls at most in a tick.
const maxGroundProbe = 8.0

func (p *Player) checkOnGround(deltaPos mgl64.Vec3) bool {
	if p.GameMode() == world.GameModeSpectator {
		return false
	}
	// Probe straight down (0.05 plus this tick's descent) instead of along the
	// whole movement, so a block the player falls past at its side is not ground.
	// The descent is bound: a move of any length must not make the probe, a
	// loop over blocks, run long.
	box := Type.BBox(p).Translate(p.Position()).Extend(mgl64.Vec3{0, -0.05}).Extend(mgl64.Vec3{0, max(math.Min(deltaPos[1], 0), -maxGroundProbe)})
	b := box.Grow(1)
	epsilon := mgl64.Vec3{mgl64.Epsilon, mgl64.Epsilon, mgl64.Epsilon}
	low, high := cube.PosFromVec3(b.Min().Add(epsilon)), cube.PosFromVec3(b.Max().Sub(epsilon))

	unavailable := false
	for x := low[0]; x <= high[0]; x++ {
		for z := low[2]; z <= high[2]; z++ {
			for y := low[1]; y < high[1]; y++ {
				pos := cube.Pos{x, y, z}
				b, loaded := p.tx.BlockLoaded(pos)
				if !loaded {
					unavailable = true
					continue
				}
				for _, bb := range b.Model().BBox(pos, p.tx) {
					if bb.Translate(pos.Vec3()).IntersectsWith(box) {
						return true
					}
				}
			}
		}
	}
	if unavailable {
		return p.onGround
	}
	return false
}

// Scale returns the scale modifier of the Player. The default value for a normal scale is 1. A scale of 0
// will make the Player completely invisible.
func (p *Player) Scale() float64 {
	return p.scale
}

// SetScale changes the scale modifier of the Player. The default value for a normal scale is 1. A scale of 0
// will make the Player completely invisible.
func (p *Player) SetScale(s float64) {
	p.scale = s
	p.updateState()
	// The box of the player changed, which its client applies a round trip
	// later.
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.Teleport }))
}

// OnGround checks if the player is considered to be on the ground.
func (p *Player) OnGround() bool {
	if p.GameMode() == world.GameModeSpectator {
		return false
	}
	if p.simulated() {
		return p.mc.OnGround()
	}
	return p.onGround
}

// EyeHeight returns the eye height of the player: 1.62, 1.26 if player is sneaking or 0.52 if the player is
// swimming, gliding or crawling.
func (p *Player) EyeHeight() float64 {
	switch {
	case p.swimming || p.crawling || p.gliding:
		return 0.4
	case p.sneaking:
		return 1.27
	default:
		return 1.62
	}
}

// TorsoHeight returns the player's torso offset above its feet. This is 0.1 blocks below the eye height.
func (p *Player) TorsoHeight() float64 {
	return p.EyeHeight() - 0.1
}

// Emote makes the player perform the emote with the UUID passed. The viewers
// of the position of the player are shown the emote, the client of the
// player too if it views that position: the player itself is not left out.
func (p *Player) Emote(emote uuid.UUID) {
	viewers := p.tx.Viewers(p.Position())
	// Emotes are cosmetic but frequent; reusing the viewer slice prevents busy
	// lobbies from generating a stream of temporary allocations as players
	// spam emotes.
	for _, viewer := range viewers {
		viewer.ViewEmote(p, emote)
	}
	p.tx.ReleaseViewers(viewers)
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

// canReach checks if a player can reach a position with its current range. The range depends on if the player
// is either survival or creative mode. Blocks are reached by canReachBlock, which the anti-cheat configures.
func (p *Player) canReach(pos mgl64.Vec3) bool {
	return p.canReachPoint(pos)
}

func (p *Player) unlockedRecipeIDs() []string {
	ids := make([]string, 0, len(p.unlockedRecipes))
	for id := range p.unlockedRecipes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// UnlockRecipes unlocks the recipes of the Java Edition IDs passed for the
// player, keeping those of registered recipes, recipe.KnownJavaID, in its
// persistent unlock state, and shows the client the IDs passed as newly
// unlocked.
func (p *Player) UnlockRecipes(ids []string) {
	for _, id := range ids {
		if recipe.KnownJavaID(id) {
			p.unlockedRecipes[id] = struct{}{}
		}
	}
	p.client().ViewRecipesUnlocked(ids)
}

// UnlockedRecipeIDs returns the player's persisted Java recipe keys.
func (p *Player) UnlockedRecipeIDs() []string { return p.unlockedRecipeIDs() }

// RaidOmenPosition returns the village position recorded when Bad Omen became
// Raid Omen. The position is zero when there is no pending Raid Omen.
func (p *Player) RaidOmenPosition() cube.Pos {
	if p.raidOmenPosition == nil {
		return cube.Pos{}
	}
	return *p.raidOmenPosition
}

// RaidOmenLevel returns the current Raid Omen level, or zero when absent.
func (p *Player) RaidOmenLevel() int {
	for _, eff := range p.Effects() {
		if eff.Type() == effect.RaidOmen {
			return eff.Level()
		}
	}
	return 0
}

// GrantHeroOfTheVillage grants the raid victory effect for the specified number
// of game ticks.
func (p *Player) GrantHeroOfTheVillage(_ *world.Tx, durationTicks int) {
	if durationTicks <= 0 {
		return
	}
	p.AddEffect(effect.New(effect.HeroOfTheVillage, 1, time.Duration(durationTicks)*time.Second/20))
}

// TryActivateBadOmen converts Bad Omen to Raid Omen when a raid can begin at
// the player's current position.
func (p *Player) TryActivateBadOmen(tx *world.Tx, amplifier int) bool {
	if p.GameMode() == world.GameModeSpectator || tx.World().Difficulty() == world.DifficultyPeaceful {
		return false
	}
	pos := cube.PosFromVec3(p.Position())
	if !tx.TryApplyBadOmen(p, pos, amplifier) {
		return false
	}
	p.raidOmenPosition = &pos
	p.AddEffect(effect.New(effect.RaidOmen, amplifier+1, effect.RaidOmenDuration))
	return true
}

// ActivateRaidOmen attempts to start or extend the raid at the stored position.
func (p *Player) ActivateRaidOmen(tx *world.Tx) bool {
	if p.raidOmenPosition == nil || p.GameMode() == world.GameModeSpectator {
		return false
	}
	_ = tx.TryApplyRaidOmen(p)
	p.raidOmenPosition = nil
	return true
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

// Handler returns the Handler of the player.
func (p *Player) Handler() Handler {
	return p.h
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
