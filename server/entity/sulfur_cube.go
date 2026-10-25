package entity

import (
	"math"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

const (
	sulfurCubeMaxSize     = 2
	sulfurCubePickupDelay = 100
)

// NewSulfurCube creates a worldless SulfurCube entity handle.
func NewSulfurCube(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(SulfurCubeType, SulfurCubeBehaviourConfig{Size: 2})
}

// SulfurCubeType is the Java sulfur cube entity type.
var SulfurCubeType sulfurCubeType

type sulfurCubeType struct{}

func (sulfurCubeType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		SulfurCubeBehaviourConfig{}.Apply(data)
	}
	return &SulfurCube{Ent: &Ent{tx: tx, handle: handle, data: data}}
}
func (sulfurCubeType) EncodeEntity() string { return "minecraft:sulfur_cube" }
func (sulfurCubeType) BBox(e world.Entity) cube.BBox {
	size := 2
	if c, ok := e.(*SulfurCube); ok {
		size = c.b().size
	}
	width := float64(size) * .245
	return cube.Box(-width, 0, -width, width, float64(size)*.49, width)
}
func (sulfurCubeType) DecodeNBT(m map[string]any, data *world.EntityData) {
	size, _ := nbtconv.Integer(m, "Size")
	c := SulfurCubeBehaviourConfig{Size: int(size) + 1, Age: int(nbtconv.Int32(m, "Age")), PickupTimer: int(nbtconv.Int32(m, "pickup_timer")), Fuse: -1, FromBucket: nbtconv.Bool(m, "from_bucket") || nbtconv.Bool(m, "FromBucket")}
	// Java saves -1 for a cube nobody primed, and earlier versions saved 0.
	if fuse := int(nbtconv.Int32(m, "fuse")); fuse > 0 {
		c.Fuse = fuse
	}
	if c.Size < 1 {
		c.Size = 1
	}
	if _, ok := m["Health"]; ok {
		c.Health = float64(nbtconv.Float32(m, "Health"))
		c.healthSet = true
	}
	if _, ok := m["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(m, "MaxHealth"))
	}
	if body, ok := m["BodyItem"].(map[string]any); ok {
		c.BodyItem = decodeSulfurCubeStack(body)
	}
	data.Data = c.New()
}
func (sulfurCubeType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*SulfurCubeBehaviour)
	m := map[string]any{"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()), "Size": uint8(b.size - 1), "Age": int32(b.age), "pickup_timer": int32(b.pickupTimer), "fuse": int32(b.fuse)}
	if b.fromBucket {
		m["from_bucket"] = byte(1)
	}
	if !b.bodyItem.Empty() {
		m["BodyItem"] = encodeSulfurCubeStack(b.bodyItem)
	}
	return m
}

// SulfurCubeBehaviourConfig configures native SulfurCube state.
type SulfurCubeBehaviourConfig struct {
	Health, MaxHealth            float64
	Age, Size, PickupTimer, Fuse int
	FromBucket                   bool
	BodyItem                     item.Stack
	healthSet                    bool
}

func (c SulfurCubeBehaviourConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c SulfurCubeBehaviourConfig) New() *SulfurCubeBehaviour {
	size := c.Size
	if size <= 0 {
		size = 1
	}
	size = min(size, sulfurCubeMaxSize)
	maxHealth := c.MaxHealth
	if maxHealth <= 0 {
		maxHealth = float64(size * 4)
	}
	health := c.Health
	if !c.healthSet && health <= 0 {
		health = maxHealth
	}
	fuse := c.Fuse
	if fuse <= 0 {
		// A cube nobody primed has no fuse.
		fuse = -1
	}
	b := &SulfurCubeBehaviour{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), size: size, age: c.Age, pickupTimer: c.PickupTimer, fuse: fuse, fromBucket: c.FromBucket, bodyItem: c.BodyItem}
	b.archetype = sulfurCubeArchetypeOf(b.bodyItem)
	return b
}

// SulfurCube is a cube of sulfur slime that swallows a block, which gives it
// the bounce, slide and weight of the archetype the block belongs to.
type SulfurCube struct{ *Ent }

