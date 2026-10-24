package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewStrider creates an adult, unsaddled strider.
func NewStrider(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(StriderType, StriderConfig{})
}

// StriderType is the registered Java strider type.
var StriderType striderType

type striderType struct{}

// Strider is a passive nether animal that walks on lava and shivers out of
// it, ridden with a saddle and steered with a warped fungus on a stick.
type Strider struct{ *Ent }

// StriderConfig supplies a strider's persistent state.
type StriderConfig struct {
	Health  float64
	Age     int
	Saddled bool
}

type striderState struct {
	health   *HealthManager
	effects  *EffectManager
	breeding breeding
	animalAI
	age                        int
	speed                      float64
	saddled, dead, suffocating bool
	passenger                  *world.EntityHandle
	steering                   itemBasedSteering
	tempt                      *TemptGoal
}

func (c StriderConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c StriderConfig) New() *striderState {
	h := c.Health
	if h <= 0 {
		h = 20
	}
	return &striderState{health: NewHealthManager(h, 20), effects: NewEffectManager(), age: c.Age, saddled: c.Saddled, speed: .175}
}

func (striderType) EncodeEntity() string { return "minecraft:strider" }
func (striderType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		StriderConfig{}.Apply(d)
	}
	return &Strider{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (striderType) BBox(e world.Entity) cube.BBox {
	if s, ok := e.(*Strider); ok && s.b().age < 0 {
		return cube.Box(-.225, 0, -.225, .225, .85, .225)
	}
	return cube.Box(-.45, 0, -.45, .45, 1.7, .45)
}

// striderAttributes are the attributes of a strider as Bedrock knows them.
func striderAttributes() []NativeMobAttribute {
	return []NativeMobAttribute{{Name: "minecraft:health", Base: 20, Current: 20, DefaultMax: 20, Max: 20}, {Name: "minecraft:movement", Base: .175, Current: .175, DefaultMax: .175, Max: .175}, {Name: "minecraft:attack_damage", DefaultMax: 0}}
}

func (striderType) DecodeNBT(n map[string]any, d *world.EntityData) {
	j := ReadNativeMobJavaState(n)
	if j == nil {
		j = n
	}
	c := StriderConfig{Age: int(nbtconv.Int32(j, "Age")), Saddled: nbtconv.Uint8(n, "Saddle") != 0}
	if c.Age == 0 && nbtconv.Bool(j, "IsBaby") {
		c.Age = babyStartAge
	}
	if x, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.Health = x.Current
	} else if _, ok := j["Health"]; ok {
		c.Health = float64(nbtconv.Float32(j, "Health"))
	}
	s := c.New()
	s.breeding.decodeNBT(j)
	s.suffocating = nbtconv.Bool(j, "Suffocating")
	s.steering.boostTime, s.steering.boostTimeTotal = int(nbtconv.Int32(j, "BoostTime")), int(nbtconv.Int32(j, "BoostTimeMax"))
	s.steering.boosting = s.steering.boostTimeTotal > 0 && s.steering.boostTime <= s.steering.boostTimeTotal
	d.Data = s
}

func (striderType) EncodeNBT(d *world.EntityData) map[string]any {
	s := d.Data.(*striderState)
	attrs := striderAttributes()
	attrs[0].Current = s.health.Health()
	attrs[1].Current = s.speed
	native := map[string]any{"Attributes": EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), attrs), "IsBaby": byte(boolByte(s.age < 0)), "Saddle": byte(boolByte(s.saddled))}
	out := MergeNativeMobNBT(d.NativeNBT, native)
	j := ReadNativeMobJavaState(d.NativeNBT)
	if j == nil {
		j = make(map[string]any)
	}
	j["Age"] = int32(s.age)
	s.breeding.encodeNBT(j)
	boostTime, boostTotal := 0, 0
	if s.steering.boosting {
		boostTime, boostTotal = s.steering.boostTime, s.steering.boostTimeTotal
	}
	j["Suffocating"], j["BoostTime"], j["BoostTimeMax"] = byte(boolByte(s.suffocating)), int32(boostTime), int32(boostTotal)
	return MergeNativeMobJavaState(out, j)
}

