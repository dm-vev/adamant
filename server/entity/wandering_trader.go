package entity

import (
	"fmt"
	"hash/fnv"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// WanderingTraderType is Java's minecraft:wandering_trader: a merchant that
// sells the wandering trader trade sets, drinks invisibility at night and
// leaves the world when its despawn delay runs out.
var WanderingTraderType wanderingTraderType

type wanderingTraderType struct{}

func (wanderingTraderType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		wanderingTraderConfig{}.Apply(d)
	}
	if b := d.Data.(*wanderingTraderBehaviour); !b.seeded {
		id := h.UUID()
		hash := fnv.New64a()
		_, _ = hash.Write(id[:])
		b.random, b.seeded = java26.NewLegacyRandom(int64(hash.Sum64())), true
	}
	return &WanderingTrader{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (wanderingTraderType) EncodeEntity() string        { return "minecraft:wandering_trader" }
func (wanderingTraderType) BBox(world.Entity) cube.BBox { return cube.Box(-.3, 0, -.3, .3, 1.95, .3) }

func (wanderingTraderType) DecodeNBT(m map[string]any, d *world.EntityData) {
	s := ReadNativeMobJavaState(m)
	if s == nil {
		s = m
	}
	c := wanderingTraderConfig{DespawnDelay: int(nbtconv.Int32(s, "DespawnDelay")), Home: readMobHome(s)}
	if _, ok := s["Health"]; ok {
		c.Health, c.healthSet = float64(nbtconv.Float32(s, "Health")), true
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.healthSet = h.Current, true
	}
	c.WanderTarget, c.HasWanderTarget = villagerReadPos(s["wander_target"])
	if _, ok := s["Offers"]; ok {
		c.Offers, c.OffersGenerated = readMerchantOffers(s), true
	}
	c.Apply(d)
}

func (wanderingTraderType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*wanderingTraderBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{
		{Name: "minecraft:health", Base: h.MaxHealth(), Current: h.Health(), DefaultMin: 0, DefaultMax: h.MaxHealth(), Min: 0, Max: h.MaxHealth()},
		{Name: "minecraft:movement", Base: .5, Current: .5, DefaultMin: 0, DefaultMax: .5, Min: 0, Max: .5},
	})
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Health": float32(h.Health()), "Attributes": attrs})
	state := map[string]any{"Health": float32(h.Health()), "DespawnDelay": int32(b.despawnDelay), "Age": int32(0)}
	if b.hasWanderTarget {
		state["wander_target"] = villagerPosNBT(b.wanderTarget)
	}
	b.home.write(state)
	if b.offersGenerated {
		state["Offers"] = writeMerchantOffers(b.offers)
	}
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), state)
	if !b.hasWanderTarget {
		delete(javaState, "wander_target")
	}
	if !b.home.set() {
		delete(javaState, "home_pos")
		delete(javaState, "home_radius")
	}
	return MergeNativeMobJavaState(native, javaState)
}

type wanderingTraderConfig struct {
	Health          float64
	DespawnDelay    int
	WanderTarget    cube.Pos
	HasWanderTarget bool
	// Home restricts where the trader wanders. The zero value means no home.
	Home            mobHome
	Offers          []MerchantOffer
	OffersGenerated bool
	healthSet       bool
}

func (c wanderingTraderConfig) Apply(d *world.EntityData) { d.Data = c.New() }

func (c wanderingTraderConfig) New() *wanderingTraderBehaviour {
	if !c.healthSet {
		c.Health = 20
	}
	if c.Home == (mobHome{}) {
		c.Home = noHome
	}
	return &wanderingTraderBehaviour{
		health: NewHealthManager(c.Health, 20), effects: NewEffectManager(),
		mc:           &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true},
		despawnDelay: c.DespawnDelay, wanderTarget: c.WanderTarget, hasWanderTarget: c.HasWanderTarget, home: c.Home,
		offers: c.Offers, offersGenerated: c.OffersGenerated,
	}
}

// traderGoal is the movement goal the trader runs, in Java goal priority
// order.
type traderGoal int

const (
	traderIdle traderGoal = iota
	traderAvoid
	traderPanic
	traderWander
	traderReturnHome
	traderStroll
)

