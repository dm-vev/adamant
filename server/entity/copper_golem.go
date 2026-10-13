package entity

import (
	"hash/fnv"
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// CopperGolemType is the Java copper golem entity type.
var CopperGolemType copperGolemType

type copperGolemType struct{}

func (copperGolemType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		(CopperGolemBehaviourConfig{NextWeatheringTick: -1}).Apply(d)
	}
	golem := &CopperGolem{Ent: &Ent{tx: tx, handle: h, data: d}}
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(h.UUID().String()))
	b := golem.b()
	b.random = java26.NewLegacyRandom(int64(hash.Sum64()))
	b.cooldown = 60 + b.randomInt(41)
	return golem
}
func (copperGolemType) EncodeEntity() string        { return "minecraft:copper_golem" }
func (copperGolemType) BBox(world.Entity) cube.BBox { return cube.Box(-.35, 0, -.35, .35, .9, .35) }
func (copperGolemType) DecodeNBT(m map[string]any, d *world.EntityData) {
	nextWeatheringTick := int64(-1)
	if value, ok := m["next_weather_age"].(int64); ok {
		nextWeatheringTick = value
	}
	c := CopperGolemBehaviourConfig{WeatherState: copperWeatherFromName(nbtconv.String(m, "weather_state")), State: copperStateFromName(nbtconv.String(m, "State")), NextWeatheringTick: nextWeatheringTick}
	c.Waxed = nextWeatheringTick == -2
	if antenna, ok := m["Antenna"].(map[string]any); ok {
		c.Antenna = item.ReadNBT(antenna, nil)
	}
	if held, ok := m["CarriedItem"].(map[string]any); ok {
		c.Carried = item.ReadNBT(held, nil)
	}
	if _, ok := m["Health"]; ok {
		c.Health = float64(nbtconv.Float32(m, "Health"))
		c.healthSet = true
	}
	if _, ok := m["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(m, "MaxHealth"))
	}
	d.Data = c.New()
}
func (copperGolemType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*CopperGolemBehaviour)
	m := map[string]any{"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()), "weather_state": copperWeatherNames[b.weather], "State": copperStates[b.state], "Waxed": boolByte(b.waxed), "next_weather_age": b.nextWeatheringTick}
	if !b.antenna.Empty() {
		m["Antenna"] = item.WriteNBT(b.antenna, true)
	}
	if !b.carried.Empty() {
		m["CarriedItem"] = item.WriteNBT(b.carried, true)
	}
	return m
}

// CopperGolemState is the current container-interaction animation state.
type CopperGolemState uint8

const (
	CopperGolemIdle CopperGolemState = iota
	CopperGolemGettingItem
	CopperGolemGettingNoItem
	CopperGolemDroppingItem
	CopperGolemDroppingNoItem
)

var copperWeatherNames = [...]string{"unaffected", "exposed", "weathered", "oxidized"}
var copperStates = [...]string{"idle", "getting_item", "getting_no_item", "dropping_item", "dropping_no_item"}

func copperWeatherFromName(s string) int {
	for i, n := range copperWeatherNames {
		if n == s {
			return i
		}
	}
	return 0
}
func copperStateFromName(s string) int {
	for i, n := range copperStates {
		if n == s {
			return i
		}
	}
	return 0
}

type CopperGolemBehaviourConfig struct {
	Health, MaxHealth   float64
	WeatherState, State int
	Waxed               bool
	NextWeatheringTick  int64
	Antenna             item.Stack
	Carried             item.Stack
	healthSet           bool
}

func (c CopperGolemBehaviourConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c CopperGolemBehaviourConfig) New() *CopperGolemBehaviour {
	maxHealth := c.MaxHealth
	if maxHealth <= 0 {
		maxHealth = 12
	}
	health := c.Health
	if !c.healthSet && health <= 0 {
		health = maxHealth
	}
	return &CopperGolemBehaviour{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, speed: .2, weather: max(0, min(c.WeatherState, 3)), state: max(0, min(c.State, 4)), waxed: c.Waxed, nextWeatheringTick: c.NextWeatheringTick, antenna: c.Antenna, carried: c.Carried, cooldown: 60, nextWander: 1}
}

