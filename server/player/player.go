package player

import (
	"bytes"
	"log/slog"
	"math"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
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
