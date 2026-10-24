package entity

import (
	"hash/fnv"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
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
	golem.b().random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
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
	b := &CopperGolemBehaviour{health: NewHealthManager(health, maxHealth), effects: NewEffectManager(), speed: .2, weather: max(0, min(c.WeatherState, 3)), state: max(0, min(c.State, 4)), waxed: c.Waxed, nextWeatheringTick: c.NextWeatheringTick, antenna: c.Antenna, carried: c.Carried}
	b.br = newCopperGolemBrain(b)
	return b
}

type CopperGolemBehaviour struct {
	health             *HealthManager
	effects            *EffectManager
	ai                 MobAI
	br                 *Brain
	speed              float64
	random             vanilla.Random
	weather, state     int
	waxed, dead        bool
	nextWeatheringTick int64
	antenna            item.Stack
	carried            item.Stack
	lastLightningBolt  string
	// openedChest is the chest the golem has open, CopperGolem.openedChestPos.
	openedChest *cube.Pos
}

// CopperGolem is a mobile item-sorting golem that oxidises over time.
type CopperGolem struct{ *Ent }

func NewCopperGolem(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(CopperGolemType, CopperGolemBehaviourConfig{NextWeatheringTick: -1})
}

// TrySpawnCopperGolemPattern ports the copper golem pattern of
// CarvedPumpkinBlock.trySpawnGolem in every orientation supported by Java's
// block-pattern matcher: a carved pumpkin on a copper block becomes a copper
// golem of the oxidation of the block, standing on a copper chest that
// replaces the block.
func TrySpawnCopperGolemPattern(tx *world.Tx, pumpkinPos cube.Pos) bool {
	var facing cube.Direction
	switch head := tx.Block(pumpkinPos).(type) {
	case block.Pumpkin:
		if !head.Carved {
			return false
		}
		facing = head.Facing
	case block.LitPumpkin:
		facing = head.Facing
	default:
		return false
	}
	var copperPos cube.Pos
	var copper block.Copper
	if !forEachGolemPatternOrientation(func(_, up, _ cube.Pos) bool {
		candidate := pumpkinPos.Sub(up)
		c, ok := tx.Block(candidate).(block.Copper)
		if !ok || c.Type != block.NormalCopper() {
			return false
		}
		copperPos, copper = candidate, c
		return true
	}) {
		return false
	}
	// CarvedPumpkinBlock.spawnGolemInWorld clears the pattern and spawns the
	// golem where the pumpkin was.
	tx.SetBlock(pumpkinPos, block.Air{}, nil)
	tx.SetBlock(copperPos, block.Air{}, nil)
	golem := tx.AddEntity(NewCopperGolem(world.EntitySpawnOpts{Position: pumpkinPos.Vec3().Add(mgl64.Vec3{.5, .05, .5})}))
	// CarvedPumpkinBlock.replaceCopperBlockWithChest.
	chest := block.NewCopperChest()
	chest.Facing, chest.Oxidation, chest.Waxed = facing, copper.Oxidation, copper.Waxed
	tx.SetBlock(copperPos, chest, nil)
	// CopperGolem.spawn.
	if g, ok := golem.(*CopperGolem); ok {
		g.b().weather = int(copper.Oxidation.Uint8())
		g.updateState()
	}
	return true
}

func (*CopperGolem) SpeciesType() world.EntityType { return CopperGolemType }
func (c *CopperGolem) b() *CopperGolemBehaviour    { return c.Behaviour().(*CopperGolemBehaviour) }