// SulfurCubeBehaviour is the state of a sulfur cube.
type SulfurCubeBehaviour struct {
	health  *HealthManager
	effects *EffectManager
	ai      MobAI
	cubeAI
	size, age, pickupTimer, pushSoundCooldown, fuse int
	fromBucket, dead                                bool
	bodyItem                                        item.Stack
	archetype                                       *sulfurCubeArchetype
	tempt                                           *TemptGoal
	// goalsRegistered reports if the cube has goals, which it drops while
	// it holds a block.
	goalsRegistered bool
	// hitBy is the entity whose attack of hitDamage the cube took this
	// tick, which SulfurCube.knockback turns it away from.
	hitBy     world.Entity
	hitDamage float64
}

func (s *SulfurCube) b() *SulfurCubeBehaviour { return s.Behaviour().(*SulfurCubeBehaviour) }
func (s *SulfurCube) Tick(tx *world.Tx, current int64) {
	s.b().effects.Tick(s, tx)
	s.Ent.Tick(tx, current)
}

func (s *SulfurCube) Health() float64        { return s.b().health.Health() }
func (s *SulfurCube) MaxHealth() float64     { return s.b().health.MaxHealth() }
func (s *SulfurCube) SetMaxHealth(v float64) { s.b().health.SetMaxHealth(v) }
func (s *SulfurCube) Dead() bool             { return s.b().dead || s.Health() <= 0 }

// Archetype returns the name of the archetype of the block the cube holds,
// none if it holds none.
func (s *SulfurCube) Archetype() string {
	if a := s.b().archetype; a != nil {
		return a.name
	}
	return "none"
}

// Primed reports if the fuse of the cube burns.
func (s *SulfurCube) Primed() bool { return s.b().fuse >= 0 }

// sulfurCubeImmuneTo reports if a cube holding a block shrugs off the damage,
// as #sulfur_cube_with_block_immune_to lists: blows, projectiles, falls,
// explosions and the like only push it.
func sulfurCubeImmuneTo(src world.DamageSource) bool {
	switch src.(type) {
	case AttackDamageSource, SpearDamageSource, MaceSmashDamageSource, ProjectileDamageSource, WindChargeDamageSource,
		FallDamageSource, ExplosionDamageSource, FreezeDamageSource, DryOutDamageSource, block.MagmaDamageSource,
		block.StalagmiteDamageSource, block.DamageSource, sulfurCubeHotDamageSource:
		return true
	}
	return false
}

