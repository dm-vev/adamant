package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// BeeType is the source minecraft:bee species.
var BeeType beeType

type beeType struct{}

func (beeType) EncodeEntity() string { return "minecraft:bee" }
func (beeType) BBox(e world.Entity) cube.BBox {
	halfWidth, height := float64(float32(.55))/2, float64(float32(.5))
	if b, ok := e.(*Bee); ok && b.state().age < 0 {
		halfWidth, height = halfWidth/2, height/2
	}
	return cube.Box(-halfWidth, 0, -halfWidth, halfWidth, height, halfWidth)
}
func (beeType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		BeeConfig{}.Apply(d)
	}
	return &Bee{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (beeType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := BeeConfig{Health: 10}
	for _, a := range ReadNativeMobAttributes(n) {
		if a.Name == "minecraft:health" {
			c.Health = a.Current
		}
	}
	j := ReadNativeMobJavaState(n)
	if j != nil {
		c.HasNectar = nbtconv.Bool(j, "HasNectar")
		c.HasStung = nbtconv.Bool(j, "HasStung")
		c.crops = int(nbtconv.Int32(j, "CropsGrownSincePollination"))
		c.ticksWithoutNectar = int(nbtconv.Int32(j, "TicksSincePollination"))
		c.StayOutOfHive = int(nbtconv.Int32(j, "CannotEnterHiveTicks"))
		c.anger = int(nbtconv.Int32(j, "AngerTime"))
		c.angerTarget, _ = uuid.Parse(nbtconv.String(j, "AngerTarget"))
		c.Hive, c.Flower = decodeBeePos(j, "hive_pos"), decodeBeePos(j, "flower_pos")
		c.Age = int(nbtconv.Int32(j, "Age"))
		c.noGravity = nbtconv.Bool(j, "NoGravity")
	}
	c.Apply(d)
	d.Data.(*beeBehaviour).breeding.decodeNBT(j)
}
func (beeType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*beeBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: 10, Current: h.Health(), DefaultMin: 0, DefaultMax: 10, Min: 0, Max: 10}, {Name: "minecraft:movement", Base: .3, Current: .3, DefaultMin: 0, DefaultMax: .3, Min: 0, Max: .3}, {Name: "minecraft:flying_speed", Base: .6, Current: .6, DefaultMin: 0, DefaultMax: .6, Min: 0, Max: .6}, {Name: "minecraft:attack_damage", Base: 2, Current: 2, DefaultMin: 0, DefaultMax: 2, Min: 0, Max: 2}})
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs})
	target := ""
	if b.angerTarget != uuid.Nil {
		target = b.angerTarget.String()
	}
	j := b.breeding.encodeNBT(map[string]any{"HasNectar": byte(boolByte(b.hasNectar)), "HasStung": byte(boolByte(b.hasStung)), "CropsGrownSincePollination": int32(b.crops), "TicksSincePollination": int32(b.ticksWithoutNectar), "CannotEnterHiveTicks": int32(b.stayOut), "AngerTime": int32(b.angerTicks), "AngerTarget": target, "Age": int32(b.age), "NoGravity": byte(boolByte(b.ai.NoGravity))})
	if b.hive != nil {
		j["hive_pos"] = encodeBeePos(*b.hive)
	}
	if b.flower != nil {
		j["flower_pos"] = encodeBeePos(*b.flower)
	}
	return MergeNativeMobJavaState(m, j)
}

// decodeBeePos reads a block position saved as a compound, or nil if there
// is none.
func decodeBeePos(m map[string]any, key string) *cube.Pos {
	p, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	pos := cube.Pos{int(nbtconv.Int32(p, "X")), int(nbtconv.Int32(p, "Y")), int(nbtconv.Int32(p, "Z"))}
	return &pos
}
func encodeBeePos(p cube.Pos) map[string]any {
	return map[string]any{"X": int32(p.X()), "Y": int32(p.Y()), "Z": int32(p.Z())}
}