// FinalizeSpawn plays the one-time creation sound.
func (c *CopperGolem) FinalizeSpawn(_ string, _ *vanilla.Random) {
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
func (b *CopperGolemBehaviour) mobAI() *MobAI   { return &b.ai }
func (b *CopperGolemBehaviour) brain() *Brain   { return b.br }
func (b *CopperGolemBehaviour) panicking() bool { return b.br.has(memIsPanicking) }

func (b *CopperGolemBehaviour) transportHeld() item.Stack     { return b.carried }
func (b *CopperGolemBehaviour) setTransportHeld(s item.Stack) { b.carried = s }

// Tick runs the MobAI of the copper golem with its brain.
func (b *CopperGolemBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	// CopperGolem.createAttributes.
	b.ai.MovementSpeed, b.ai.StepHeight = b.speed, 1
	return b.ai.Tick(e, &CopperGolem{Ent: e}, func(m *Mob) {
		m.AI.nav.SetRequiredPathLength(m, 48)
		m.AI.nav.SetCanOpenDoors(true)
		m.AI.SetPathfindingMalus(PathFireInNeighbour, 16)
		m.AI.SetPathfindingMalus(PathDamagingInNeighbour, 16)
		m.AI.SetPathfindingMalus(PathFire, -1)
		if !b.br.has(memTransportItemsCooldownTicks) {
			b.br.set(memTransportItemsCooldownTicks, 60+int(m.Random().NextInt(40)))
		}
	})
}

// customServerAiStep ports CopperGolem.customServerAiStep and
// CopperGolemAi.updateActivity.
func (b *CopperGolemBehaviour) customServerAiStep(m *Mob) {
	b.br.tick(m)
	b.br.setActiveToFirstValid(activityIdle)
}

// newCopperGolemBrain ports CopperGolemAi and the brain provider of
// CopperGolem: a copper golem takes items out of copper chests and sorts
// them into chests holding the same item.
func newCopperGolemBrain(b *CopperGolemBehaviour) *Brain {
	br := newBrain(nearestLivingEntitySensor{}, hurtBySensor{})
	br.addActivity(activityCore, 0,
		newAnimalPanic(1.5, nil, nil, nil),
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newInteractWithDoor(),
		newCountDownCooldownTicks(memGazeCooldownTicks),
		newCountDownCooldownTicks(memTransportItemsCooldownTicks),
	)
	transport := &transportItems{
		speed: 1, horizontal: 32, vertical: 8,
		source: func(bl world.Block) bool { _, ok := bl.(block.CopperChest); return ok },
		destination: func(bl world.Block) bool {
			switch bl.(type) {
			case block.Chest, block.TrappedChest:
				return true
			}
			return false
		},
		onReached: b.onReachedChest,
		onSetOff: func(m *Mob) {
			b.closeChest(m.tx, m.H())
			b.setState(m.Ent, CopperGolemIdle)
		},
		// CopperGolemAi.shouldQueueForTarget: a chest somebody has open.
		shouldQueue: func(_ *Mob, t transportTarget) bool { return t.chest.Opened() },
	}
	br.addActivityWith(activityIdle, []prioritizedBehavior{
		{0, newTransportItemsBetweenContainers(transport)},
		{1, newSetEntityLookTargetSometimes(isPlayerType, 6, uniformInt{40, 80})},
		{2, newRunOne([]memoryCondition{{memWalkTarget, memAbsent}, {memTransportItemsCooldownTicks, memPresent}},
			weightedBehavior{newRandomStrollWithin(1, 2, 2), 1},
			weightedBehavior{newDoNothing(30, 60), 1},
		)},
	}, nil, nil)
	return br
}

// copperGolemInteractions are the states and sounds of the interactions of
// a copper golem with a chest it reached.
var copperGolemInteractions = [...]struct {
	state CopperGolemState
	sound string
}{
	transportPickupItem:   {CopperGolemGettingItem, "entity.copper_golem.no_item_get"},
	transportPickupNoItem: {CopperGolemGettingNoItem, "entity.copper_golem.no_item_no_get"},
	transportPlaceItem:    {CopperGolemDroppingItem, "entity.copper_golem.item_drop"},
	transportPlaceNoItem:  {CopperGolemDroppingNoItem, "entity.copper_golem.item_no_drop"},
}

// onReachedChest ports CopperGolemAi.onReachedTargetInteraction: the golem
// opens the chest, plays the sound of what it does after nine ticks and
// closes the chest after three seconds.
func (b *CopperGolemBehaviour) onReachedChest(m *Mob, t transportTarget, interaction transportInteraction, ticks int) {
	i := copperGolemInteractions[interaction]
	switch ticks {
	case 1:
		b.openChest(m.tx, m.H(), t.pos)
		b.setState(m.Ent, i.state)
	case 9:
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: i.sound, Volume: 1, Pitch: 1})
	case 60:
		b.closeChest(m.tx, m.H())
	}
}

// openChest ports Container.startOpen for a copper golem: it counts as an
// opener of the chest until it closes it.
func (b *CopperGolemBehaviour) openChest(tx *world.Tx, h *world.EntityHandle, pos cube.Pos) {
	b.closeChest(tx, h)
	c, ok := tx.Block(pos).(block.Container)
	if !ok {
		return
	}
	c.AddViewer(block.MobContainerOpener{Handle: h}, tx, pos)
	b.openedChest = &pos
}

// closeChest ports Container.stopOpen and CopperGolem.clearOpenedChestPos.
func (b *CopperGolemBehaviour) closeChest(tx *world.Tx, h *world.EntityHandle) {
	if b.openedChest == nil {
		return
	}
	pos := *b.openedChest
	b.openedChest = nil
	if c, ok := tx.Block(pos).(block.Container); ok {
		c.RemoveViewer(block.MobContainerOpener{Handle: h}, tx, pos)
	}
}

// setState ports CopperGolem.setState: viewers see the state of the golem.
func (b *CopperGolemBehaviour) setState(e *Ent, state CopperGolemState) {
	if b.state != int(state) {
		b.state = int(state)
		e.updateState()
	}
}