type CopperGolemBehaviour struct {
	health               *HealthManager
	effects              *EffectManager
	mc                   *MovementComputer
	speed                float64
	random               java26.Random
	weather, state       int
	waxed, dead          bool
	nextWeatheringTick   int64
	antenna              item.Stack
	carried              item.Stack
	lastLightningBolt    string
	cooldown, nextWander int
	navigator            Navigator
	target               cube.Pos
	targetSet            bool
	interactTicks        int
}

// CopperGolem is a mobile item-sorting golem that oxidises over time.
type CopperGolem struct{ *Ent }

func NewCopperGolem(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(CopperGolemType, CopperGolemBehaviourConfig{NextWeatheringTick: -1})
}

// TrySpawnCopperGolemPattern completes the copper golem's two-block pattern in
// every orientation supported by Java's block-pattern matcher.
func TrySpawnCopperGolemPattern(tx *world.Tx, pumpkinPos cube.Pos) bool {
	switch head := tx.Block(pumpkinPos).(type) {
	case block.Pumpkin:
		if !head.Carved {
			return false
		}
	case block.LitPumpkin:
	default:
		return false
	}
	var copperPos cube.Pos
	if !forEachGolemPatternOrientation(func(_, up, _ cube.Pos) bool {
		candidate := pumpkinPos.Sub(up)
		if _, ok := tx.Block(candidate).(block.Copper); !ok {
			return false
		}
		copperPos = candidate
		return true
	}) {
		return false
	}
	tx.SetBlock(pumpkinPos, block.Air{}, nil)
	tx.SetBlock(copperPos, block.Air{}, nil)
	tx.AddEntity(NewCopperGolem(world.EntitySpawnOpts{Position: copperPos.Vec3Centre()}))
	return true
}

func (*CopperGolem) SpeciesType() world.EntityType { return CopperGolemType }
func (c *CopperGolem) b() *CopperGolemBehaviour    { return c.Behaviour().(*CopperGolemBehaviour) }

// FinalizeSpawn plays the one-time creation sound.
func (c *CopperGolem) FinalizeSpawn(_ string, _ *java26.Random) {
	c.tx.PlaySound(c.Position(), sound.Custom{Name: "entity.copper_golem.spawn", Volume: 1, Pitch: 1})
}

// State returns the golem's current item-transfer state.
func (c *CopperGolem) State() CopperGolemState { return CopperGolemState(c.b().state) }

// WeatherState returns its oxidation stage, from unaffected through oxidized.
func (c *CopperGolem) WeatherState() int { return c.b().weather }

// Waxed reports whether honeycomb has stopped further oxidation.
func (c *CopperGolem) Waxed() bool { return c.b().waxed }

// CarriedItem returns the single stack currently held for transfer.
func (c *CopperGolem) CarriedItem() item.Stack { return c.b().carried }

// Antenna returns the flower gifted to the golem by an iron golem, if any.
func (c *CopperGolem) Antenna() item.Stack { return c.b().antenna }

// SetAntenna installs a single gifted flower.
func (c *CopperGolem) SetAntenna(stack item.Stack) {
	if flower, ok := stack.Item().(block.Flower); ok && flower.Type == block.Poppy() && !stack.Empty() && c.b().antenna.Empty() {
		c.b().antenna = stack.Grow(-stack.Count() + 1)
	}
}