func (s *Strider) b() *striderState { return s.Behaviour().(*striderState) }

func (s *Strider) SpeciesType() world.EntityType { return StriderType }

func (s *Strider) Tick(tx *world.Tx, n int64) {
	b := s.b()
	b.effects.Tick(s, tx)
	s.Ent.Tick(tx, n)
	if b.passenger == nil {
		return
	}
	rider, ok := b.passenger.Entity(tx)
	if !ok {
		b.passenger = nil
		return
	}
	if p, ok := rider.(interface{ Teleport(mgl64.Vec3) }); ok && isPlayerEntity(rider) {
		// A player sits 1.7 blocks up, the passenger attachment of a
		// strider, less the 0.6 of its own vehicle attachment.
		p.Teleport(s.Position().Add(mgl64.Vec3{0, 1.1, 0}))
	}
}

// PassengerPosition returns where a mob riding the strider sits, its
// passenger attachment as a Java strider has it.
func (s *Strider) PassengerPosition(passenger world.Entity) mgl64.Vec3 {
	y := 1.7
	if s.b().age < 0 {
		y = .65625
	}
	return s.Position().Add(mgl64.Vec3{0, y}).Sub(vehicleAttachmentPoint(passenger))
}

func (s *Strider) Health() float64        { return s.b().health.Health() }
func (s *Strider) MaxHealth() float64     { return s.b().health.MaxHealth() }
func (s *Strider) SetMaxHealth(v float64) { s.b().health.SetMaxHealth(v) }
func (s *Strider) Dead() bool             { return s.b().dead || s.Health() <= 0 }
func (s *Strider) Age() int               { return s.b().age }
func (s *Strider) Saddled() bool          { return s.b().saddled }

// Suffocating reports if the strider is out of lava and shivers.
func (s *Strider) Suffocating() bool { return s.b().suffocating }

// Shaking reports if the strider shivers, as Bedrock shows one that is
// suffocating.
func (s *Strider) Shaking() bool { return s.b().suffocating }

// Panicking reports whether the strider runs from danger.
func (s *Strider) Panicking() bool { return s.b().panicking() }

// IsSensitiveToWater reports that water hurts striders, as Strider does.
func (s *Strider) IsSensitiveToWater() bool { return true }

