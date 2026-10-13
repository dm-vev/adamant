package entity

import (
	"math"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewChicken creates a worldless chicken handle with the supplied variant and age.
func NewChicken(opts world.EntitySpawnOpts, conf ChickenBehaviourConfig) *world.EntityHandle {
	return opts.New(ChickenType, conf)
}

var ChickenType chickenType

type chickenType struct{}

func (chickenType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		ChickenBehaviourConfig{}.Apply(data)
	}
	return &Chicken{Ent: &Ent{tx: tx, handle: handle, data: data}}
}
func (chickenType) EncodeEntity() string        { return "minecraft:chicken" }
func (chickenType) BBox(world.Entity) cube.BBox { return cube.Box(-0.2, 0, -0.2, 0.2, 0.7, 0.2) }
func (chickenType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := ChickenBehaviourConfig{}
	if _, ok := m["Health"]; ok {
		conf.Health = float64(nbtconv.Float32(m, "Health"))
		conf.healthSet = true
	}
	if _, ok := m["MaxHealth"]; ok {
		conf.MaxHealth = float64(nbtconv.Float32(m, "MaxHealth"))
	}
	if _, ok := m["Age"]; ok {
		conf.AgeTicks = int(nbtconv.Int32(m, "Age"))
	}
	if _, ok := m["EggLayTime"]; ok {
		conf.EggLayTime = int(nbtconv.Int32(m, "EggLayTime"))
		conf.eggTimerSet = true
	}
	if _, ok := m["Variant"]; ok {
		conf.Variant = int(nbtconv.Int32(m, "Variant"))
	}
	if v, ok := m["variant"].(string); ok {
		switch strings.TrimPrefix(v, "minecraft:") {
		case "cold":
			conf.Variant = 1
		case "warm":
			conf.Variant = 2
		}
	}
	conf.Jockey = nbtconv.Bool(m, "IsChickenJockey")
	data.Data = conf.New()
}
func (chickenType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*ChickenBehaviour)
	variant := []string{"temperate", "cold", "warm"}[min(b.variant, 2)]
	return map[string]any{"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()), "Age": int32(b.ageTicks), "EggLayTime": int32(b.eggTimer), "variant": "minecraft:" + variant, "IsChickenJockey": boolByte(b.jockey)}
}

type ChickenBehaviourConfig struct {
	Health, MaxHealth             float64
	AgeTicks, Variant, EggLayTime int
	Jockey                        bool
	healthSet, eggTimerSet        bool
}

func (c ChickenBehaviourConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c ChickenBehaviourConfig) New() *ChickenBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 4
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	c.Variant = max(0, min(c.Variant, 2))
	if !c.eggTimerSet {
		c.EggLayTime = 6000 + rand.IntN(6000)
	}
	return &ChickenBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, speed: .25, ageTicks: c.AgeTicks, eggTimer: c.EggLayTime, variant: c.Variant, jockey: c.Jockey, nextWander: 1}
}

type Chicken struct{ *Ent }
type ChickenBehaviour struct {
	health                         *HealthManager
	effects                        *EffectManager
	mc                             *MovementComputer
	speed                          float64
	ageTicks, eggTimer, nextWander int
	variant                        int
	navigator                      Navigator
	dead, jockey                   bool
}