// Hurt ports SulfurCube.hurtServer: fire or a burning arrow primes a cube
// holding TNT, and an explosion primes it with a short fuse. A cube holding
// a block takes no damage from what it is immune to, only the push.
func (s *SulfurCube) Hurt(d float64, src world.DamageSource) (float64, bool) {
	b := s.b()
	if s.Dead() || d <= 0 || src.Fire() && s.hasEffect(effect.FireResistance) || b.fuse >= 0 && b.archetype != nil && b.archetype.explosion != nil {
		// A primed cube is invulnerable, as setPermanentlyInvulnerable.
		return 0, false
	}
	if !b.bodyItem.Empty() {
		if s.canExplode() {
			_, explosion := src.(ExplosionDamageSource)
			burningArrow := false
			if p, ok := src.(ProjectileDamageSource); ok && p.Projectile != nil {
				if f, ok := p.Projectile.(interface{ OnFireDuration() time.Duration }); ok {
					burningArrow = f.OnFireDuration() > 0
				}
			}
			if src.Fire() || burningArrow {
				s.primeTime(false)
			} else if explosion {
				s.primeTime(true)
			}
		}
		if sulfurCubeImmuneTo(src) {
			if a, ok := src.(AttackDamageSource); ok {
				b.hitBy, b.hitDamage = a.Attacker, d
			}
			return 0, true
		}
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
	if a, ok := src.(AttackDamageSource); ok {
		b.hitBy, b.hitDamage = a.Attacker, d
	}
	s.viewAction(HurtAction{})
	onMobHurt(s.tx, s)
	if s.Dead() {
		s.die(src)
	}
	return d, true
}
func (s *SulfurCube) Heal(h float64, _ world.HealingSource) float64 {
	if s.Dead() || h <= 0 {
		return 0
	}
	old := s.Health()
	s.b().health.AddHealth(h)
	return s.Health() - old
}
func (s *SulfurCube) AddEffect(e effect.Effect)    { s.b().effects.Add(e, s) }
func (s *SulfurCube) RemoveEffect(t effect.Type)   { s.b().effects.Remove(t, s) }
func (s *SulfurCube) Effects() []effect.Effect     { return s.b().effects.Effects() }
func (s *SulfurCube) Speed() float64               { return s.b().ai.MovementSpeed }
func (s *SulfurCube) Size() int                    { return s.b().size }
func (s *SulfurCube) SetSpeed(v float64)           { s.b().ai.MovementSpeed = v }
func (s *SulfurCube) hasEffect(t effect.Type) bool { _, ok := s.b().effects.Effect(t); return ok }
func (s *SulfurCube) viewAction(a world.EntityAction) {
	vs := s.tx.Viewers(s.Position())
	for _, v := range vs {
		v.ViewEntityAction(s, a)
	}
	s.tx.ReleaseViewers(vs)
}

// viewBody shows viewers the block the cube holds and its archetype.
func (s *SulfurCube) viewBody() {
	vs := s.tx.Viewers(s.Position())
	for _, v := range vs {
		v.ViewEntityArmour(s)
	}
	s.tx.ReleaseViewers(vs)
	s.updateState()
}

// die kills the cube: it drops its loot and the block it held, and a cube
// that was not primed splits into two small cubes, as AbstractCubeMob.remove
// with SulfurCube.getSplitCount.
func (s *SulfurCube) die(src world.DamageSource) {
	b := s.b()
	if b.dead {
		return
	}
	b.dead = true
	s.viewAction(DeathAction{})
	dropAllDeathLoot(s.tx, s, src)
	if !b.bodyItem.Empty() {
		s.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: s.Position()}, b.bodyItem))
	}
	if b.size > 1 && b.fuse < 0 {
		m := b.ai.Mob(s.Ent, s)
		offset := m.Width() / 2
		r := m.Random()
		for i := range 2 {
			xd := (float64(i%2) - .5) * offset
			zd := (float64(i/2) - .5) * offset
			// SulfurCube.setUpSplitCube makes the halves babies.
			opts := world.EntitySpawnOpts{Position: s.Position().Add(mgl64.Vec3{xd, .5, zd}), Rotation: cube.Rotation{float64(r.NextFloat()) * 360, 0}, SpawnReason: "triggered", SpawnFinalized: true}
			s.tx.AddEntity(opts.New(SulfurCubeType, SulfurCubeBehaviourConfig{Size: b.size / 2, Age: babyStartAge}))
		}
	}
	_ = s.CloseIn(s.tx)
}
func (s *SulfurCube) FromBucket() bool                                { return s.b().fromBucket }
func (s *SulfurCube) SetFromBucket(v bool)                            { s.b().fromBucket = v }
func (s *SulfurCube) CanBePickedUpWithBucket(bucket item.Bucket) bool { return bucket.Empty() }
func (s *SulfurCube) BucketItem() item.Stack {
	return item.NewStack(item.MobBucket{Entity: "minecraft:sulfur_cube"}, 1)
}
func (s *SulfurCube) SaveToBucket(stack item.Stack) item.Stack {
	b := s.b()
	data := map[string]any{"Health": float32(b.health.Health()), "Size": int32(b.size - 1), "Age": int32(b.age), "FromBucket": byte(1)}
	if !b.bodyItem.Empty() {
		data["BodyItem"] = encodeSulfurCubeStack(b.bodyItem)
	}
	bucket, ok := stack.Item().(item.MobBucket)
	if !ok {
		return item.Stack{}
	}
	bucket.Data = data
	return item.NewStack(bucket, 1)
}
func (s *SulfurCube) LoadFromBucket(stack item.Stack) {
	bucket, ok := stack.Item().(item.MobBucket)
	if !ok {
		return
	}
	data, b := bucket.Data, s.b()
	b.fromBucket = true
	b.age = int(nbtconv.Int32(data, "Age"))
	b.size = min(2, max(1, int(nbtconv.Int32(data, "Size"))+1))
	b.health.SetMaxHealth(float64(b.size * 4))
	if _, ok := data["Health"]; ok {
		b.health.AddHealth(float64(nbtconv.Float32(data, "Health")) - b.health.Health())
	}
	if body, ok := data["BodyItem"].(map[string]any); ok {
		s.setBodyItem(decodeSulfurCubeStack(body))
	}
}
func (s *SulfurCube) BucketPickupSound() world.Sound {
	return sound.Custom{Name: "item.bucket.fill_sulfur_cube", Volume: 1, Pitch: 1}
}