// Hurt ports LivingEntity.hurtServer for a strider, which fire never hurts
// as it is immune to it.
func (s *Strider) Hurt(d float64, src world.DamageSource) (float64, bool) {
	b := s.b()
	if s.Dead() || d <= 0 || src.Fire() {
		return 0, false
	}
	d, ok := b.ai.DamageCooldown(s.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(src)
	resetLove(s)
	s.viewAction(HurtAction{})
	onMobHurt(s.tx, s)
	if s.Dead() {
		b.dead = true
		s.viewAction(DeathAction{})
		dropAllDeathLoot(s.tx, s, src)
		s.DismountAll(s.Ent, s.tx)
		_ = s.CloseIn(s.tx)
	}
	return d, true
}

func (s *Strider) Heal(v float64, _ world.HealingSource) float64 {
	if s.Dead() || v <= 0 {
		return 0
	}
	old := s.Health()
	s.b().health.AddHealth(v)
	return s.Health() - old
}

func (s *Strider) KnockBack(src mgl64.Vec3, f, h float64) {
	d := s.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	s.SetVelocity(d)
}

func (s *Strider) viewAction(a world.EntityAction) {
	vs := s.tx.Viewers(s.Position())
	for _, v := range vs {
		v.ViewEntityAction(s, a)
	}
	s.tx.ReleaseViewers(vs)
}

func (s *Strider) AddEffect(e effect.Effect)  { s.b().effects.Add(e, s) }
func (s *Strider) RemoveEffect(t effect.Type) { s.b().effects.Remove(t, s) }
func (s *Strider) Effects() []effect.Effect   { return s.b().effects.Effects() }
func (s *Strider) Speed() float64             { return s.b().speed }
func (s *Strider) SetSpeed(v float64)         { s.b().speed = v }

// dropCustomDeathLoot drops a strider's saddle, a preserved equipment item.
func (s *Strider) dropCustomDeathLoot(bool, int) {
	if s.b().saddled {
		dropMobEquipment(s.tx, s, item.NewStack(item.Saddle{}, 1), 2, false, 0)
	}
}

// InLove reports whether the strider is in love.
func (s *Strider) InLove() bool { return s.b().breeding.InLove() }

func (s *Strider) breedingState() *breeding { return &s.b().breeding }

// ageTicks returns the age of the strider, negative while it is young.
func (s *Strider) ageTicks() *int { return &s.b().age }

// breedingFood reports whether the stack is in #strider_food.
func (s *Strider) breedingFood(stack item.Stack) bool {
	return itemNamed(stack, "minecraft:warped_fungus")
}

func (s *Strider) breedOffspring(*world.Tx, breeder) *world.EntityHandle {
	return babyAt(s).New(StriderType, StriderConfig{Age: babyStartAge})
}

// controller ports Strider.getControllingPassenger: a player riding a
// saddled strider steers it while holding a warped fungus on a stick.
func (s *Strider) controller(tx *world.Tx) world.Entity {
	b := s.b()
	if !b.saddled || b.passenger == nil {
		return nil
	}
	e, ok := b.passenger.Entity(tx)
	if !ok || !isPlayerEntity(e) {
		return nil
	}
	u, ok := e.(item.User)
	if !ok {
		return nil
	}
	main, off := u.HeldItems()
	if !itemNamed(main, "minecraft:warped_fungus_on_a_stick") && !itemNamed(off, "minecraft:warped_fungus_on_a_stick") {
		return nil
	}
	return e
}

// ControlsVehicle reports if the passenger steers the strider.
func (s *Strider) ControlsVehicle(passenger world.Entity) bool {
	c := s.controller(s.tx)
	return c != nil && passenger != nil && c.H() == passenger.H()
}

// Boost ports Strider.boost: a warped fungus on a stick used by the player
// steering the strider boosts it, unless it is boosted already.
func (s *Strider) Boost(_ *world.Tx, rider item.User) bool {
	b := s.b()
	if c := s.controller(s.tx); c == nil || c.H() != rider.H() {
		return false
	}
	return b.steering.boost(b.ai.Mob(s.Ent, s).Random())
}

// Mount seats p on the strider as Entity.startRiding does when forced, as
// /ride does: unlike riding it by hand, which Interact allows only once
// saddled, the strider need not wear a saddle.
func (s *Strider) Mount(tx *world.Tx, p world.Entity) bool {
	b := s.b()
	if p == nil || b.passenger != nil || s.Dead() {
		return false
	}
	r, ok := p.(Rider)
	if !ok || r.Riding() != nil {
		return false
	}
	b.passenger = p.H()
	seatRider(r, s)
	if isPlayerEntity(p) {
		if v, ok := p.(interface{ Teleport(mgl64.Vec3) }); ok {
			v.Teleport(s.Position().Add(mgl64.Vec3{0, 1.1, 0}))
		}
	}
	s.viewLink(tx, p.H(), true)
	return true
}

// addMobPassenger seats a mob that starts riding the strider.
func (s *Strider) addMobPassenger(tx *world.Tx, p world.Entity, _ bool) bool {
	return s.Mount(tx, p)
}

func (s *Strider) Dismount(tx *world.Tx, p world.Entity) {
	b := s.b()
	if p == nil || b.passenger != p.H() {
		return
	}
	if r, ok := p.(Rider); ok {
		r.SetRiding(nil)
	}
	s.viewLink(tx, p.H(), false)
	b.passenger = nil
}

func (s *Strider) DismountAll(_ *Ent, tx *world.Tx) {
	b := s.b()
	if b.passenger == nil {
		return
	}
	if p, ok := b.passenger.Entity(tx); ok {
		if r, ok := p.(Rider); ok {
			r.SetRiding(nil)
		}
	}
	s.viewLink(tx, b.passenger, false)
	b.passenger = nil
}

// Links returns the rider of the strider, for viewers that start seeing it.
func (s *Strider) Links() []Link {
	if h := s.b().passenger; h != nil {
		return []Link{{Ridden: s.H(), Rider: h, Type: LinkRider}}
	}
	return nil
}

// RiderSeat returns the seat of Bedrock's strider, which centres a baby
// strider riding its parent.
func (s *Strider) RiderSeat(rider world.Entity) world.RiderSeat {
	pos := mgl64.Vec3{0, 1.7, -0.2}
	if rider.H().Type() == s.H().Type() {
		pos[2] = 0
	}
	return seat{pos: pos}.riderSeat(rider)
}

func (s *Strider) viewLink(tx *world.Tx, h *world.EntityHandle, link bool) {
	vs := tx.Viewers(s.Position())
	for _, v := range vs {
		if lv, ok := v.(interface {
			ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool)
		}); ok {
			lv.ViewEntityLinkType(s.H(), h, LinkRider, link)
		} else if lv, ok := v.(interface {
			ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool)
		}); ok {
			lv.ViewEntityLink(s.H(), h, link)
		}
	}
	tx.ReleaseViewers(vs)
}