type wanderingTraderBehaviour struct {
	health  *HealthManager
	effects *EffectManager
	mc      *MovementComputer
	random  java26.Random
	seeded  bool
	dead    bool

	despawnDelay    int
	wanderTarget    cube.Pos
	hasWanderTarget bool
	home            mobHome

	offers          []MerchantOffer
	offersGenerated bool
	trading         tradingPlayer

	navigator    Navigator
	goal         traderGoal
	panic        panicState
	noActionTime int
	look         *world.EntityHandle
	lookTicks    int

	using    item.Stack
	useTicks int

	lastHurtBy      *world.EntityHandle
	lastHurtByStamp int64
}

// WanderingTrader is a wandering trader entity.
type WanderingTrader struct{ *Ent }

// NewWanderingTrader creates a wandering trader that never despawns.
func NewWanderingTrader(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(WanderingTraderType, wanderingTraderConfig{})
}

func (t *WanderingTrader) b() *wanderingTraderBehaviour {
	return t.Behaviour().(*wanderingTraderBehaviour)
}

// Tick ticks the trader and removes it once its despawn delay runs out while
// nobody trades with it.
func (t *WanderingTrader) Tick(tx *world.Tx, current int64) {
	b := t.b()
	b.effects.Tick(t, tx)
	t.Ent.Tick(tx, current)
	if b.despawnDelay > 0 && b.trading.user == nil {
		if b.despawnDelay--; b.despawnDelay == 0 {
			_ = t.CloseIn(tx)
		}
	}
}

// traderSpeed is the trader's movement speed attribute.
const traderSpeed = .7

func (b *wanderingTraderBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	t := &WanderingTrader{Ent: e}
	customer, trading := b.trading.tick(tx, e.data.Pos)
	b.tickUseItem(t, tx)
	b.tickNoActionTime(e, tx)
	var v mgl64.Vec3
	var r cube.Rotation
	if trading {
		// TradeWithPlayerGoal holds the trader still while
		// LookAtTradingPlayerGoal turns it to its customer.
		b.stopGoal()
		v, r = e.data.Vel, faceTowards(e.data.Rot, EyePosition(e), EyePosition(customer))
		v[0], v[2] = 0, 0
	} else {
		v, r = b.tickGoals(e, tx)
	}
	if frogWater(tx, cube.PosFromVec3(e.data.Pos.Add(mgl64.Vec3{0, .4}))) && b.random.NextFloat() < .8 {
		// FloatGoal.
		v[1] += .04
	}
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}

func (b *wanderingTraderBehaviour) posMob(e *Ent, tx *world.Tx) randomPosMob {
	return randomPosMob{tx: tx, pos: e.data.Pos, home: b.home, profile: GroundProfile{Width: .6, Height: 1.95, FollowRange: 16}, random: &b.random}
}

// tickNoActionTime ports Mob.checkDespawn's noActionTime: it resets while a
// player is within 32 blocks, and random strolls stop after 100 ticks
// without one.
func (b *wanderingTraderBehaviour) tickNoActionTime(e *Ent, tx *world.Tx) {
	b.noActionTime++
	for p := range tx.Players() {
		if p.Position().Sub(e.data.Pos).LenSqr() < 32*32 {
			b.noActionTime = 0
			return
		}
	}
}

// stopGoal stops the running movement goal. WanderToPositionGoal forgets its
// target when it stops.
func (b *wanderingTraderBehaviour) stopGoal() {
	if b.goal == traderWander {
		b.hasWanderTarget = false
	}
	b.goal = traderIdle
	b.navigator.Path, b.navigator.Index, b.navigator.HasTarget = nil, 0, false
}

// startGoal replaces the running goal with goal walking to target, if the
// trader can path there.
func (b *wanderingTraderBehaviour) startGoal(e *Ent, m randomPosMob, goal traderGoal, target cube.Pos) bool {
	path := FindGroundPath(m.tx, cube.PosFromVec3(e.data.Pos), target, m.profile)
	if len(path) == 0 {
		return false
	}
	b.stopGoal()
	b.goal = goal
	b.navigator.Path, b.navigator.Index, b.navigator.Target, b.navigator.HasTarget, b.navigator.LastComputeTick = path, 0, target, true, navigatorTick(e)
	return true
}

func (b *wanderingTraderBehaviour) navigating() bool {
	return b.navigator.Index < len(b.navigator.Path)
}