// setBodyItem changes the block the cube holds, and with it its archetype,
// as SulfurCube.collectEquipmentChanges.
func (s *SulfurCube) setBodyItem(stack item.Stack) {
	b := s.b()
	b.bodyItem = stack
	b.archetype = sulfurCubeArchetypeOf(stack)
	if s.tx != nil {
		s.viewBody()
	}
}

// equipItem ports SulfurCube.equipItem: an adult cube swallows a block,
// spitting out the one it held unless it is the same.
func (s *SulfurCube) equipItem(stack item.Stack) bool {
	b := s.b()
	if b.age < 0 || !sulfurCubeSwallowable(stack) {
		return false
	}
	if !b.bodyItem.Empty() {
		if b.bodyItem.Comparable(stack) {
			return false
		}
		s.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: s.Position().Add(mgl64.Vec3{0, s.H().Type().BBox(s).Height()})}, b.bodyItem))
	}
	s.setBodyItem(item.NewStack(stack.Item(), 1))
	s.tx.PlaySound(s.Position(), sound.Custom{Name: "entity.sulfur_cube.absorb", Volume: 1, Pitch: 1})
	return true
}
func (s *SulfurCube) CanEquipBodyArmor(_ world.Entity) bool {
	return s.b().bodyItem.Empty() && s.b().age >= 0
}
func (s *SulfurCube) EquipBodyArmor(_ *world.Tx, _ world.Entity, stack world.Item) bool {
	return s.equipItem(item.NewStack(stack, 1))
}
func (s *SulfurCube) BodyItem() item.Stack { return s.b().bodyItem }

// Shear ports SulfurCube.shear: the cube spits out its block and waits five
// seconds before it swallows another.
func (s *SulfurCube) Shear() item.Stack {
	b := s.b()
	out := b.bodyItem
	s.setBodyItem(item.Stack{})
	b.pickupTimer = sulfurCubePickupDelay
	return out
}
func (s *SulfurCube) AcceptItem(from world.Entity, tx *world.Tx, ctx *item.UseContext) bool {
	carrier, ok := from.(interface {
		HeldItems() (item.Stack, item.Stack)
	})
	if !ok {
		return false
	}
	held, _ := carrier.HeldItems()
	if !s.equipItem(held) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}

// canExplode ports SulfurCube.canExplode: an unprimed living cube holding a
// block of the explosive archetype may blow up.
func (s *SulfurCube) canExplode() bool {
	b := s.b()
	return b.archetype != nil && b.archetype.explosion != nil && !s.Dead() && b.fuse < 0
}

// Prime lights the fuse of a cube holding TNT.
func (s *SulfurCube) Prime() bool { return s.primeTime(false) }

// primeTime ports SulfurCube.primeTime: the fuse of a cube holding TNT
// burns six seconds, or a random short time when an explosion lit it.
func (s *SulfurCube) primeTime(imminent bool) bool {
	b := s.b()
	if !s.canExplode() || !gameRuleTNTExplodes(s.tx) {
		return false
	}
	fuse := b.archetype.explosion.fuse
	if imminent {
		// PrimedTnt.getRandomShortFuse.
		fuse = int(b.ai.Mob(s.Ent, s).Random().NextInt(int32(fuse/4))) + fuse/8
	}
	b.fuse = fuse
	r := b.ai.Mob(s.Ent, s).Random()
	s.tx.PlaySound(s.Position(), sound.Custom{Name: "entity.tnt.primed", Volume: 1, Pitch: float64((r.NextFloat()-r.NextFloat())*.2 + 1)})
	s.tx.EmitGameEvent(world.GameEventPrimeFuse, s.Position(), world.GameEventContext{Source: s})
	s.updateState()
	return true
}

// gameRuleTNTExplodes reports the tnt_explodes game rule.
func gameRuleTNTExplodes(tx *world.Tx) bool {
	v, ok := tx.World().GameRule("tnt_explodes")
	b, isBool := v.(bool)
	return !ok || !isBool || b
}