// Interact ports Strider.mobInteract: a saddled strider carries a player who
// is not holding its food or sneaking, unless its eyes are in lava, warped
// fungus feeds it, and a saddle saddles an adult.
func (s *Strider) Interact(tx *world.Tx, u item.User, c *item.UseContext) bool {
	if u == nil {
		return false
	}
	b := s.b()
	held, _ := u.HeldItems()
	food := s.breedingFood(held)
	sneaking, ok := u.(interface{ Sneaking() bool })
	if !food && b.saddled && b.passenger == nil && (!ok || !sneaking.Sneaking()) {
		// Strider.canAddPassenger: nobody mounts a strider with its eyes in
		// lava.
		if !s.eyeInLava(tx) {
			s.Mount(tx, u)
		}
		return true
	}
	if feedBreeder(s, u, c) {
		if food {
			tx.PlaySound(s.Position(), sound.Custom{Name: "entity.strider.eat", Volume: 1, Pitch: 1 + (rand.Float64()-rand.Float64())*.2})
		}
		return true
	}
	if itemNamed(held, "minecraft:saddle") && !b.saddled && b.age >= 0 && !s.Dead() {
		b.saddled = true
		if c != nil {
			c.SubtractFromCount(1)
		}
		return true
	}
	return false
}

// eyeInLava reports if the eyes of the strider are in lava.
func (s *Strider) eyeInLava(tx *world.Tx) bool {
	eye := s.Position().Add(mgl64.Vec3{0, s.b().ai.EyeHeight})
	pos := cube.PosFromVec3(eye)
	l, ok := tx.Liquid(pos)
	return ok && l.LiquidType() == "lava" && eye[1] < float64(pos[1])+liquidHeight(tx, pos, l)
}

// FinalizeSpawn ports Strider.finalizeSpawn: one in 30 adults spawns ridden
// by a zombified piglin holding a warped fungus on a stick, saddled, and one
// in ten of the others carries a baby strider.
func (s *Strider) FinalizeSpawn(reason string, r *vanilla.Random) {
	b := s.b()
	if reason == "jockey" || r == nil || b.age < 0 {
		return
	}
	b.age = 0
	if r.NextInt(30) == 0 {
		opts := world.EntitySpawnOpts{Position: s.Position(), Rotation: s.Rotation(), SpawnReason: "jockey"}
		conf := zombifiedPiglinConfig{}
		conf.gear.items[slotMainhand] = item.NewStack(item.WarpedFungusOnAStick{}, 1)
		jockey := opts.New(ZombifiedPiglinType{}, conf)
		b.saddled = true
		if s.tx != nil {
			s.Mount(s.tx, s.tx.AddEntity(jockey))
		}
		return
	}
	if r.NextInt(10) == 0 && s.tx != nil {
		opts := world.EntitySpawnOpts{Position: s.Position(), Rotation: s.Rotation(), SpawnReason: "jockey"}
		s.Mount(s.tx, s.tx.AddEntity(opts.New(StriderType, StriderConfig{Age: babyStartAge})))
	}
}

var _ Living = (*Strider)(nil)
var _ Interactable = (*Strider)(nil)
var _ VehicleController = (*Strider)(nil)
var _ PassengerDismounter = (*Strider)(nil)
var _ Linker = (*Strider)(nil)
var _ item.ItemSteerable = (*Strider)(nil)
