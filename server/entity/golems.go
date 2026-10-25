package entity

import (
	"encoding/binary"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

const (
	ironGolemHealth = 100.0
	snowGolemHealth = 4.0
)

type golemKind uint8

const (
	ironKind golemKind = iota
	snowKind
)

type golemState struct {
	kind                         golemKind
	health                       *HealthManager
	effects                      *EffectManager
	speed                        float64
	dead, playerCreated, pumpkin bool
	ai                           MobAI
	neutralAnger
	// offerFlowerTick counts down while an iron golem holds out a poppy.
	offerFlowerTick int
}
type golemConfig struct {
	kind                              golemKind
	health                            float64
	healthSet, playerCreated, pumpkin bool
	angerTarget                       string
	angerTicks                        int
}

func (c golemConfig) Apply(d *world.EntityData) {
	h := c.health
	maxH := ironGolemHealth
	speed := .25
	if c.kind == snowKind {
		maxH = snowGolemHealth
		speed = .2
	}
	if !c.healthSet {
		h = maxH
	}
	b := &golemState{kind: c.kind, health: NewHealthManager(h, maxH), effects: NewEffectManager(), speed: speed, playerCreated: c.playerCreated, pumpkin: c.pumpkin}
	if target, err := uuid.Parse(c.angerTarget); err == nil && c.angerTicks > 0 {
		b.angerTarget, b.angerTicks = target, c.angerTicks
	}
	d.Data = b
}

func NewIronGolem(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(IronGolemType, golemConfig{kind: ironKind})
}
func NewSnowGolem(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(SnowGolemType, golemConfig{kind: snowKind, pumpkin: true})
}

var IronGolemType ironGolemType
var SnowGolemType snowGolemType

type ironGolemType struct{}
type snowGolemType struct{}

func (ironGolemType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		golemConfig{kind: ironKind}.Apply(d)
	}
	return &IronGolem{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (snowGolemType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		golemConfig{kind: snowKind, pumpkin: true}.Apply(d)
	}
	return &SnowGolem{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (ironGolemType) EncodeEntity() string        { return "minecraft:iron_golem" }
func (snowGolemType) EncodeEntity() string        { return "minecraft:snow_golem" }
func (ironGolemType) BBox(world.Entity) cube.BBox { return cube.Box(-.7, 0, -.7, .7, 2.7, .7) }
func (snowGolemType) BBox(world.Entity) cube.BBox { return cube.Box(-.35, 0, -.35, .35, 1.9, .35) }
func (ironGolemType) DecodeNBT(n map[string]any, d *world.EntityData) {
	source := ReadNativeMobJavaState(n)
	if source == nil {
		source = n
	}
	c := golemConfig{kind: ironKind}
	if _, ok := source["Health"]; ok {
		c.health = float64(nbtconv.Float32(source, "Health"))
		c.healthSet = true
	}
	if health, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health, c.healthSet = health.Current, true
	}
	c.playerCreated = nbtconv.Bool(source, "PlayerCreated")
	c.angerTarget = golemUUIDString(source["AngryAt"])
	c.angerTicks = int(nbtconv.Int32(source, "AngerTime"))
	c.Apply(d)
}
func (snowGolemType) DecodeNBT(n map[string]any, d *world.EntityData) {
	source := ReadNativeMobJavaState(n)
	if source == nil {
		source = n
	}
	c := golemConfig{kind: snowKind, pumpkin: true}
	if _, ok := source["Health"]; ok {
		c.health = float64(nbtconv.Float32(source, "Health"))
		c.healthSet = true
	}
	if health, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health, c.healthSet = health.Current, true
	}
	if _, ok := source["Pumpkin"]; ok {
		c.pumpkin = nbtconv.Bool(source, "Pumpkin")
	}
	c.Apply(d)
}
func (ironGolemType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*golemState)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{
		{Name: "minecraft:health", Base: ironGolemHealth, Current: h.Health(), DefaultMin: 0, DefaultMax: ironGolemHealth, Min: 0, Max: ironGolemHealth},
	})
	javaState := map[string]any{"Health": float32(h.Health()), "PlayerCreated": byte(boolByte(b.playerCreated)),
		"AngerTime": int32(b.angerTicks)}
	var target []int32
	if b.angerTarget != uuid.Nil {
		target = golemUUIDNBT(b.angerTarget.String())
	}
	if target != nil && b.angerTicks > 0 {
		javaState["AngryAt"] = target
	}
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Health": float32(h.Health()), "Attributes": attrs,
		"PlayerCreated": byte(boolByte(b.playerCreated)), "IsAngry": byte(boolByte(b.angerTicks > 0))})
	state := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), javaState)
	if target == nil || b.angerTicks <= 0 {
		delete(state, "AngryAt")
	}
	return MergeNativeMobJavaState(native, state)
}
func (snowGolemType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*golemState)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{
		{Name: "minecraft:health", Base: snowGolemHealth, Current: h.Health(), DefaultMin: 0, DefaultMax: snowGolemHealth, Min: 0, Max: snowGolemHealth},
	})
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Health": float32(h.Health()), "Attributes": attrs,
		"Pumpkin": byte(boolByte(b.pumpkin))})
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{"Health": float32(h.Health()), "Pumpkin": byte(boolByte(b.pumpkin))})
	return MergeNativeMobJavaState(native, javaState)
}