// Interact ports SulfurCube.mobInteract: slime balls feed a baby cube, flint
// and steel or a fire charge primes one holding TNT, shears make it spit out
// its block, and a block it can swallow is swallowed.
func (s *SulfurCube) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if user == nil {
		return false
	}
	b := s.b()
	held, _ := user.HeldItems()
	if b.age < 0 {
		if sulfurCubeFood(held) {
			// AgeableMob.ageUp with getSpeedUpSecondsWhenFeeding.
			b.age = min(0, b.age+int(float64(-b.age/20)*.1)*20)
			ctx.SubtractFromCount(1)
			tx.PlaySound(s.Position(), sound.Custom{Name: "entity.small_sulfur_cube.eat", Volume: 1, Pitch: 1})
			return true
		}
		return false
	}
	if b.fuse >= 0 {
		return false
	}
	name, _ := stackName(held)
	if s.canExplode() && (name == "minecraft:flint_and_steel" || name == "minecraft:fire_charge") {
		if !s.primeTime(false) {
			return false
		}
		if name == "minecraft:flint_and_steel" {
			ctx.DamageItem(1)
		} else {
			ctx.SubtractFromCount(1)
		}
		return true
	}
	if name == "minecraft:shears" && !b.bodyItem.Empty() {
		out := s.Shear()
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: s.Position().Add(mgl64.Vec3{0, s.H().Type().BBox(s).Height()})}, out))
		tx.PlaySound(s.Position(), sound.Custom{Name: "entity.sulfur_cube.eject", Volume: 1, Pitch: 1})
		ctx.DamageItem(1)
		return true
	}
	if s.equipItem(held) {
		ctx.SubtractFromCount(1)
		return true
	}
	return false
}

// sulfurCubeFood reports if the stack is in #sulfur_cube_food: slime balls.
func sulfurCubeFood(stack item.Stack) bool { return inItemTag(stack, "sulfur_cube_food") }

// sulfurCubeSwallowable reports if the stack is in #sulfur_cube_swallowable,
// the blocks of every archetype.
func sulfurCubeSwallowable(stack item.Stack) bool {
	return inItemTag(stack, "sulfur_cube_swallowable")
}

func stackName(stack item.Stack) (string, bool) {
	if stack.Empty() {
		return "", false
	}
	name, _ := stack.Item().EncodeItem()
	return name, true
}

// itemTagSets caches the Bedrock items of the Java item tags looked up.
var itemTagSets struct {
	sync.Mutex
	sets map[string]map[itemKey]struct{}
}

// itemKey is the name and metadata an item encodes as.
type itemKey struct {
	name string
	meta int16
}

// inItemTag reports if the item of the stack is one of the Java item tag
// passed.
func inItemTag(stack item.Stack, tag string) bool {
	if stack.Empty() {
		return false
	}
	itemTagSets.Lock()
	set, ok := itemTagSets.sets[tag]
	if !ok {
		set = make(map[itemKey]struct{})
		for _, id := range loot.ItemTagMembers(tag) {
			if it, ok := item.JavaItem(id); ok {
				name, meta := it.EncodeItem()
				set[itemKey{name, meta}] = struct{}{}
			}
		}
		if itemTagSets.sets == nil {
			itemTagSets.sets = make(map[string]map[itemKey]struct{})
		}
		itemTagSets.sets[tag] = set
	}
	itemTagSets.Unlock()
	name, meta := stack.Item().EncodeItem()
	_, ok = set[itemKey{name, meta}]
	return ok
}

func (s *SulfurCube) RequiresCustomPersistence() bool {
	return s.b().fromBucket || !s.b().bodyItem.Empty()
}
func (s *SulfurCube) CanBePickedFromInside() bool { return s.b().bodyItem.Empty() }
func encodeSulfurCubeStack(stack item.Stack) map[string]any {
	if stack.Empty() {
		return nil
	}
	return item.WriteNBT(stack, true)
}
func decodeSulfurCubeStack(data map[string]any) item.Stack { return item.ReadNBT(data, nil) }