// tickGoals runs the trader's movement goals: avoiding illagers and zombies
// and panicking (priority 1), walking to its wander target (2), returning
// home (4) and strolling (8). A running goal only yields to a goal of a
// higher priority.
func (b *wanderingTraderBehaviour) tickGoals(e *Ent, tx *world.Tx) (mgl64.Vec3, cube.Rotation) {
	m := b.posMob(e, tx)
	switch b.goal {
	case traderAvoid, traderPanic, traderReturnHome, traderStroll:
		if !b.navigating() {
			b.stopGoal()
		}
	case traderWander:
		if !b.wanderTargetFar(e, 2) {
			b.stopGoal()
		}
	}
	if b.goal == traderIdle || b.goal > traderPanic {
		if threat := b.threat(e, tx); threat != nil {
			if goal, ok := avoidTarget(e, m, threat); ok {
				b.startGoal(e, m, traderAvoid, goal)
			}
		}
	}
	if (b.goal == traderIdle || b.goal > traderPanic) && b.panic.shouldPanic(tx.CurrentTick()) {
		if goal, ok := b.panic.target(e, m); ok {
			b.startGoal(e, m, traderPanic, goal)
		}
	}
	if (b.goal == traderIdle || b.goal > traderWander) && b.wanderTargetFar(e, 2) {
		b.stopGoal()
		b.goal = traderWander
	}
	if b.goal == traderWander {
		b.tickWander(e, tx, m)
	}
	if (b.goal == traderIdle || b.goal > traderReturnHome) && !b.home.contains(cube.PosFromVec3(e.data.Pos)) {
		if goal, ok := m.randomPosTowards(16, 7, b.home.pos.Vec3Middle()); ok {
			b.startGoal(e, m, traderReturnHome, goal)
		}
	}
	if b.goal == traderIdle && b.noActionTime < 100 && b.random.NextInt(120) == 0 {
		if goal, ok := m.randomPos(10, 7); ok {
			b.startGoal(e, m, traderStroll, goal)
		}
	}

	modifier := .35
	if b.goal == traderAvoid || b.goal == traderPanic {
		modifier = .5
	}
	v, r, moving := steer(e, &b.navigator, groundSpeed(traderSpeed, modifier), .6)
	if !moving {
		r = b.tickLook(e, tx, r)
	}
	return v, r
}

// avoidedMobs are the AvoidEntityGoal classes of the trader with their
// distances, in goal order. Zombie covers its subclasses.
var avoidedMobs = []struct {
	match    func(id string) bool
	distance float64
}{
	{func(id string) bool { return zombieClass[id] }, 8},
	{func(id string) bool { return id == "minecraft:evoker" }, 12},
	{func(id string) bool { return id == "minecraft:vindicator" }, 8},
	{func(id string) bool { return id == "minecraft:vex" }, 8},
	{func(id string) bool { return id == "minecraft:pillager" }, 15},
	{func(id string) bool { return id == "minecraft:illusioner" }, 12},
	{func(id string) bool { return id == "minecraft:zoglin" }, 10},
}

// threat returns the nearest visible mob the trader avoids.
func (b *wanderingTraderBehaviour) threat(e *Ent, tx *world.Tx) world.Entity {
	for _, avoided := range avoidedMobs {
		var nearest world.Entity
		best := avoided.distance * avoided.distance
		box := cube.Box(-avoided.distance, -3, -avoided.distance, avoided.distance, 3, avoided.distance).Translate(e.data.Pos)
		for other := range tx.EntitiesWithin(box) {
			if !avoided.match(other.H().Type().EncodeEntity()) {
				continue
			}
			if l, ok := other.(Living); ok && l.Dead() {
				continue
			}
			if d := other.Position().Sub(e.data.Pos).LenSqr(); d <= best && lineVisible(e, tx, other) {
				nearest, best = other, d
			}
		}
		if nearest != nil {
			return nearest
		}
	}
	return nil
}

// avoidTarget ports AvoidEntityGoal.canUse: a position up to 16 blocks away
// that lies farther from the threat than the mob does.
func avoidTarget(e *Ent, m randomPosMob, threat world.Entity) (cube.Pos, bool) {
	goal, ok := m.randomPosAway(16, 7, threat.Position())
	if !ok || goal.Vec3Middle().Sub(threat.Position()).LenSqr() < e.data.Pos.Sub(threat.Position()).LenSqr() {
		return cube.Pos{}, false
	}
	return goal, true
}

// wanderTargetFar reports whether the wander target is at least distance
// blocks from the trader.
func (b *wanderingTraderBehaviour) wanderTargetFar(e *Ent, distance float64) bool {
	return b.hasWanderTarget && b.wanderTarget.Vec3Centre().Sub(e.data.Pos).Len() >= distance
}