// BeeConfig is the configuration of a bee: its health, age, whether it
// carries nectar or stung something, how long it stays out of hives and the
// hive and flower it remembers.
type BeeConfig struct {
	Health              float64
	HasNectar, HasStung bool
	StayOutOfHive, Age  int
	Hive, Flower        *cube.Pos

	crops, ticksWithoutNectar, anger int
	angerTarget                      uuid.UUID
	noGravity                        bool
}

func (c BeeConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c BeeConfig) New() *beeBehaviour {
	if c.Health <= 0 {
		c.Health = 10
	}
	b := &beeBehaviour{health: NewHealthManager(c.Health, 10), effects: NewEffectManager(), hasNectar: c.HasNectar, hasStung: c.HasStung,
		crops: c.crops, ticksWithoutNectar: c.ticksWithoutNectar, stayOut: c.StayOutOfHive, age: c.Age, hive: c.Hive, flower: c.Flower, speed: .3,
		neutralAnger: neutralAnger{angerTicks: c.anger, angerTarget: c.angerTarget}}
	b.ai.NoGravity = c.noGravity
	return b
}

type beeBehaviour struct {
	health   *HealthManager
	effects  *EffectManager
	breeding breeding
	animalAI
	neutralAnger
	speed float64
	age   int
	dead  bool
	// hasNectar, hasStung and rolling are the flags of Bee.
	hasNectar, hasStung, rolling bool
	hive, flower                 *cube.Pos
	// timeSinceSting, ticksWithoutNectar, stayOut and crops are the counters
	// of Bee, and hiveCooldown and flowerCooldown the ticks before it looks
	// for a new hive or flower.
	timeSinceSting, ticksWithoutNectar, stayOut, crops int
	hiveCooldown, flowerCooldown                       int
	underWaterTicks                                    int
	pollinate                                          *beePollinateGoal
	goToHive                                           *beeGoToHiveGoal
	shown                                              beeShownState
}

// beeShownState are the flags of a bee viewers were last shown.
type beeShownState struct{ angry, nectar bool }

type Bee struct{ *Ent }

func NewBee(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(BeeType, BeeConfig{}) }
func (b *Bee) SpeciesType() world.EntityType                { return BeeType }
func (b *Bee) state() *beeBehaviour                         { return b.Behaviour().(*beeBehaviour) }
func (b *Bee) Tick(tx *world.Tx, n int64)                   { b.state().effects.Tick(b, tx); b.Ent.Tick(tx, n) }
func (b *beeBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	bee := &Bee{Ent: e}
	ai := &b.ai
	ai.MovementSpeed, ai.FlyingSpeed, ai.EyeHeight = b.speed, .6, animalEyeHeight(.3, b.age < 0)
	mv := animalTick(e, tx, bee, &b.animalAI, b.registerGoals)
	if shown := (beeShownState{b.angry(), b.hasNectar}); shown != b.shown {
		b.shown = shown
		bee.updateState()
	}
	return mv
}
func (b *Bee) Health() float64        { return b.state().health.Health() }
func (b *Bee) MaxHealth() float64     { return b.state().health.MaxHealth() }
func (b *Bee) SetMaxHealth(v float64) { b.state().health.SetMaxHealth(v) }
func (b *Bee) Dead() bool             { return b.state().dead || b.Health() <= 0 }