func (c *CopperGolem) Tick(tx *world.Tx, n int64) {
	c.b().effects.Tick(c, tx)
	c.updateWeathering(tx, n)
	c.Ent.Tick(tx, n)
}
func (b *CopperGolemBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.cooldown > 0 {
		b.cooldown--
	}
	if b.targetSet {
		b.followTarget(e, tx)
	} else if b.cooldown == 0 {
		b.findContainer(e, tx)
	} else {
		b.wander(e, tx)
	}
	v, r := e.data.Vel, e.data.Rot
	if p, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .9); ok {
		d := p.Sub(e.data.Pos)
		d[1] = 0
		if d.Len() < .3 {
			b.navigator.Index++
		} else {
			d = d.Normalize()
			v[0], v[2] = d[0]*b.speed, d[2]*b.speed
			r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
		}
	} else {
		v[0], v[2] = 0, 0
	}
	return b.mc.TickMovement(e, e.data.Pos, v, r, e.data.Rot, tx)
}
func (b *CopperGolemBehaviour) findContainer(e *Ent, tx *world.Tx) {
	origin := cube.PosFromVec3(e.Position())
	var source, dest cube.Pos
	sourceFound, destFound := false, false
	bestS, bestD := math.MaxFloat64, math.MaxFloat64
	for y := origin[1] - 8; y <= origin[1]+8; y++ {
		for x := origin[0] - 32; x <= origin[0]+32; x++ {
			for z := origin[2] - 32; z <= origin[2]+32; z++ {
				p := cube.Pos{x, y, z}
				dist := origin.Vec3Middle().Sub(p.Vec3Middle()).LenSqr()
				switch tx.Block(p).(type) {
				case block.CopperChest:
					if dist < bestS {
						source, bestS, sourceFound = p, dist, true
					}
				case block.Chest:
					if dist < bestD {
						dest, bestD, destFound = p, dist, true
					}
				}
			}
		}
	}
	if b.carried.Empty() && sourceFound {
		b.setPath(tx, origin, source)
	} else if !b.carried.Empty() && destFound {
		b.setPath(tx, origin, dest)
	} else {
		b.cooldown = 40
		b.state = 0
	}
}
func (b *CopperGolemBehaviour) setPath(tx *world.Tx, start, container cube.Pos) {
	targets := [...]cube.Pos{{1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}}
	var best []cube.Pos
	for _, delta := range targets {
		path := FindGroundPath(tx, start, container.Add(delta), GroundProfile{MaxVisitedNodes: 2048})
		if len(path) != 0 && (len(best) == 0 || len(path) < len(best)) {
			best = path
		}
	}
	if len(best) == 0 {
		b.cooldown = 40
		return
	}
	b.navigator = Navigator{Path: best, Target: container, HasTarget: true}
	b.interactTicks = 0
}
func (b *CopperGolemBehaviour) followTarget(e *Ent, tx *world.Tx) {
	if b.navigator.Index < len(b.navigator.Path) {
		return
	}
	b.interactTicks++
	container, ok := tx.Block(b.target).(interface {
		Inventory(*world.Tx, cube.Pos) *inventory.Inventory
		StartOpen(*world.Tx, cube.Pos)
		StopOpen(*world.Tx, cube.Pos)
	})
	if !ok {
		b.targetSet = false
		b.navigator.Path = nil
		b.state = 0
		return
	}
	if b.interactTicks == 1 {
		container.StartOpen(tx, b.target)
		inv := container.Inventory(tx, b.target)
		if b.carried.Empty() {
			b.state = int(CopperGolemGettingNoItem)
			for _, stack := range inv.Slots() {
				if !stack.Empty() {
					b.state = int(CopperGolemGettingItem)
					break
				}
			}
		} else if inventoryAccepts(inv, b.carried) {
			b.state = int(CopperGolemDroppingItem)
		} else {
			b.state = int(CopperGolemDroppingNoItem)
		}
	}
	if b.interactTicks == 9 {
		name := [...]string{"entity.copper_golem.item_get", "entity.copper_golem.item_get", "entity.copper_golem.no_item_get", "entity.copper_golem.item_drop", "entity.copper_golem.no_item_drop"}[b.state]
		tx.PlaySound(e.Position(), sound.Custom{Name: name, Volume: 1, Pitch: 1})
	}
	if b.interactTicks == 20 {
		inv := container.Inventory(tx, b.target)
		if b.carried.Empty() {
			b.carried = takeFirst(inv)
			if b.carried.Empty() {
				b.state = 2
			}
		} else {
			before := b.carried
			putSorted(inv, &b.carried)
			if before.Count() == b.carried.Count() {
				b.state = 4
			}
		}
	}
	if b.interactTicks >= 60 {
		container.StopOpen(tx, b.target)
		b.targetSet = false
		b.navigator.Path, b.navigator.Index = nil, 0
		b.interactTicks, b.state = 0, 0
		b.cooldown = 60 + b.randomInt(41)
	}
}
func inventoryAccepts(inv *inventory.Inventory, carried item.Stack) bool {
	for _, stack := range inv.Slots() {
		if stack.Empty() || stack.Comparable(carried) && stack.Count() < stack.MaxCount() {
			return true
		}
	}
	return false
}
func takeFirst(inv *inventory.Inventory) item.Stack {
	for slot, stack := range inv.Slots() {
		if !stack.Empty() {
			one := stack.Grow(-stack.Count() + 1)
			_ = inv.SetItem(slot, stack.Grow(-1))
			return one
		}
	}
	return item.Stack{}
}
func putSorted(inv *inventory.Inventory, carried *item.Stack) {
	if carried.Empty() {
		return
	}
	slots := inv.Slots()
	for slot, stack := range slots {
		if stack.Comparable(*carried) && stack.Count() < stack.MaxCount() {
			count := min(carried.Count(), stack.MaxCount()-stack.Count())
			_ = inv.SetItem(slot, stack.Grow(count))
			*carried = carried.Grow(-count)
			if carried.Empty() {
				return
			}
		}
	}
	for slot, stack := range slots {
		if stack.Empty() {
			_ = inv.SetItem(slot, *carried)
			*carried = item.Stack{}
			return
		}
	}
}
func (b *CopperGolemBehaviour) randomInt(bound int) int { return int(b.random.NextInt(int32(bound))) }
func (b *CopperGolemBehaviour) wander(e *Ent, tx *world.Tx) {
	if b.nextWander > 0 {
		b.nextWander--
	}
	if b.nextWander == 0 {
		b.nextWander = 40 + b.randomInt(81)
		start := cube.PosFromVec3(e.Position())
		for range 4 {
			goal := start.Add(cube.Pos{b.randomInt(13) - 6, 0, b.randomInt(13) - 6})
			if p := FindGroundPath(tx, start, goal, GroundProfile{MaxVisitedNodes: 128}); len(p) > 0 {
				b.navigator = Navigator{Path: p, Target: goal, HasTarget: true}
				break
			}
		}
	}
}
func (c *CopperGolem) updateWeathering(tx *world.Tx, tick int64) {
	b := c.b()
	if b.waxed {
		return
	}
	if b.nextWeatheringTick == -1 {
		b.nextWeatheringTick = tick + int64(504000+b.randomInt(48001))
		return
	}
	if b.weather < 3 && tick >= b.nextWeatheringTick {
		b.weather++
		if b.weather == 3 {
			b.nextWeatheringTick = 0
		} else {
			b.nextWeatheringTick += int64(504000 + b.randomInt(48001))
		}
	}
	if b.weather == 3 && float64(b.random.NextFloat()) <= .0058 {
		p := cube.PosFromVec3(c.Position())
		if replaceable, ok := tx.Block(p).(block.Replaceable); ok && replaceable.ReplaceableBy(block.CopperGolemStatue{}) {
			poses := block.CopperGolemPoses()
			tx.SetBlock(p, block.CopperGolemStatue{Pose: poses[b.randomInt(len(poses))], Oxidation: block.OxidisedOxidation(), Facing: c.Rotation().Direction()}, nil)
			tx.PlaySound(c.Position(), sound.Custom{Name: "entity.copper_golem.become_statue", Volume: 1, Pitch: 1})
			c.dropEquipment(tx)
			_ = c.CloseIn(tx)
		}
	}
}
func (c *CopperGolem) dropEquipment(tx *world.Tx) {
	b := c.b()
	for _, stack := range []item.Stack{b.antenna, b.carried} {
		if !stack.Empty() {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, stack))
		}
	}
	b.antenna, b.carried = item.Stack{}, item.Stack{}
}
func (c *CopperGolem) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	b := c.b()
	if u == nil {
		return false
	}
	main, _ := u.HeldItems()
	if main.Empty() && !b.carried.Empty() {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: u.Position().Add(mgl64.Vec3{0, 1, 0})}, b.carried))
		b.carried = item.Stack{}
		return true
	}
	switch main.Item().(type) {
	case item.Honeycomb:
		if b.waxed {
			return false
		}
		b.waxed, b.nextWeatheringTick = true, -2
		ctx.SubtractFromCount(1)
		tx.PlaySound(c.Position(), sound.Custom{Name: "item.honeycomb.wax_on", Volume: 1, Pitch: 1})
		return true
	case item.Axe:
		if b.waxed {
			b.waxed, b.nextWeatheringTick = false, -1
			ctx.DamageItem(1)
			tx.PlaySound(c.Position(), sound.Custom{Name: "item.axe.wax_off", Volume: 1, Pitch: 1})
			return true
		}
		if b.weather > 0 {
			b.weather--
			b.nextWeatheringTick = -1
			ctx.DamageItem(1)
			tx.PlaySound(c.Position(), sound.Custom{Name: "item.axe.scrape", Volume: 1, Pitch: 1})
			return true
		}
	case item.Shears:
		if !b.antenna.Empty() {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position().Add(mgl64.Vec3{0, 1.5, 0})}, b.antenna))
			b.antenna = item.Stack{}
			ctx.DamageItem(1)
			tx.PlaySound(c.Position(), sound.Custom{Name: "entity.copper_golem.shear", Volume: 1, Pitch: 1})
			return true
		}
	}
	return false
}
func (c *CopperGolem) Health() float64        { return c.b().health.Health() }
func (c *CopperGolem) MaxHealth() float64     { return c.b().health.MaxHealth() }
func (c *CopperGolem) SetMaxHealth(v float64) { c.b().health.SetMaxHealth(v) }
func (c *CopperGolem) Dead() bool             { return c.b().dead || c.Health() <= 0 }
func (c *CopperGolem) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := c.b()
	if c.Dead() || d <= 0 || s.Fire() && c.hasEffect(effect.FireResistance) {
		return 0, false
	}
	if lightning, ok := s.(LightningDamageSource); ok {
		c.thunderHit(lightning.BoltUUID)
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	c.viewAction(HurtAction{})
	soundName := "entity.copper_golem.hurt"
	if b.weather == 2 {
		soundName = "entity.copper_golem.weathered_hurt"
	} else if b.weather == 3 {
		soundName = "entity.copper_golem.oxidized_hurt"
	}
	if b.health.Health() <= 0 {
		soundName = "entity.copper_golem.death"
		if b.weather == 2 {
			soundName = "entity.copper_golem.weathered_death"
		} else if b.weather == 3 {
			soundName = "entity.copper_golem.oxidized_death"
		}
	}
	c.tx.PlaySound(c.Position(), sound.Custom{Name: soundName, Volume: 1, Pitch: 1})
	if c.Dead() {
		b.dead = true
		c.viewAction(DeathAction{})
		c.dropEquipment(c.tx)
		c.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, item.NewStack(item.CopperIngot{}, 1+c.b().randomInt(3))))
		_ = c.CloseIn(c.tx)
	}
	return d, true
}