func golemUUIDNBT(raw string) []int32 {
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil
	}
	return []int32{int32(binary.BigEndian.Uint32(id[0:4])), int32(binary.BigEndian.Uint32(id[4:8])), int32(binary.BigEndian.Uint32(id[8:12])), int32(binary.BigEndian.Uint32(id[12:16]))}
}

func golemUUIDString(raw any) string {
	parts, ok := raw.([]int32)
	if !ok || len(parts) != 4 {
		if text, ok := raw.(string); ok {
			id, err := uuid.Parse(text)
			if err == nil {
				return id.String()
			}
		}
		return ""
	}
	var bytes [16]byte
	for i, part := range parts {
		binary.BigEndian.PutUint32(bytes[i*4:], uint32(part))
	}
	id, err := uuid.FromBytes(bytes[:])
	if err != nil {
		return ""
	}
	return id.String()
}

type IronGolem struct{ *Ent }
type SnowGolem struct{ *Ent }

func (m *IronGolem) b() *golemState             { return m.Behaviour().(*golemState) }
func (m *SnowGolem) b() *golemState             { return m.Behaviour().(*golemState) }
func (m *IronGolem) Tick(tx *world.Tx, n int64) { m.b().effects.Tick(m, tx); m.Ent.Tick(tx, n) }
func (m *SnowGolem) Tick(tx *world.Tx, n int64) { m.b().effects.Tick(m, tx); m.Ent.Tick(tx, n) }
func (b *golemState) Tick(e *Ent, tx *world.Tx) *Movement {
	ai := &b.ai
	ai.MovementSpeed = b.speed
	var self world.Entity
	if b.kind == ironKind {
		self = &IronGolem{Ent: e}
		ai.EyeHeight, ai.StepHeight = 2.295, 1
	} else {
		self = &SnowGolem{Ent: e}
		ai.EyeHeight = 1.7
	}
	mv := ai.Tick(e, self, b.registerGoals)
	if !b.dead && !e.handle.Closed() {
		b.afterAiStep(ai.Mob(e, self))
	}
	return mv
}

// OfferFlower reports whether the golem is currently offering a flower.
func (m *IronGolem) OfferFlower() bool { return m.b().offerFlowerTick > 0 }

