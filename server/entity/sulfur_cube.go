package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
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
	c := SulfurCubeBehaviourConfig{Size: int(nbtconv.Int32(m, "Size")) + 1, Age: int(nbtconv.Int32(m, "Age")), PickupTimer: int(nbtconv.Int32(m, "pickup_timer")), Fuse: int(nbtconv.Int32(m, "fuse")), FromBucket: nbtconv.Bool(m, "from_bucket") || nbtconv.Bool(m, "FromBucket")}
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
	m := map[string]any{"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()), "Size": int32(b.size - 1), "Age": int32(b.age), "pickup_timer": int32(b.pickupTimer), "fuse": int32(b.fuse)}
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
	return &SulfurCubeBehaviour{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, size: size, age: c.Age, pickupTimer: c.PickupTimer, fuse: c.Fuse, fromBucket: c.FromBucket, bodyItem: c.BodyItem, speed: .2 + .1*float64(size), jumpDelay: 10 + rand.IntN(20), nextWander: 1}
}

type SulfurCube struct{ *Ent }
type SulfurCubeBehaviour struct {
	health                                              *HealthManager
	effects                                             *EffectManager
	mc                                                  *MovementComputer
	size, age, pickupTimer, fuse, nextWander, jumpDelay int
	fromBucket, dead                                    bool
	bodyItem                                            item.Stack
	speed                                               float64
	navigator                                           Navigator
}