// sulfurCubeKnockback is the knockback a cube holding a block takes, sideways and
// up, as SulfurCube.knockback works it out.
func sulfurCubeKnockback(a *sulfurCubeArchetype, attacker world.Entity, self *SulfurCube, xd, zd, damage float64) mgl64.Vec3 {
	horizontal, vertical := a.knockbackH, a.knockbackV
	box := self.H().Type().BBox(self).Translate(self.Position())
	centre := box.Min().Add(box.Max()).Mul(.5)
	eye := EyePosition(attacker)
	look := attacker.Rotation().Vec3().Normalize()
	// applyHorizontalHitAngleScale: a glancing blow turns the push further
	// sideways.
	toTarget := centre.Sub(eye).Normalize()
	angleDiff := math.Atan2(look[0]*toTarget[2]-look[2]*toTarget[0], look[0]*toTarget[0]+look[2]*toTarget[2])
	sin, cos := math.Sincos(angleDiff * 1.6)
	xd, zd = xd*cos-zd*sin, xd*sin+zd*cos
	// applyVerticalHitAnglePowerTransfer: hitting the cube low lifts it,
	// hitting it high flattens the push.
	half := box.Height() / 2
	top := centre.Add(mgl64.Vec3{0, half}).Sub(eye).Normalize()
	bottom := centre.Sub(mgl64.Vec3{0, half}).Sub(eye).Normalize()
	factor := clampedMap(look[1], top[1], bottom[1], -1, 1)
	ratio := math.Abs(factor * .5)
	if factor < 0 {
		ratio = -ratio
	}
	horizontal, vertical = horizontal*(1-ratio), vertical*(1+ratio)
	// applyVerticalPositionAnglePowerRotation: an attacker above the cube
	// pushes it down, one below it up.
	feet := self.Position().Sub(attacker.Position())
	angle := math.Atan2(-feet[1], math.Hypot(feet[0], feet[2]))
	sin, cos = math.Sincos(-angle * .8)
	rh, rv := horizontal*cos-vertical*sin, horizontal*sin+vertical*cos
	hRatio, vRatio := 0.0, 0.0
	if a.knockbackH > 0 {
		hRatio = math.Abs(rh) / a.knockbackH
	}
	if a.knockbackV > 0 {
		vRatio = math.Abs(rv) / a.knockbackV
	}
	if maxRatio := math.Max(hRatio, vRatio); maxRatio > 1 {
		rh, rv = rh/maxRatio, rv/maxRatio
	}
	multiplier := math.Sqrt(damage)
	resistance := a.knockbackResistance()
	horizontal = rh * multiplier * (1 - resistance) * .4
	vertical = rv * multiplier * (1 - resistance)
	horizontal, vertical = math.Max(-128, math.Min(128, horizontal)), math.Max(-128, math.Min(128, vertical))
	push := mgl64.Vec3{xd, 0, zd}
	if push.Len() > 1e-4 {
		push = push.Normalize().Mul(horizontal)
	} else {
		push = mgl64.Vec3{}
	}
	return mgl64.Vec3{-push[0], vertical * 1.2, -push[2]}
}

// clampedMap ports Mth.clampedMap.
func clampedMap(v, fromMin, fromMax, toMin, toMax float64) float64 {
	t := (v - fromMin) / (fromMax - fromMin)
	t = math.Max(0, math.Min(1, t))
	return toMin + t*(toMax-toMin)
}

// KnockBack ports SulfurCube.knockback: a cube holding a block that is hit
// flies off as its archetype and the angle of the blow decide, and makes
// the sound of its archetype. Otherwise it takes knockback as any mob,
// less its knockback resistance.
func (s *SulfurCube) KnockBack(src mgl64.Vec3, force, height float64) {
	b := s.b()
	attacker := b.hitBy
	b.hitBy = nil
	resistance := 0.0
	if b.archetype != nil {
		resistance = b.archetype.knockbackResistance()
	}
	d := s.Position().Sub(src)
	d[1] = 0
	if b.archetype != nil && attacker != nil {
		v := s.Velocity()
		push := sulfurCubeKnockback(b.archetype, attacker, s, -d[0], -d[2], b.hitDamage)
		s.SetVelocity(mgl64.Vec3{v[0] + push[0], v[1] + push[1], v[2] + push[2]})
		s.tx.PlaySound(s.Position(), sound.Custom{Name: b.archetype.hitSound(), Volume: 1, Pitch: 1})
		return
	}
	knockBackResisted(s, src, force, height, resistance)
}

var _ world.EntityType = sulfurCubeType{}
var _ Living = (*SulfurCube)(nil)
var _ Bucketable = (*SulfurCube)(nil)
var _ BodyArmorable = (*SulfurCube)(nil)
var _ ItemAcceptor = (*SulfurCube)(nil)
var _ Interactable = (*SulfurCube)(nil)