func newGolemSnowball(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := snowballConf
	conf.Owner = owner.H()
	conf.Hit = func(projectile *Ent, tx *world.Tx, result trace.Result) {
		hit, ok := result.(trace.EntityResult)
		if !ok || hit.Entity().H().Type().EncodeEntity() != "minecraft:blaze" {
			return
		}
		attacker, _ := owner.H().Entity(tx)
		if living, ok := hit.Entity().(Living); ok {
			living.Hurt(3, ProjectileDamageSource{Projectile: projectile, Owner: attacker})
		}
	}
	return opts.New(SnowballType, conf)
}
func hurtGolem(e *Ent, b *golemState, d float64, src world.DamageSource) {
	m := golemLiving{Ent: e, b: b}
	m.Hurt(d, src)
}

type golemFireDamage struct{}

func (golemFireDamage) ReducedByArmour() bool     { return false }
func (golemFireDamage) ReducedByResistance() bool { return true }
func (golemFireDamage) Fire() bool                { return true }
func (golemFireDamage) IgnoreTotem() bool         { return false }

type golemLiving struct {
	*Ent
	b *golemState
}

func (m golemLiving) Health() float64        { return m.b.health.Health() }
func (m golemLiving) MaxHealth() float64     { return m.b.health.MaxHealth() }
func (m golemLiving) SetMaxHealth(h float64) { m.b.health.SetMaxHealth(h) }
func (m golemLiving) Dead() bool             { return m.b.dead || m.Health() <= 0 }
func (m golemLiving) Hurt(d float64, s world.DamageSource) (float64, bool) {
	if m.Dead() || d <= 0 {
		return 0, false
	}
	d, ok := m.b.ai.DamageCooldown(m.tx, d)
	if !ok {
		return 0, false
	}
	if r, ok := m.b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	m.b.health.AddHealth(-d)
	m.b.ai.Hurt(s)
	m.viewAction(HurtAction{})
	onMobHurt(m.tx, m)
	if m.Dead() {
		m.b.dead = true
		m.viewAction(DeathAction{})
		dropAllDeathLoot(m.tx, m.Ent, s)
		_ = m.CloseIn(m.tx)
	}
	return d, true
}
func (m golemLiving) Heal(h float64, _ world.HealingSource) float64 {
	if h <= 0 || m.Dead() {
		return 0
	}
	old := m.Health()
	m.b.health.AddHealth(h)
	return m.Health() - old
}
func (m golemLiving) KnockBack(src mgl64.Vec3, f, h float64) {
	knockBack(m, src, f, h)
}
func (m golemLiving) AddEffect(e effect.Effect)  { m.b.effects.Add(e, &m) }
func (m golemLiving) RemoveEffect(t effect.Type) { m.b.effects.Remove(t, &m) }
func (m golemLiving) Effects() []effect.Effect   { return m.b.effects.Effects() }
func (m golemLiving) Speed() float64             { return m.b.speed }
func (m golemLiving) SetSpeed(s float64)         { m.b.speed = s }
func (m golemLiving) viewAction(a world.EntityAction) {
	v := m.tx.Viewers(m.Position())
	for _, x := range v {
		x.ViewEntityAction(m, a)
	}
	m.tx.ReleaseViewers(v)
}
func (m *IronGolem) Health() float64        { return m.b().health.Health() }
func (m *SnowGolem) Health() float64        { return m.b().health.Health() }
func (m *IronGolem) MaxHealth() float64     { return m.b().health.MaxHealth() }
func (m *SnowGolem) MaxHealth() float64     { return m.b().health.MaxHealth() }
func (m *IronGolem) SetMaxHealth(h float64) { m.b().health.SetMaxHealth(h) }
func (m *SnowGolem) SetMaxHealth(h float64) { m.b().health.SetMaxHealth(h) }
func (m *IronGolem) Dead() bool             { return m.b().dead || m.Health() <= 0 }
func (m *SnowGolem) Dead() bool             { return m.b().dead || m.Health() <= 0 }
func (m *IronGolem) Hurt(d float64, s world.DamageSource) (float64, bool) {
	return golemLiving{m.Ent, m.b()}.Hurt(d, s)
}
func (m *SnowGolem) Hurt(d float64, s world.DamageSource) (float64, bool) {
	return golemLiving{m.Ent, m.b()}.Hurt(d, s)
}
func (m *IronGolem) Heal(h float64, s world.HealingSource) float64 {
	return golemLiving{m.Ent, m.b()}.Heal(h, s)
}
func (m *SnowGolem) Heal(h float64, s world.HealingSource) float64 {
	return golemLiving{m.Ent, m.b()}.Heal(h, s)
}