// tickWander ports WanderToPositionGoal.tick: once a path ends the trader
// paths towards its target in legs of at most ten blocks.
func (b *wanderingTraderBehaviour) tickWander(e *Ent, tx *world.Tx, m randomPosMob) {
	if b.navigating() {
		return
	}
	target := b.wanderTarget
	if b.wanderTargetFar(e, 10) {
		dir := b.wanderTarget.Vec3().Sub(e.data.Pos).Normalize()
		target = cube.PosFromVec3(e.data.Pos.Add(dir.Mul(10)))
	}
	for dy := range 15 {
		candidate := target.Add(cube.Pos{0, 7 - dy, 0})
		if groundWalkable(tx, candidate, m.profile) {
			b.navigator.MoveTo(tx, cube.PosFromVec3(e.data.Pos), candidate, m.profile, navigatorTick(e))
			return
		}
	}
}

// tickLook ports InteractGoal and LookAtPlayerGoal: an idle trader faces a
// player within three blocks, or now and then any mob within eight.
func (b *wanderingTraderBehaviour) tickLook(e *Ent, tx *world.Tx, r cube.Rotation) cube.Rotation {
	if b.lookTicks > 0 {
		b.lookTicks--
		if target, ok := b.look.Entity(tx); ok && target.Position().Sub(e.data.Pos).LenSqr() <= 64 {
			return faceTowards(r, EyePosition(e), EyePosition(target))
		}
		b.lookTicks = 0
	}
	var nearest world.Entity
	best := 9.0
	for p := range tx.Players() {
		if d := p.Position().Sub(e.data.Pos).LenSqr(); d <= best {
			nearest, best = p, d
		}
	}
	if nearest == nil && b.random.NextFloat() < .02 {
		best = 64
		for other := range tx.EntitiesWithin(cube.Box(-8, -3, -8, 8, 3, 8).Translate(e.data.Pos)) {
			if _, ok := other.(Living); !ok || other.H() == e.H() {
				continue
			}
			if d := other.Position().Sub(e.data.Pos).LenSqr(); d <= best {
				nearest, best = other, d
			}
		}
	}
	if nearest == nil {
		return r
	}
	b.look, b.lookTicks = nearest.H(), 40+int(b.random.NextInt(40))
	return faceTowards(r, EyePosition(e), EyePosition(nearest))
}

// darkOutside ports Level.isDarkOutside.
func darkOutside(tx *world.Tx) bool {
	return tx.World().Dimension().TimeCycle() && !brightOutside(tx)
}

// traderUseDuration is the use duration of potions and milk.
const traderUseDuration = 32

// tickUseItem ports the trader's two UseItemGoals: it drinks an
// invisibility potion when it is dark outside and it is visible, and milk
// when it is bright outside and it is invisible.
func (b *wanderingTraderBehaviour) tickUseItem(t *WanderingTrader, tx *world.Tx) {
	if b.useTicks == 0 {
		_, invisible := b.effects.Effect(effect.Invisibility)
		switch {
		case darkOutside(tx) && !invisible:
			b.startUsing(t, tx, item.NewStack(item.Potion{Type: potion.Invisibility()}, 1))
		case brightOutside(tx) && invisible:
			b.startUsing(t, tx, item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1))
		}
		return
	}
	if b.useTicks--; b.useTicks > 0 {
		return
	}
	// Bedrock clients play the drinking sounds for the drink event; the
	// trader then announces that it disappeared or reappeared.
	_, milk := b.using.Item().(item.Bucket)
	t.viewAction(DrinkAction{Milk: milk})
	finish := "reappeared"
	if p, ok := b.using.Item().(item.Potion); ok {
		for _, eff := range p.Type.Effects() {
			t.AddEffect(eff)
		}
		finish = "disappeared"
	} else {
		for _, eff := range t.Effects() {
			t.RemoveEffect(eff.Type())
		}
	}
	b.using = item.Stack{}
	t.viewItems(tx)
	tx.PlaySound(t.Position(), sound.NativeEvent{SoundType: finish, EntityType: "minecraft:wandering_trader", EntityUniqueID: t.H().UniqueID(), ExtraData: -1})
}

func (b *wanderingTraderBehaviour) startUsing(t *WanderingTrader, tx *world.Tx, stack item.Stack) {
	b.using, b.useTicks = stack, traderUseDuration
	t.viewItems(tx)
}