func (b *CopperGolemBehaviour) randomInt(bound int) int { return int(b.random.NextInt(int32(bound))) }

// updateWeathering ports CopperGolem.updateWeathering: an unwaxed copper
// golem oxidises a stage every seven hours or so, and a fully oxidised one
// may turn into a statue where it stands.
func (c *CopperGolem) updateWeathering(tx *world.Tx, tick int64) {
	b := c.b()
	if b.waxed {
		return
	}
	if b.nextWeatheringTick == -1 {
		b.nextWeatheringTick = tick + int64(504000+b.randomInt(48001))
		return
	}
	oxidised := b.weather == 3
	if tick >= b.nextWeatheringTick && !oxidised {
		b.weather++
		if b.weather == 3 {
			b.nextWeatheringTick = 0
		} else {
			b.nextWeatheringTick += int64(504000 + b.randomInt(48001))
		}
		c.updateState()
	}
	if !oxidised {
		return
	}
	p := cube.PosFromVec3(c.Position())
	if _, air := tx.Block(p).(block.Air); !air || float64(b.random.NextFloat()) > .0058 {
		return
	}
	poses := block.CopperGolemPoses()
	tx.SetBlock(p, block.CopperGolemStatue{Pose: poses[b.randomInt(len(poses))], Oxidation: block.OxidisedOxidation(), Facing: c.Rotation().Direction()}, nil)
	b.closeChest(tx, c.H())
	c.dropEquipment(tx)
	tx.PlaySound(c.Position(), sound.Custom{Name: "entity.copper_golem_become_statue", Volume: 1, Pitch: 1})
	_ = c.CloseIn(tx)
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

// Interact ports CopperGolem.mobInteract: an empty hand takes the item the
// golem carries, shears take its flower, honeycomb waxes it and an axe
// scrapes off the wax or a stage of oxidation.
func (c *CopperGolem) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	b := c.b()
	if u == nil {
		return false
	}
	main, _ := u.HeldItems()
	if main.Empty() && !b.carried.Empty() {
		// BehaviorUtils.throwItem towards the player.
		m := b.ai.Mob(c.Ent, c)
		throwItemTowards(m, b.carried, u.Position(), mgl64.Vec3{float64(float32(.3)), float64(float32(.3)), float64(float32(.3))}, float64(float32(.3)))
		b.carried = item.Stack{}
		return true
	}
	switch main.Item().(type) {
	case item.Shears:
		if !b.antenna.Empty() {
			tx.PlaySound(c.Position(), sound.Custom{Name: "entity.copper_golem.shear", Volume: 1, Pitch: 1})
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position().Add(mgl64.Vec3{0, 1.5, 0})}, b.antenna))
			b.antenna = item.Stack{}
			ctx.DamageItem(1)
			return true
		}
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
			tx.PlaySound(c.Position(), sound.Custom{Name: "item.axe.scrape", Volume: 1, Pitch: 1})
			return true
		}
		if b.weather > 0 {
			b.weather--
			b.nextWeatheringTick = -1
			ctx.DamageItem(1)
			tx.PlaySound(c.Position(), sound.Custom{Name: "item.axe.scrape", Volume: 1, Pitch: 1})
			c.updateState()
			return true
		}
	}
	return false
}
func (c *CopperGolem) Health() float64        { return c.b().health.Health() }
func (c *CopperGolem) MaxHealth() float64     { return c.b().health.MaxHealth() }
func (c *CopperGolem) SetMaxHealth(v float64) { c.b().health.SetMaxHealth(v) }
func (c *CopperGolem) Dead() bool             { return c.b().dead || c.Health() <= 0 }

// Panicking reports whether the copper golem runs from danger.
func (c *CopperGolem) Panicking() bool { return c.b().panicking() }

// Hurt ports CopperGolem.actuallyHurt: a hurt copper golem stops what it
// does with a chest.
func (c *CopperGolem) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := c.b()
	if c.Dead() || d <= 0 {
		return 0, false
	}
	if lightning, ok := s.(LightningDamageSource); ok {
		c.thunderHit(lightning.BoltUUID)
	}
	var ok bool
	if d, ok = b.ai.DamageCooldown(c.tx, d); !ok {
		return 0, false
	}
	if d, ok = mobEffectDamage(b.effects, d, s); !ok {
		return 0, false
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(s)
	b.setState(c.Ent, CopperGolemIdle)
	c.viewAction(HurtAction{})
	onMobHurt(c.tx, c)
	if c.Dead() {
		b.dead = true
		b.closeChest(c.tx, c.H())
		c.viewAction(DeathAction{})
		c.dropEquipment(c.tx)
		dropAllDeathLoot(c.tx, c, s)
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
		if c.tx != nil {
			c.updateState()
		}
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
	knockBack(c, s, f, h)
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