// KnockBack does nothing: iron golems resist all knockback.
func (m *IronGolem) KnockBack(mgl64.Vec3, float64, float64) {}
func (m *SnowGolem) KnockBack(p mgl64.Vec3, f, h float64) {
	golemLiving{m.Ent, m.b()}.KnockBack(p, f, h)
}
func (m *IronGolem) AddEffect(e effect.Effect)  { m.b().effects.Add(e, m) }
func (m *SnowGolem) AddEffect(e effect.Effect)  { m.b().effects.Add(e, m) }
func (m *IronGolem) RemoveEffect(t effect.Type) { m.b().effects.Remove(t, m) }
func (m *SnowGolem) RemoveEffect(t effect.Type) { m.b().effects.Remove(t, m) }
func (m *IronGolem) Effects() []effect.Effect   { return m.b().effects.Effects() }
func (m *SnowGolem) Effects() []effect.Effect   { return m.b().effects.Effects() }
func (m *IronGolem) Speed() float64             { return m.b().speed }
func (m *SnowGolem) Speed() float64             { return m.b().speed }
func (m *IronGolem) SetSpeed(v float64)         { m.b().speed = v }
func (m *SnowGolem) SetSpeed(v float64)         { m.b().speed = v }
func (m *IronGolem) PlayerCreated() bool        { return m.b().playerCreated }
func (m *IronGolem) SetPlayerCreated(v bool)    { m.b().playerCreated = v }
func (m *SnowGolem) HasPumpkin() bool           { return m.b().pumpkin }
func (m *SnowGolem) SetPumpkin(v bool)          { m.b().pumpkin = v }
func (m *IronGolem) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil {
		return false
	}
	main, _ := u.HeldItems()
	if _, ok := main.Item().(item.IronIngot); !ok || main.Empty() {
		return false
	}
	if m.Health() >= m.MaxHealth() {
		return false
	}
	m.Heal(25, nil)
	ctx.SubtractFromCount(1)
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.iron_golem.repair", Volume: 1, Pitch: 1})
	return true
}
func (m *SnowGolem) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if !m.HasPumpkin() || u == nil {
		return false
	}
	main, _ := u.HeldItems()
	if _, ok := main.Item().(item.Shears); !ok || main.Empty() {
		return false
	}
	m.SetPumpkin(false)
	ctx.DamageItem(1)
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(block.Pumpkin{Carved: true}, 1)))
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.snow_golem.shear", Volume: 1, Pitch: 1})
	return true
}
func (m *SnowGolem) RequiresCustomPersistence() bool            { return false }
func (m *SnowGolem) CanBeHitByPiercingWeapon(world.Entity) bool { return true }
func (m *IronGolem) CanBeHitByPiercingWeapon(world.Entity) bool { return true }
func (m *SnowGolem) IsSensitiveToWater() bool                   { return true }
func (m *IronGolem) IsSensitiveToWater() bool                   { return false }