// viewItems shows the item in the trader's hand and whether it is drinking.
func (t *WanderingTrader) viewItems(tx *world.Tx) {
	viewers := tx.Viewers(t.Position())
	for _, v := range viewers {
		v.ViewEntityItems(t)
		v.ViewEntityState(t)
	}
	tx.ReleaseViewers(viewers)
}

// HeldItems returns the potion or milk the trader is drinking.
func (t *WanderingTrader) HeldItems() (mainHand, offHand item.Stack) {
	return t.b().using, item.Stack{}
}

// UsingItem reports whether the trader is drinking.
func (t *WanderingTrader) UsingItem() bool { return t.b().useTicks > 0 }

// DespawnDelay returns the ticks until the trader leaves, or 0 if it stays.
func (t *WanderingTrader) DespawnDelay() int { return t.b().despawnDelay }

// SetDespawnDelay sets the ticks until the trader leaves. Zero keeps it in
// the world.
func (t *WanderingTrader) SetDespawnDelay(ticks int) { t.b().despawnDelay = max(0, ticks) }

// WanderTarget returns the position the trader walks to, if any.
func (t *WanderingTrader) WanderTarget() (cube.Pos, bool) {
	return t.b().wanderTarget, t.b().hasWanderTarget
}

// SetWanderTarget makes the trader walk to pos.
func (t *WanderingTrader) SetWanderTarget(pos cube.Pos) {
	t.b().wanderTarget, t.b().hasWanderTarget = pos, true
}

// Home returns the centre and radius the trader stays around, or false if it
// roams freely.
func (t *WanderingTrader) Home() (cube.Pos, int, bool) {
	h := t.b().home
	return h.pos, h.radius, h.set()
}

// SetHome restricts the trader to radius blocks around pos.
func (t *WanderingTrader) SetHome(pos cube.Pos, radius int) {
	t.b().home = mobHome{pos: pos, radius: radius}
}

// lastAttacker returns the living entity that last hurt the trader in the
// last 100 ticks and the tick it did so.
func (t *WanderingTrader) lastAttacker(tx *world.Tx) (world.Entity, int64, bool) {
	b := t.b()
	if b.lastHurtBy == nil || tx.CurrentTick()-b.lastHurtByStamp > 100 {
		return nil, 0, false
	}
	attacker, ok := b.lastHurtBy.Entity(tx)
	if !ok {
		return nil, 0, false
	}
	if l, living := attacker.(Living); living && l.Dead() {
		return nil, 0, false
	}
	return attacker, b.lastHurtByStamp, true
}

func (t *WanderingTrader) Health() float64        { return t.b().health.Health() }
func (t *WanderingTrader) MaxHealth() float64     { return t.b().health.MaxHealth() }
func (t *WanderingTrader) SetMaxHealth(h float64) { t.b().health.SetMaxHealth(h) }
func (t *WanderingTrader) Dead() bool             { return t.b().dead || t.Health() <= 0 }

func (t *WanderingTrader) Heal(h float64, _ world.HealingSource) float64 {
	if t.Dead() || h <= 0 {
		return 0
	}
	old := t.Health()
	t.b().health.AddHealth(h)
	return t.Health() - old
}