// Hurt ports Bee.hurtServer: a hurt bee stops pollinating.
func (b *Bee) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := b.state()
	if b.Dead() || d <= 0 {
		return 0, false
	}
	if s.pollinate != nil {
		s.pollinate.pollinating = false
	}
	d, ok := s.ai.DamageCooldown(b.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	s.health.AddHealth(-d)
	s.ai.Hurt(src)
	resetLove(b)
	b.viewAction(HurtAction{})
	onMobHurt(b.tx, b)
	if b.Dead() {
		b.die(src)
	}
	return d, true
}
func (b *Bee) Heal(v float64, _ world.HealingSource) float64 {
	if b.Dead() || v <= 0 {
		return 0
	}
	old := b.Health()
	b.state().health.AddHealth(v)
	return b.Health() - old
}
func (b *Bee) KnockBack(src mgl64.Vec3, f, h float64) {
	d := b.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	b.SetVelocity(d)
}
func (b *Bee) AddEffect(e effect.Effect)  { b.state().effects.Add(e, b) }
func (b *Bee) RemoveEffect(t effect.Type) { b.state().effects.Remove(t, b) }
func (b *Bee) Effects() []effect.Effect   { return b.state().effects.Effects() }
func (b *Bee) Speed() float64             { return b.state().speed }
func (b *Bee) SetSpeed(v float64)         { b.state().speed = v }
func (b *Bee) viewAction(a world.EntityAction) {
	vs := b.tx.Viewers(b.Position())
	for _, v := range vs {
		v.ViewEntityAction(b, a)
	}
	b.tx.ReleaseViewers(vs)
}
func (b *Bee) die(src world.DamageSource) {
	s := b.state()
	if s.dead {
		return
	}
	s.dead = true
	b.viewAction(DeathAction{})
	dropAllDeathLoot(b.tx, b, src)
	_ = b.CloseIn(b.tx)
}

// Interact ports Bee.mobInteract: a wither rose or an eyeblossom fed to a bee
// gives it the effect it gives bees, and other flowers breed it or let a
// baby bee grow up faster.
func (b *Bee) Interact(_ *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	if !held.Empty() && b.breedingFood(held) {
		// FlowerBlock.getBeeInteractionEffect.
		var e effect.Effect
		switch f := held.Item().(type) {
		case block.Flower:
			if f.Type == block.WitherRose() {
				e = effect.New(effect.Wither, 1, 40*time.Second/20)
			}
		case block.Eyeblossom:
			e = effect.New(effect.Poison, 1, 25*time.Second/20)
		}
		if e.Type() != nil {
			if ctx != nil {
				ctx.SubtractFromCount(1)
			}
			b.AddEffect(e)
			return true
		}
	}
	return feedBreeder(b, u, ctx)
}

// SetTarget makes the bee attack an entity, as hives do with the player
// that took their honey.
func (b *Bee) SetTarget(target world.Entity) {
	s := b.state()
	s.ai.Mob(b.Ent, b).SetTarget(target)
}

// Target returns the entity the bee attacks, or nil.
func (b *Bee) Target() world.Entity {
	s := b.state()
	return s.ai.Mob(b.Ent, b).Target()
}

// SetStayOutOfHive keeps the bee out of hives for a number of ticks, as
// Bee.setStayOutOfHiveCountdown.
func (b *Bee) SetStayOutOfHive(ticks int) { b.state().stayOut = ticks }

// Baby reports whether the bee is a baby.
func (b *Bee) Baby() bool { return b.state().age < 0 }

// HasNectar reports whether the bee carries pollen back to its hive.
func (b *Bee) HasNectar() bool { return b.state().hasNectar }

// HasStung reports whether the bee lost its stinger.
func (b *Bee) HasStung() bool { return b.state().hasStung }

// Angry reports whether the bee is angry.
func (b *Bee) Angry() bool { return b.state().angry() }

// InLove reports whether the bee is in love.
func (b *Bee) InLove() bool { return b.state().breeding.InLove() }

func (b *Bee) breedingState() *breeding { return &b.state().breeding }
func (b *Bee) ageTicks() *int           { return &b.state().age }

// breedingFood reports whether the stack is in #bee_food, the items of the
// blocks in #bee_attractive.
func (b *Bee) breedingFood(stack item.Stack) bool { return blockItemInTag(stack, "bee_attractive") }

func (b *Bee) breedOffspring(_ *world.Tx, _ breeder) *world.EntityHandle {
	return babyAt(b).New(BeeType, BeeConfig{Age: babyStartAge})
}

var _ world.EntityType = beeType{}
var _ Living = (*Bee)(nil)