func (c *Chicken) behaviour() *ChickenBehaviour { return c.Behaviour().(*ChickenBehaviour) }
func (c *Chicken) Tick(tx *world.Tx, current int64) {
	c.behaviour().effects.Tick(c, tx)
	c.Ent.Tick(tx, current)
}
func (b *ChickenBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	b.ageTicks = min(0, b.ageTicks+1)
	if b.ageTicks == 0 && !b.jockey {
		b.eggTimer--
		if b.eggTimer <= 0 {
			b.eggTimer = 6000 + rand.IntN(6000)
			egg := item.Egg{}
			switch b.variant {
			case 1:
				egg.Variant = item.EggBlue
			case 2:
				egg.Variant = item.EggBrown
			}
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, item.NewStack(egg, 1)))
		}
	}
	if b.nextWander > 0 {
		b.nextWander--
	}
	if b.navigator.Index >= len(b.navigator.Path) && b.nextWander == 0 {
		b.chooseWanderPath(e, tx)
	}
	velocity := e.data.Vel
	rot := e.data.Rot
	if goal, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .4); ok {
		delta := goal.Sub(e.data.Pos)
		delta[1] = 0
		if delta.Len() < .25 {
			b.navigator.Index++
		} else {
			dir := delta.Normalize()
			velocity[0], velocity[2] = dir[0]*b.speed, dir[2]*b.speed
			rot[0] = math.Atan2(-dir[0], dir[2]) * 180 / math.Pi
		}
	} else {
		velocity[0], velocity[2] = 0, 0
	}
	m := b.mc.TickMovement(e, e.data.Pos, velocity, e.data.Rot, rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (b *ChickenBehaviour) chooseWanderPath(e *Ent, tx *world.Tx) {
	b.nextWander = 40 + rand.IntN(81)
	start := cube.PosFromVec3(e.Position())
	for range 4 {
		goal := start.Add(cube.Pos{rand.IntN(13) - 6, 0, rand.IntN(13) - 6})
		if b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e)) {
			return
		}
	}
	b.navigator.Path, b.navigator.Index = nil, 0
}
func (c *Chicken) Health() float64        { return c.behaviour().health.Health() }
func (c *Chicken) MaxHealth() float64     { return c.behaviour().health.MaxHealth() }
func (c *Chicken) SetMaxHealth(v float64) { c.behaviour().health.SetMaxHealth(v) }
func (c *Chicken) Dead() bool             { return c.behaviour().dead || c.Health() <= 0 }
func (c *Chicken) Hurt(damage float64, src world.DamageSource) (float64, bool) {
	if c.Dead() || damage <= 0 || (src.Fire() && c.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if res, ok := c.behaviour().effects.Effect(effect.Resistance); ok {
		damage *= effect.Resistance.Multiplier(src, res.Level())
	}
	c.behaviour().health.AddHealth(-damage)
	c.viewAction(HurtAction{})
	if c.Dead() {
		c.die()
	}
	return damage, true
}
func (c *Chicken) Heal(h float64, _ world.HealingSource) float64 {
	if c.Dead() || h <= 0 {
		return 0
	}
	old := c.Health()
	c.behaviour().health.AddHealth(h)
	return c.Health() - old
}
func (c *Chicken) KnockBack(src mgl64.Vec3, force, height float64) {
	d := c.Position().Sub(src)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	c.SetVelocity(d)
}
func (c *Chicken) AddEffect(e effect.Effect)    { c.behaviour().effects.Add(e, c) }
func (c *Chicken) RemoveEffect(t effect.Type)   { c.behaviour().effects.Remove(t, c) }
func (c *Chicken) Effects() []effect.Effect     { return c.behaviour().effects.Effects() }
func (c *Chicken) Speed() float64               { return c.behaviour().speed }
func (c *Chicken) SetSpeed(speed float64)       { c.behaviour().speed = speed }
func (c *Chicken) hasEffect(t effect.Type) bool { _, ok := c.behaviour().effects.Effect(t); return ok }
func (c *Chicken) PassengerPosition(passenger world.Entity) mgl64.Vec3 {
	offset := mgl64.Vec3{0, .7, -.1}
	if c.behaviour().ageTicks < 0 {
		offset = mgl64.Vec3{0, .375, 0}
	}
	sin, cos := math.Sincos(-c.Rotation().Yaw() * math.Pi / 180)
	offset[0], offset[2] = offset[0]*cos+offset[2]*sin, offset[2]*cos-offset[0]*sin
	return c.Position().Add(offset).Sub(vehicleAttachmentPoint(passenger))
}
func (c *Chicken) viewAction(a world.EntityAction) {
	vs := c.tx.Viewers(c.Position())
	for _, v := range vs {
		v.ViewEntityAction(c, a)
	}
	c.tx.ReleaseViewers(vs)
}
func (c *Chicken) die() {
	b := c.behaviour()
	if b.dead {
		return
	}
	b.dead = true
	c.viewAction(DeathAction{})
	tx := c.tx
	if n := rand.IntN(3); n > 0 {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, item.NewStack(item.Feather{}, n)))
	}
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, item.NewStack(item.Chicken{Cooked: c.OnFireDuration() > 0}, 1)))
	_ = c.CloseIn(tx)
}