func (t *WanderingTrader) Hurt(d float64, src world.DamageSource) (float64, bool) {
	b := t.b()
	if t.Dead() || d <= 0 || (src.Fire() && t.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	b.health.AddHealth(-d)
	b.panic.hurt(src, t.tx.CurrentTick())
	if attacker := mobKiller(src); attacker != nil {
		if _, living := attacker.(Living); living {
			b.lastHurtBy, b.lastHurtByStamp = attacker.H(), t.tx.CurrentTick()
		}
	}
	t.viewAction(HurtAction{})
	onMobHurt(t.tx, t)
	if t.Dead() {
		b.dead = true
		b.trading.user = nil
		t.viewAction(DeathAction{})
		dropAllDeathLoot(t.tx, t, src)
		_ = t.CloseIn(t.tx)
	}
	return d, true
}

func (t *WanderingTrader) KnockBack(src mgl64.Vec3, force, height float64) {
	d := t.Position().Sub(src)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	t.SetVelocity(d)
}

func (t *WanderingTrader) AddEffect(e effect.Effect)    { t.b().effects.Add(e, t) }
func (t *WanderingTrader) RemoveEffect(e effect.Type)   { t.b().effects.Remove(e, t) }
func (t *WanderingTrader) Effects() []effect.Effect     { return t.b().effects.Effects() }
func (t *WanderingTrader) Speed() float64               { return traderSpeed }
func (t *WanderingTrader) SetSpeed(float64)             {}
func (t *WanderingTrader) hasEffect(e effect.Type) bool { _, ok := t.b().effects.Effect(e); return ok }

// Interact ports WanderingTrader.mobInteract: a trader nobody else trades
// with opens its trade screen.
func (t *WanderingTrader) Interact(tx *world.Tx, user item.User, _ *item.UseContext) bool {
	b := t.b()
	if t.Dead() || b.trading.user != nil {
		return false
	}
	t.bindTx(tx)
	opener, ok := user.(interface {
		OpenMerchant(world.Entity, Merchant, *world.Tx)
	})
	if !ok {
		return false
	}
	if offers, err := t.MerchantOffers(); err != nil || len(offers) == 0 {
		return err == nil
	}
	opener.OpenMerchant(t, t, tx)
	return true
}

// ensureOffers ports WanderingTrader.updateTrades: two buying offers, two
// uncommon and five common offers, generated when first needed.
func (t *WanderingTrader) ensureOffers() error {
	b := t.b()
	if b.offersGenerated {
		return nil
	}
	if t.tx == nil {
		return fmt.Errorf("wandering trader trade generation requires an active world transaction")
	}
	ctx := tradeContext{tx: t.tx, id: t.H().UUID(), pos: t.Position()}
	var offers []MerchantOffer
	for _, set := range [...]string{"minecraft:wandering_trader/buying", "minecraft:wandering_trader/uncommon", "minecraft:wandering_trader/common"} {
		var err error
		if offers, err = appendTradeSetOffers(ctx, set, 0, offers); err != nil {
			return err
		}
	}
	b.offers, b.offersGenerated = offers, true
	return nil
}

// MerchantName returns the trade screen title.
func (t *WanderingTrader) MerchantName() string { return "entity.wandering_trader.name" }

// MerchantProfession returns the trader's entity type, as it has no
// profession.
func (t *WanderingTrader) MerchantProfession() string { return "minecraft:wandering_trader" }

// MerchantLevel returns 1: wandering traders have a single trade tier.
func (t *WanderingTrader) MerchantLevel() int { return 1 }

// MerchantOffers returns the trader's offers.
func (t *WanderingTrader) MerchantOffers() ([]MerchantOffer, error) {
	if err := t.ensureOffers(); err != nil {
		return nil, err
	}
	return append([]MerchantOffer(nil), t.b().offers...), nil
}

// MerchantOffersFor returns the trader's offers, which carry no reputation
// discounts.
func (t *WanderingTrader) MerchantOffersFor(world.Entity) ([]MerchantOffer, error) {
	return t.MerchantOffers()
}

// CompleteMerchantTrade ports AbstractVillager.notifyTrade and
// WanderingTrader.rewardTradeXp: the offer is used once and 3-6 experience
// drops if it rewards experience.
func (t *WanderingTrader) CompleteMerchantTrade(tx *world.Tx, _ world.Entity, index int) (bool, error) {
	if tx != nil {
		t.bindTx(tx)
	}
	if err := t.ensureOffers(); err != nil {
		return false, err
	}
	b := t.b()
	if index < 0 || index >= len(b.offers) {
		return false, nil
	}
	o := &b.offers[index]
	if o.Input.Empty() || o.Output.Empty() || o.MaxUses <= 0 || o.Uses >= o.MaxUses {
		return false, nil
	}
	o.Uses++
	if tx != nil && o.RewardXP {
		spawnTradeXP(tx, t.Position(), 3+int(b.random.NextInt(4)))
	}
	return true, nil
}

// StartTrading claims the trader for a trading player.
func (t *WanderingTrader) StartTrading(user *world.EntityHandle) bool {
	return t.b().trading.start(user)
}

// StopTrading ends a trade with user.
func (t *WanderingTrader) StopTrading(user *world.EntityHandle) { t.b().trading.stop(user) }

func (t *WanderingTrader) viewAction(a world.EntityAction) {
	vs := t.tx.Viewers(t.Position())
	for _, v := range vs {
		v.ViewEntityAction(t, a)
	}
	t.tx.ReleaseViewers(vs)
}

var (
	_ Living          = (*WanderingTrader)(nil)
	_ Interactable    = (*WanderingTrader)(nil)
	_ TradingMerchant = (*WanderingTrader)(nil)
)
