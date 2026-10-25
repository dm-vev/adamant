package entity

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
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
	b.ai.home.write(state)
	if b.offersGenerated {
		state["Offers"] = writeMerchantOffers(b.offers)
	}
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), state)
	if !b.hasWanderTarget {
		delete(javaState, "wander_target")
	}
	if !b.ai.home.set() {
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
	b := &wanderingTraderBehaviour{
		health: NewHealthManager(c.Health, 20), effects: NewEffectManager(),
		despawnDelay: c.DespawnDelay, wanderTarget: c.WanderTarget, hasWanderTarget: c.HasWanderTarget,
		offers: c.Offers, offersGenerated: c.OffersGenerated,
	}
	b.ai.home = c.Home
	return b
}

type wanderingTraderBehaviour struct {
	health  *HealthManager
	effects *EffectManager
	ai      MobAI
	panic   *PanicGoal
	dead    bool

	despawnDelay    int
	wanderTarget    cube.Pos
	hasWanderTarget bool

	offers          []MerchantOffer
	offersGenerated bool
	trading         tradingPlayer

	using    item.Stack
	useTicks int
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

// traderSpeed is the movement speed attribute of 0.7 the trader keeps from
// Mob.
const traderSpeed = .7

func (b *wanderingTraderBehaviour) mobAI() *MobAI { return &b.ai }

func (b *wanderingTraderBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	t := &WanderingTrader{Ent: e}
	b.trading.tick(tx, e.data.Pos)
	b.tickUsingItem(t, tx)
	b.ai.MovementSpeed, b.ai.EyeHeight = traderSpeed, 1.62
	return b.ai.Tick(e, t, b.registerGoals)
}

// registerGoals ports WanderingTrader.registerGoals.
func (b *wanderingTraderBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	ai.Goals.Add(0, NewFloatGoal(m))
	ai.Goals.Add(0, &traderUseItemGoal{trader: b, item: item.NewStack(item.Potion{Type: potion.Invisibility()}, 1), sound: "disappeared",
		canUse: func(m *Mob) bool { return darkOutside(m.tx) && !b.invisible() }})
	ai.Goals.Add(0, &traderUseItemGoal{trader: b, item: item.NewStack(item.Bucket{Content: item.MilkBucketContent()}, 1), sound: "reappeared",
		canUse: func(m *Mob) bool { return brightOutside(m.tx) && b.invisible() }})
	ai.Goals.Add(1, &tradeWithPlayerGoal{goalFlags: goalFlags{GoalJump | GoalMove}, trader: b})
	for _, avoided := range [...]struct {
		isType   func(world.Entity) bool
		distance float64
	}{
		{func(e world.Entity) bool { return zombieClass[e.H().Type().EncodeEntity()] }, 8},
		{entityIs("minecraft:evoker"), 12},
		{entityIs("minecraft:vindicator"), 8},
		{entityIs("minecraft:vex"), 8},
		{entityIs("minecraft:pillager"), 15},
		{entityIs("minecraft:illusioner"), 12},
		{entityIs("minecraft:zoglin"), 10},
	} {
		ai.Goals.Add(1, NewAvoidEntityGoal(avoided.isType, avoided.distance, .5, .5))
	}
	b.panic = NewPanicGoal(.5)
	ai.Goals.Add(1, b.panic)
	ai.Goals.Add(1, &lookAtTradingPlayerGoal{NewLookAtPlayerGoal(8), b})
	ai.Goals.Add(2, &wanderToPositionGoal{goalFlags: goalFlags{GoalMove}, trader: b, stopDistance: 2, speedModifier: .35})
	ai.Goals.Add(4, &moveTowardsRestrictionGoal{goalFlags: goalFlags{GoalMove}, speedModifier: .35})
	ai.Goals.Add(8, NewWaterAvoidingRandomStrollGoal(.35))
	// InteractGoal: a LookAtPlayerGoal that always looks at a player within
	// three blocks and holds the trader still meanwhile.
	interact := NewLookAtPlayerGoal(3)
	interact.Probability = 1
	interact.setFlags(GoalLook | GoalMove)
	ai.Goals.Add(9, interact)
	mobs := NewLookAtPlayerGoal(8)
	mobs.IsType = isMobEntity
	ai.Goals.Add(10, mobs)
}

// invisible reports if the trader has drunk invisibility.
func (b *wanderingTraderBehaviour) invisible() bool {
	_, ok := b.effects.Effect(effect.Invisibility)
	return ok
}

// tradeWithPlayerGoal ports TradeWithPlayerGoal: a trader on the ground
// stands still while a player within four blocks trades with it, and ends
// the trade when it is hurt or falls into water.
type tradeWithPlayerGoal struct {
	goalFlags
	trader *wanderingTraderBehaviour
}

func (g *tradeWithPlayerGoal) CanUse(m *Mob) bool {
	if !m.Alive() || m.InWater() || !m.OnGround() || m.HurtRecently() {
		return false
	}
	if g.trader.trading.user == nil {
		return false
	}
	p, ok := g.trader.trading.user.Entity(m.tx)
	return ok && m.DistanceToSqr(p) <= 16
}

func (g *tradeWithPlayerGoal) Start(m *Mob) { m.AI.nav.Stop() }

func (g *tradeWithPlayerGoal) Stop(*Mob) { g.trader.trading.user = nil }

// lookAtTradingPlayerGoal ports LookAtTradingPlayerGoal: a trader looks at
// the player it trades with.
type lookAtTradingPlayerGoal struct {
	*LookAtPlayerGoal
	trader *wanderingTraderBehaviour
}

func (g *lookAtTradingPlayerGoal) CanUse(m *Mob) bool {
	if g.trader.trading.user == nil {
		return false
	}
	g.lookAt = g.trader.trading.user
	return true
}

// wanderToPositionGoal ports WanderingTrader.WanderToPositionGoal: a trader
// walks to its wander target in legs of at most ten blocks and forgets it
// once within two.
type wanderToPositionGoal struct {
	goalFlags
	trader                      *wanderingTraderBehaviour
	stopDistance, speedModifier float64
}

func (g *wanderToPositionGoal) tooFar(m *Mob, distance float64) bool {
	return g.trader.wanderTarget.Vec3Centre().Sub(m.data.Pos).LenSqr() >= distance*distance
}

func (g *wanderToPositionGoal) CanUse(m *Mob) bool {
	return g.trader.hasWanderTarget && g.tooFar(m, g.stopDistance)
}

func (g *wanderToPositionGoal) Stop(m *Mob) {
	g.trader.hasWanderTarget = false
	m.AI.nav.Stop()
}

func (g *wanderToPositionGoal) Tick(m *Mob) {
	b := g.trader
	if !b.hasWanderTarget || !m.AI.nav.IsDone() {
		return
	}
	target := b.wanderTarget
	if g.tooFar(m, 10) {
		dir := mgl64.Vec3{float64(target.X()) - m.data.Pos[0], float64(target.Y()) - m.data.Pos[1], float64(target.Z()) - m.data.Pos[2]}.Normalize()
		pos := dir.Mul(10).Add(m.data.Pos)
		m.AI.nav.MoveToPos(m, pos[0], pos[1], pos[2], g.speedModifier)
		return
	}
	m.AI.nav.MoveToPos(m, float64(target.X()), float64(target.Y()), float64(target.Z()), g.speedModifier)
}

// darkOutside ports Level.isDarkOutside.
func darkOutside(tx *world.Tx) bool {
	return tx.World().Dimension().TimeCycle() && !brightOutside(tx)
}

// traderUseDuration is the use duration of potions and milk.
const traderUseDuration = 32

// traderUseItemGoal ports UseItemGoal: a trader drinks an invisibility potion
// when it is dark outside and it is visible, and milk when it is bright
// outside and it is invisible, saying it disappeared or reappeared.
type traderUseItemGoal struct {
	goalFlags
	trader *wanderingTraderBehaviour
	item   item.Stack
	sound  string
	canUse func(m *Mob) bool
}

func (g *traderUseItemGoal) CanUse(m *Mob) bool { return g.canUse(m) }

func (g *traderUseItemGoal) CanContinueToUse(*Mob) bool { return g.trader.useTicks > 0 }

func (g *traderUseItemGoal) Start(m *Mob) {
	g.trader.using, g.trader.useTicks = g.item, traderUseDuration
	(&WanderingTrader{Ent: m.Ent}).viewItems(m.tx)
}

func (g *traderUseItemGoal) Stop(m *Mob) {
	g.trader.using, g.trader.useTicks = item.Stack{}, 0
	(&WanderingTrader{Ent: m.Ent}).viewItems(m.tx)
	m.tx.PlaySound(m.data.Pos, sound.NativeEvent{SoundType: g.sound, EntityType: "minecraft:wandering_trader", EntityUniqueID: m.H().UniqueID(), ExtraData: -1})
}

// tickUsingItem ports LivingEntity.updatingUsingItem for the trader: once
// it drank its potion or milk, the potion gives it invisibility and the
// milk clears its effects.
func (b *wanderingTraderBehaviour) tickUsingItem(t *WanderingTrader, tx *world.Tx) {
	if b.useTicks == 0 {
		return
	}
	if b.useTicks--; b.useTicks > 0 {
		return
	}
	// Bedrock clients play the drinking sounds for the drink event.
	_, milk := b.using.Item().(item.Bucket)
	t.viewAction(DrinkAction{Milk: milk})
	// LivingEntity.completeUsingItem leaves the emptied bottle or bucket in
	// the hand until UseItemGoal stops.
	if p, ok := b.using.Item().(item.Potion); ok {
		for _, eff := range p.Type.Effects() {
			t.AddEffect(eff)
		}
		b.using = item.NewStack(item.GlassBottle{}, 1)
	} else {
		for _, eff := range t.Effects() {
			t.RemoveEffect(eff.Type())
		}
		b.using = item.NewStack(item.Bucket{}, 1)
	}
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
	h := t.b().ai.home
	return h.pos, h.radius, h.set()
}

// SetHome restricts the trader to radius blocks around pos.
func (t *WanderingTrader) SetHome(pos cube.Pos, radius int) {
	t.b().ai.home = mobHome{pos: pos, radius: radius}
}

// lastAttacker returns the living entity that last hurt the trader in the
// last 100 ticks and when it did so.
func (t *WanderingTrader) lastAttacker(tx *world.Tx) (world.Entity, int64, bool) {
	attacker, stamp := t.b().ai.Mob(t.Ent, t).LastHurtBy()
	if attacker == nil {
		return nil, 0, false
	}
	return attacker, int64(stamp), true
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
	d, ok := b.ai.DamageCooldown(t.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(src)
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
	knockBack(t, src, force, height)
}

func (t *WanderingTrader) AddEffect(e effect.Effect)    { t.b().effects.Add(e, t) }
func (t *WanderingTrader) RemoveEffect(e effect.Type)   { t.b().effects.Remove(e, t) }
func (t *WanderingTrader) Effects() []effect.Effect     { return t.b().effects.Effects() }
func (t *WanderingTrader) Speed() float64               { return t.b().ai.speed }
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
		spawnTradeXP(tx, t.Position(), 3+int(b.ai.Mob(t.Ent, t).Random().NextInt(4)))
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