func (s *SulfurCube) b() *SulfurCubeBehaviour { return s.Behaviour().(*SulfurCubeBehaviour) }
func (s *SulfurCube) Tick(tx *world.Tx, current int64) {
	s.b().effects.Tick(s, tx)
	s.Ent.Tick(tx, current)
}
func (b *SulfurCubeBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	s := &SulfurCube{Ent: e}
	if b.pickupTimer > 0 {
		b.pickupTimer--
	}
	if b.fuse > 0 {
		b.fuse--
		if b.fuse == 0 {
			(block.ExplosionConfig{}).Explode(tx, world.NewEntityExplosionSource(s, 3))
			s.die(ExplosionDamageSource{Source: world.NewEntityExplosionSource(s, 3)})
		}
	}
	if b.age < 0 {
		b.age++
		if b.age == 0 {
			b.size = 2
			b.health.SetMaxHealth(8)
			b.health.AddHealth(8 - b.health.Health())
			b.speed = .4
		}
	}
	if b.nextWander > 0 {
		b.nextWander--
	}
	if b.bodyItem.Empty() && b.age >= 0 && b.pickupTimer == 0 {
		s.pickUpNearby(tx)
	}
	s.applyContactDamage(tx)
	v, rot := e.data.Vel, e.data.Rot
	if b.navigator.Index >= len(b.navigator.Path) && b.nextWander == 0 {
		s.chooseWander(tx)
	}
	if target, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), float64(b.size)*.6); ok && b.bodyItem.Empty() {
		d := target.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .25 {
			b.navigator.Index++
		} else {
			d = d.Normalize()
			v[0], v[2] = d[0]*b.speed, d[2]*b.speed
			rot[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
		}
	} else {
		v[0], v[2] = 0, 0
	}
	if b.bodyItem.Empty() && b.mc.OnGround() {
		b.jumpDelay--
		if b.jumpDelay <= 0 {
			b.jumpDelay = 10 + rand.IntN(20)
			if b.navigator.Index < len(b.navigator.Path) {
				b.jumpDelay /= 3
			}
			v[1] = .42 + float64(b.size)*.08
		}
	}
	return b.mc.TickMovement(s, e.data.Pos, v, rot, e.data.Rot, tx).apply(e)
}
func (s *SulfurCube) pickUpNearby(tx *world.Tx) {
	pos := s.Position()
	box := cube.Box(-8, -8, -8, 8, 8, 8).Translate(pos)
	var target *Ent
	var targetItem *ItemBehaviour
	bestDistance := math.MaxFloat64
	for e := range tx.EntitiesWithin(box) {
		itemEntity, ok := e.(*Ent)
		if !ok {
			continue
		}
		behaviour, ok := itemEntity.Behaviour().(*ItemBehaviour)
		if !ok || behaviour.i.Empty() || !s.swallowable(behaviour.i) || behaviour.pickupDelay > 0 {
			continue
		}
		distance := e.Position().Sub(pos).Len()
		if distance < bestDistance {
			target, targetItem, bestDistance = itemEntity, behaviour, distance
		}
	}
	if target == nil {
		return
	}
	b := s.b()
	if bestDistance <= 1.3 {
		if s.equipItem(targetItem.i) {
			targetItem.i = targetItem.i.Grow(-1)
			if targetItem.i.Empty() {
				_ = target.CloseIn(tx)
			}
		}
		return
	}
	start, goal := cube.PosFromVec3(pos), cube.PosFromVec3(target.Position())
	if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(s.Ent)) {
		b.nextWander = 20
	}
}
func (s *SulfurCube) chooseWander(tx *world.Tx) {
	b := s.b()
	b.nextWander = 40 + rand.IntN(81)
	start := cube.PosFromVec3(s.Position())
	for range 4 {
		goal := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
		if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(s.Ent)) {
			return
		}
	}
	b.navigator.Path, b.navigator.Index = nil, 0
}
func (s *SulfurCube) Health() float64        { return s.b().health.Health() }
func (s *SulfurCube) MaxHealth() float64     { return s.b().health.MaxHealth() }
func (s *SulfurCube) SetMaxHealth(v float64) { s.b().health.SetMaxHealth(v) }
func (s *SulfurCube) Dead() bool             { return s.b().dead || s.Health() <= 0 }
func (s *SulfurCube) Hurt(d float64, src world.DamageSource) (float64, bool) {
	b := s.b()
	if s.Dead() || d <= 0 || src.Fire() && s.hasEffect(effect.FireResistance) {
		return 0, false
	}
	if src.Fire() && s.canExplode() {
		s.Prime()
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	b.health.AddHealth(-d)
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
func (s *SulfurCube) KnockBack(src mgl64.Vec3, force, height float64) {
	d := s.Position().Sub(src)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	s.SetVelocity(d)
}
func (s *SulfurCube) AddEffect(e effect.Effect)    { s.b().effects.Add(e, s) }
func (s *SulfurCube) RemoveEffect(t effect.Type)   { s.b().effects.Remove(t, s) }
func (s *SulfurCube) Effects() []effect.Effect     { return s.b().effects.Effects() }
func (s *SulfurCube) Speed() float64               { return s.b().speed }
func (s *SulfurCube) SetSpeed(v float64)           { s.b().speed = v }
func (s *SulfurCube) hasEffect(t effect.Type) bool { _, ok := s.b().effects.Effect(t); return ok }
func (s *SulfurCube) viewAction(a world.EntityAction) {
	vs := s.tx.Viewers(s.Position())
	for _, v := range vs {
		v.ViewEntityAction(s, a)
	}
	s.tx.ReleaseViewers(vs)
}
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
	if b.size > 1 {
		size := b.size / 2
		for range 2 {
			child := NewSulfurCube(world.EntitySpawnOpts{Position: s.Position().Add(mgl64.Vec3{rand.Float64()*.5 - .25, .5, rand.Float64()*.5 - .25})})
			if spawned := s.tx.AddEntity(child); spawned != nil {
				if c, ok := spawned.(*SulfurCube); ok {
					c.b().size = size
					c.b().health.SetMaxHealth(float64(size * 4))
					c.b().health.AddHealth(float64(size*4) - c.Health())
					c.b().age = -24000
				}
			}
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
		b.bodyItem = decodeSulfurCubeStack(body)
	}
}
func (s *SulfurCube) BucketPickupSound() world.Sound {
	return sound.Custom{Name: "item.bucket.fill_sulfur_cube", Volume: 1, Pitch: 1}
}
func (s *SulfurCube) equipItem(stack item.Stack) bool {
	b := s.b()
	if b.age < 0 || !s.swallowable(stack) {
		return false
	}
	if !b.bodyItem.Empty() {
		if b.bodyItem.Comparable(stack) {
			return false
		}
		s.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: s.Position()}, b.bodyItem))
	}
	b.bodyItem = item.NewStack(stack.Item(), 1)
	return true
}
func (s *SulfurCube) swallowable(stack item.Stack) bool {
	if stack.Empty() {
		return false
	}
	encoded, ok := stack.Item().(interface{ EncodeItem() (string, int16) })
	if !ok {
		return false
	}
	name, _ := encoded.EncodeItem()
	switch name {
	case "minecraft:tnt", "minecraft:gunpowder", "minecraft:coal", "minecraft:charcoal", "minecraft:flint", "minecraft:fire_charge", "minecraft:flint_and_steel":
		return true
	default:
		return false
	}
}
func (s *SulfurCube) CanEquipBodyArmor(_ world.Entity) bool {
	return s.b().bodyItem.Empty() && s.b().age >= 0
}
func (s *SulfurCube) EquipBodyArmor(_ *world.Tx, _ world.Entity, stack world.Item) bool {
	return s.equipItem(item.NewStack(stack, 1))
}
func (s *SulfurCube) BodyItem() item.Stack { return s.b().bodyItem }
func (s *SulfurCube) Shear() item.Stack {
	b := s.b()
	out := b.bodyItem
	b.bodyItem = item.Stack{}
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
	tx.PlaySound(s.Position(), sound.Custom{Name: "entity.sulfur_cube.absorb", Volume: 1, Pitch: 1})
	return true
}
func (s *SulfurCube) canExplode() bool {
	name, ok := stackName(s.b().bodyItem)
	return ok && name == "minecraft:tnt" && s.b().fuse < 0 && !s.Dead()
}
func (s *SulfurCube) Prime() bool {
	if !s.canExplode() {
		return false
	}
	s.b().fuse = 80
	s.tx.PlaySound(s.Position(), sound.Custom{Name: "entity.tnt.primed", Volume: 1, Pitch: 1})
	return true
}
func (s *SulfurCube) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if user == nil {
		return false
	}
	held, _ := user.HeldItems()
	if s.b().age < 0 && sulfurCubeFood(held) {
		s.b().age = min(0, s.b().age+max(1, -s.b().age/10))
		ctx.SubtractFromCount(1)
		tx.PlaySound(s.Position(), sound.Custom{Name: "entity.small_sulfur_cube.eat", Volume: 1, Pitch: 1})
		return true
	}
	if s.b().age >= 0 {
		if name, ok := stackName(held); ok && (name == "minecraft:flint_and_steel" || name == "minecraft:fire_charge") && s.Prime() {
			if name == "minecraft:flint_and_steel" {
				ctx.DamageItem(1)
			} else {
				ctx.SubtractFromCount(1)
			}
			return true
		}
		if name, ok := stackName(held); ok && name == "minecraft:shears" && !s.b().bodyItem.Empty() {
			out := s.Shear()
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: s.Position()}, out))
			tx.PlaySound(s.Position(), sound.Custom{Name: "entity.sulfur_cube.eject", Volume: 1, Pitch: 1})
			ctx.DamageItem(1)
			return true
		}
		if s.equipItem(held) {
			ctx.SubtractFromCount(1)
			tx.PlaySound(s.Position(), sound.Custom{Name: "entity.sulfur_cube.absorb", Volume: 1, Pitch: 1})
			return true
		}
	}
	return false
}
func sulfurCubeFood(stack item.Stack) bool {
	if name, ok := stackName(stack); ok {
		switch name {
		case "minecraft:apple", "minecraft:carrot", "minecraft:potato", "minecraft:beetroot", "minecraft:wheat", "minecraft:melon_slice", "minecraft:sweet_berries", "minecraft:glow_berries":
			return true
		}
	}
	return false
}
func stackName(stack item.Stack) (string, bool) {
	if stack.Empty() {
		return "", false
	}
	encoded, ok := stack.Item().(interface{ EncodeItem() (string, int16) })
	if !ok {
		return "", false
	}
	name, _ := encoded.EncodeItem()
	return name, true
}
func (s *SulfurCube) applyContactDamage(tx *world.Tx) {
	name, ok := stackName(s.b().bodyItem)
	if !ok || name != "minecraft:magma_block" {
		return
	}
	box := SulfurCubeType.BBox(s).Translate(s.Position())
	for target := range tx.EntitiesWithin(box.Grow(.1)) {
		if target == s {
			continue
		}
		living, ok := target.(Living)
		if !ok || !box.IntersectsWith(target.H().Type().BBox(target).Translate(target.Position())) {
			continue
		}
		living.Hurt(1, AttackDamageSource{Attacker: s})
	}
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

var _ world.EntityType = sulfurCubeType{}
var _ Living = (*SulfurCube)(nil)
var _ Bucketable = (*SulfurCube)(nil)
var _ BodyArmorable = (*SulfurCube)(nil)
var _ ItemAcceptor = (*SulfurCube)(nil)
var _ Interactable = (*SulfurCube)(nil)