// ThunderHit applies the Java lightning de-oxidation transition.
func (c *CopperGolem) ThunderHit() { c.thunderHit("") }
func (c *CopperGolem) thunderHit(boltUUID string) {
	b := c.b()
	if boltUUID != "" && boltUUID == b.lastLightningBolt {
		return
	}
	b.lastLightningBolt = boltUUID
	if b.weather > 0 {
		b.weather--
		b.nextWeatheringTick = -1
	}
}
func (c *CopperGolem) Heal(h float64, _ world.HealingSource) float64 {
	if c.Dead() || h <= 0 {
		return 0
	}
	old := c.Health()
	c.b().health.AddHealth(h)
	return c.Health() - old
}
func (c *CopperGolem) KnockBack(s mgl64.Vec3, f, h float64) {
	d := c.Position().Sub(s)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	c.SetVelocity(d)
}
func (c *CopperGolem) AddEffect(e effect.Effect)    { c.b().effects.Add(e, c) }
func (c *CopperGolem) RemoveEffect(t effect.Type)   { c.b().effects.Remove(t, c) }
func (c *CopperGolem) Effects() []effect.Effect     { return c.b().effects.Effects() }
func (c *CopperGolem) Speed() float64               { return c.b().speed }
func (c *CopperGolem) SetSpeed(v float64)           { c.b().speed = v }
func (c *CopperGolem) hasEffect(t effect.Type) bool { _, ok := c.b().effects.Effect(t); return ok }
func (c *CopperGolem) viewAction(a world.EntityAction) {
	vs := c.tx.Viewers(c.Position())
	for _, v := range vs {
		v.ViewEntityAction(c, a)
	}
	c.tx.ReleaseViewers(vs)
}

var _ Living = (*CopperGolem)(nil)
var _ Interactable = (*CopperGolem)(nil)
var _ = sound.Custom{}