// TrySpawnGolemPattern checks the Java snow and iron golem patterns with pos as
// the newly placed carved pumpkin. It clears the pattern before adding the mob.
func TrySpawnGolemPattern(tx *world.Tx, pos cube.Pos) bool {
	switch head := tx.Block(pos).(type) {
	case block.Pumpkin:
		if !head.Carved {
			return false
		}
	case block.LitPumpkin:
	default:
		return false
	}
	return forEachGolemPatternOrientation(func(right, up, _ cube.Pos) bool {
		base := pos.Sub(golemScale(up, 2))
		if ironGolemPattern(tx, pos, right, up) {
			clearIronGolemPattern(tx, pos, right, up)
			tx.AddEntity(world.EntitySpawnOpts{Position: base.Vec3Middle().Add(mgl64.Vec3{0, .05, 0})}.New(IronGolemType, golemConfig{kind: ironKind, playerCreated: true}))
			return true
		}
		snowTop := pos.Sub(up)
		bottom := pos.Sub(golemScale(up, 2))
		if _, ok := tx.Block(snowTop).(block.Snow); !ok {
			return false
		}
		if _, ok := tx.Block(bottom).(block.Snow); !ok {
			return false
		}
		tx.SetBlock(pos, block.Air{}, nil)
		tx.SetBlock(snowTop, block.Air{}, nil)
		tx.SetBlock(bottom, block.Air{}, nil)
		tx.AddEntity(NewSnowGolem(world.EntitySpawnOpts{Position: bottom.Vec3Middle().Add(mgl64.Vec3{0, .05, 0})}))
		return true
	})
}

func forEachGolemPatternOrientation(visit func(right, up, forward cube.Pos) bool) bool {
	for _, forwardFace := range cube.Faces() {
		forward := golemFaceOffset(forwardFace)
		for _, upFace := range cube.Faces() {
			if upFace == forwardFace || upFace == forwardFace.Opposite() {
				continue
			}
			up := golemFaceOffset(upFace)
			if visit(golemCross(forward, up), up, forward) {
				return true
			}
		}
	}
	return false
}

func golemFaceOffset(face cube.Face) cube.Pos {
	switch face {
	case cube.FaceDown:
		return cube.Pos{0, -1, 0}
	case cube.FaceUp:
		return cube.Pos{0, 1, 0}
	case cube.FaceNorth:
		return cube.Pos{0, 0, -1}
	case cube.FaceSouth:
		return cube.Pos{0, 0, 1}
	case cube.FaceWest:
		return cube.Pos{-1, 0, 0}
	default:
		return cube.Pos{1, 0, 0}
	}
}

func golemCross(a, b cube.Pos) cube.Pos {
	return cube.Pos{a[1]*b[2] - a[2]*b[1], a[2]*b[0] - a[0]*b[2], a[0]*b[1] - a[1]*b[0]}
}

func golemScale(pos cube.Pos, n int) cube.Pos { return cube.Pos{pos[0] * n, pos[1] * n, pos[2] * n} }

func ironGolemPattern(tx *world.Tx, head, right, up cube.Pos) bool {
	upper := head.Sub(up)
	base := head.Sub(golemScale(up, 2))
	_, center := tx.Block(upper).(block.Iron)
	_, bottom := tx.Block(base).(block.Iron)
	_, left := tx.Block(upper.Sub(right)).(block.Iron)
	_, rightIron := tx.Block(upper.Add(right)).(block.Iron)
	_, headLeftAir := tx.Block(head.Sub(right)).(block.Air)
	_, headRightAir := tx.Block(head.Add(right)).(block.Air)
	_, baseLeftAir := tx.Block(base.Sub(right)).(block.Air)
	_, baseRightAir := tx.Block(base.Add(right)).(block.Air)
	return center && bottom && left && rightIron && headLeftAir && headRightAir && baseLeftAir && baseRightAir
}

func clearIronGolemPattern(tx *world.Tx, head, right, up cube.Pos) {
	upper := head.Sub(up)
	base := head.Sub(golemScale(up, 2))
	for _, p := range [...]cube.Pos{head, upper, base, upper.Sub(right), upper.Add(right)} {
		tx.SetBlock(p, block.Air{}, nil)
	}
}

var _ Living = (*IronGolem)(nil)
var _ Living = (*SnowGolem)(nil)
var _ world.EntityType = ironGolemType{}
var _ world.EntityType = snowGolemType{}
