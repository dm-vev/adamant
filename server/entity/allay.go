package entity

import (
	"time"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/internal/nbtconv"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
	"github.com/dm-vev/adamant/server/world/particle"
	"github.com/dm-vev/adamant/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// AllayType is the Java minecraft:allay entity type.
var entityAllayType allayType

type allayType struct{}

func (allayType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		allayBehaviourConfig{}.Apply(d)
	}
	return &allay{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (allayType) EncodeEntity() string        { return "minecraft:allay" }
func (allayType) BBox(world.Entity) cube.BBox { return cube.Box(-.35, 0, -.35, .35, .6, .35) }
func (allayType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := allayBehaviourConfig{}
	javaState := readNativeMobJavaState(m)
	source := m
	if javaState != nil {
		source = javaState
	}
	if _, ok := source["Health"]; ok {
		c.Health = float64(nbtconv.Float32(source, "Health"))
		c.healthSet = true
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.healthSet = h.Current, true
	}
	if _, ok := source["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(source, "MaxHealth"))
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.MaxHealth = h.Base
	}
	c.LikedPlayer = nbtconv.String(source, "LikedPlayer")
	if cooldown, ok := source["DuplicationCooldown"].(int64); ok {
		c.Cooldown = int(cooldown)
	} else {
		c.Cooldown = int(nbtconv.Int32(source, "DuplicationCooldown"))
	}
	c.Dancing = nbtconv.Bool(source, "Dancing")
	if in, ok := source["Inventory"].([]any); ok {
		for _, raw := range in {
			if tag, ok := raw.(map[string]any); ok {
				if s, err := item.StackFromStructureNBT(tag); err == nil && !s.Empty() {
					c.Inventory = append(c.Inventory, s)
				}
			}
		}
	} else {
		for _, native := range readNativeMobEquipment(m).ChestItems {
			if s, ok := allayNativeStack(native); ok {
				c.Inventory = append(c.Inventory, s)
			}
		}
	}
	if hand, ok := source["HandItems"].([]any); ok && len(hand) > 0 {
		if tag, ok := hand[0].(map[string]any); ok {
			c.Held, _ = item.StackFromStructureNBT(tag)
		}
	} else if tag, ok := source["HeldItem"].(map[string]any); ok {
		c.Held, _ = item.StackFromStructureNBT(tag)
	} else {
		equipment := readNativeMobEquipment(m)
		if len(equipment.Mainhand) > 0 {
			c.Held, _ = allayNativeStack(equipment.Mainhand[0])
		}
	}
	d.Data = c.New()
}
func (allayType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*allayBehaviour)
	attrs := encodeNativeMobAttributes(readNativeMobAttributes(d.NativeNBT), []nativeMobAttr{{Name: "minecraft:health", Base: b.health.MaxHealth(), Current: b.health.Health(), DefaultMin: 0, DefaultMax: b.health.MaxHealth(), Min: 0, Max: b.health.MaxHealth()}, {Name: "minecraft:movement", Base: .1, Current: .1, DefaultMin: 0, DefaultMax: .1, Min: 0, Max: .1}, {Name: "minecraft:attack_damage", Base: 2, Current: 2, DefaultMin: 0, DefaultMax: 2, Min: 0, Max: 2}})
	native := mergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs})
	equipment := readNativeMobEquipment(d.NativeNBT)
	if len(equipment.Mainhand) == 0 {
		equipment.Mainhand = []nativeMobItem{{}}
	}
	if len(equipment.Offhand) == 0 {
		equipment.Offhand = []nativeMobItem{{}}
	}
	equipment.Mainhand[0] = allayNativeItem(b.held, equipment.Mainhand[0])
	if len(equipment.ChestItems) == 0 {
		equipment.ChestItems = []nativeMobItem{{}}
	}
	slot := int32(0)
	equipment.ChestItems[0].Slot = &slot
	equipment.ChestItems[0] = allayNativeItem(b.inventory, equipment.ChestItems[0])
	equipment.ChestItems[0].Slot = &slot
	native = mergeNativeMobNBT(native, encodeNativeMobEquipment(equipment))
	inv := make([]any, 0, 1)
	if !b.inventory.Empty() {
		inv = append(inv, allayWriteStack(b.inventory))
	}
	liked := ""
	if v, ok := b.br.get(memLikedPlayer); ok {
		liked = v.(uuid.UUID).String()
	}
	java := mergeNativeMobNBT(readNativeMobJavaState(d.NativeNBT), map[string]any{"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()), "LikedPlayer": liked, "DuplicationCooldown": int64(b.duplicationCooldown), "Dancing": byte(catBool(b.dancing)), "Inventory": inv, "HandItems": []any{allayWriteStack(b.held), map[string]any{}}})
	return mergeNativeMobJavaState(native, java)
}
func allayNativeItem(s item.Stack, previous nativeMobItem) nativeMobItem {
	out := nativeMobItem{Raw: previous.Raw}
	if s.Empty() {
		out.Name = ""
		out.Count = 0
		out.Damage = 0
		out.WasPickedUp = 0
		return out
	}
	name, meta := s.Item().EncodeItem()
	out.Name = name
	out.Count = byte(s.Count())
	out.Damage = int32(meta)
	out.WasPickedUp = 0
	if s.MaxDurability() > 0 {
		out.Damage = int32(s.MaxDurability() - s.Durability())
	}
	return out
}
func allayNativeStack(s nativeMobItem) (item.Stack, bool) {
	if s.Name == "" || s.Count == 0 {
		return item.Stack{}, false
	}
	i, ok := world.ItemByName(s.Name, int16(s.Damage))
	if !ok {
		return item.Stack{}, false
	}
	return item.NewStack(i, int(s.Count)), true
}
func allayWriteStack(s item.Stack) map[string]any {
	raw := item.WriteNBT(s)
	id, ok := raw["Name"].(string)
	if !ok || s.Empty() {
		return map[string]any{}
	}
	out := map[string]any{"id": id, "count": int32(s.Count())}
	if tag, ok := raw["tag"].(map[string]any); ok {
		out["tag"] = tag
	}
	if damage, ok := raw["Damage"]; ok {
		out["components"] = map[string]any{"minecraft:damage": damage}
	}
	return out
}

type allayBehaviourConfig struct {
	Health, MaxHealth float64
	LikedPlayer       string
	Cooldown          int
	Dancing           bool
	Inventory         []item.Stack
	Held              item.Stack
	healthSet         bool
}

func (c allayBehaviourConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c allayBehaviourConfig) New() *allayBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 20
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	b := &allayBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), speed: .1, duplicationCooldown: max(0, c.Cooldown), dancing: c.Dancing, held: c.Held,
		vibrations: &world.VibrationData{}}
	if len(c.Inventory) > 0 {
		b.inventory = c.Inventory[0]
	}
	b.br = newAllayBrain(b)
	if id, err := uuid.Parse(c.LikedPlayer); err == nil {
		b.br.set(memLikedPlayer, id)
	}
	return b
}

type allayBehaviour struct {
	health              *HealthManager
	effects             *EffectManager
	ai                  MobAI
	br                  *brain
	speed               float64
	duplicationCooldown int
	dancing, dead       bool
	// inventory is the one slot of the SimpleContainer of the allay.
	inventory  item.Stack
	held       item.Stack
	jukebox    *cube.Pos
	vibrations *world.VibrationData
	likedPos   *likedNoteblock
}

// likedNoteblock is the note block an allay likes, a GlobalPos.
type likedNoteblock struct {
	dim world.Dimension
	pos cube.Pos
}

type allay struct{ *Ent }

func NewAllay(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(entityAllayType, allayBehaviourConfig{})
}
func (a *allay) SpeciesType() world.EntityType { return entityAllayType }
func (a *allay) b() *allayBehaviour            { return a.Behaviour().(*allayBehaviour) }
func (a *allay) Tick(tx *world.Tx, n int64) {
	b := a.b()
	b.effects.Tick(a, tx)
	a.Ent.Tick(tx, n)
	if b.dead || a.tx == nil {
		return
	}
	// Allay.tick: the vibrations an allay hears travel to it, and a
	// panicking allay stops dancing.
	tx.World().TickEntityVibrationSystem(tx, a)
	if a.Panicking() {
		a.SetDancing(false)
	}
}

func (b *allayBehaviour) mobAI() *MobAI   { return &b.ai }
func (b *allayBehaviour) brain() *brain   { return b.br }
func (b *allayBehaviour) panicking() bool { return b.br.has(memIsPanicking) }

// Tick runs the MobAI of the allay with its brain.
func (b *allayBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	a := &allay{Ent: e}
	// Allay.createAttributes.
	b.ai.MovementSpeed, b.ai.FlyingSpeed = b.speed, float64(float32(.1))
	return b.ai.Tick(e, a, func(m *Mob) {
		m.AI.nav.SetFlying()
		m.AI.nav.SetCanOpenDoors(false)
		m.AI.nav.SetCanFloat(true)
		m.AI.nav.SetRequiredPathLength(m, 48)
		m.AI.moveOverride = flyingMoveControl{maxTurn: 20, hoversInPlace: true}
	})
}

// customServerAiStep ports Allay.customServerAiStep and AllayAi.updateActivity.
func (b *allayBehaviour) customServerAiStep(m *Mob) {
	b.br.tick(m)
	b.br.setActiveToFirstValid(activityIdle)
}

// travel ports Allay.travel: an allay flies at its speed.
func (b *allayBehaviour) travel(m *Mob, input mgl64.Vec3) bool {
	m.AI.travelFlying(m, input, m.AI.speed, m.AI.speed, m.AI.speed)
	return true
}

// afterAiStep ports what Allay.aiStep and Mob.aiStep do after the movement
// of the allay: it picks up the items it wants, heals a point every half
// second, stops dancing once the jukebox is gone and counts down until it
// may duplicate again.
func (b *allayBehaviour) afterAiStep(m *Mob) {
	a := m.Self.(*allay)
	if b.canPickUpLoot() && !b.dead && !mobGriefingDisabled(m.tx) {
		box := m.BBox().Grow(1)
		for e := range m.tx.EntitiesOfTypesWithin(box.Grow(1), itemEntityTypes) {
			stack, ok := pickableItem(e)
			if !ok || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) || !b.wantsToPickUp(m.tx, stack) {
				continue
			}
			b.pickUpItem(m, e, stack)
		}
	}
	if !b.dead && m.AI.tickCount%10 == 0 {
		a.Heal(1, nil)
	}
	if b.dancing && b.shouldStopDancing(m) && m.AI.tickCount%20 == 0 {
		a.SetDancing(false)
		b.jukebox = nil
	}
	if b.duplicationCooldown > 0 {
		if b.duplicationCooldown--; b.duplicationCooldown == 0 {
			a.updateState()
		}
	}
}

// canPickUpLoot ports Allay.canPickUpLoot: an allay holding an item picks up
// more, unless it just gave some away.
func (b *allayBehaviour) canPickUpLoot() bool {
	return !b.br.has(memItemPickupCooldownTicks) && !b.held.Empty()
}

// wantsToPickUp ports Allay.wantsToPickUp: items like the one it holds that
// fit in its inventory.
func (b *allayBehaviour) wantsToPickUp(tx *world.Tx, stack item.Stack) bool {
	return !b.held.Empty() && !mobGriefingDisabled(tx) && allayCanAddItem(b.inventory, stack) && allaySameItem(b.held, stack)
}

// allaySameItem ports Allay.allayConsidersItemEqual: the same item, down to
// the potion.
func allaySameItem(a, b item.Stack) bool {
	if a.Empty() || b.Empty() {
		return false
	}
	an, am := a.Item().EncodeItem()
	bn, bm := b.Item().EncodeItem()
	return an == bn && am == bm
}

// allayCanAddItem ports SimpleContainer.canAddItem for the one slot of an
// allay.
func allayCanAddItem(slot, stack item.Stack) bool {
	return slot.Empty() || slot.Comparable(stack) && slot.Count() < slot.MaxCount()
}

// pickUpItem ports InventoryCarrier.pickUpItem.
func (b *allayBehaviour) pickUpItem(m *Mob, e world.Entity, stack item.Stack) {
	if !allayCanAddItem(b.inventory, stack) {
		return
	}
	taken := stack.Count()
	if b.inventory.Empty() {
		b.inventory = stack
	} else {
		merged, left := b.inventory.AddStack(stack)
		b.inventory, taken = merged, stack.Count()-left.Count()
	}
	world.BroadcastEntityAction(m.tx, e, PickedUpAction{Collector: m.Self.(Collector)})
	takeItemEntity(m, e, taken)
}

// Collect is never used by item entities: an allay picks up items itself.
func (a *allay) Collect(item.Stack) (int, bool) { return 0, false }

// shouldStopDancing ports Allay.shouldStopDancing: the jukebox is gone or
// out of earshot.
func (b *allayBehaviour) shouldStopDancing(m *Mob) bool {
	if b.jukebox == nil {
		return true
	}
	if _, ok := m.tx.Block(*b.jukebox).(block.Jukebox); !ok {
		return true
	}
	return b.jukebox.Vec3Centre().Sub(m.data.Pos).LenSqr() >= 10*10
}

// likedPlayer ports AllayAi.getLikedPlayer: the player the allay likes, in
// survival or creative and within 64 blocks.
func (b *allayBehaviour) likedPlayer(m *Mob) world.Entity {
	v, ok := b.br.get(memLikedPlayer)
	if !ok {
		return nil
	}
	p, ok := m.tx.EntityByUUID(v.(uuid.UUID))
	if !ok || !isPlayerEntity(p) {
		return nil
	}
	if g, ok := p.(interface{ GameMode() world.GameMode }); ok {
		if mode := g.GameMode(); mode != world.GameModeSurvival && mode != world.GameModeCreative {
			return nil
		}
	}
	if p.Position().Sub(m.data.Pos).LenSqr() >= 64*64 {
		return nil
	}
	return p
}

// depositPosition ports AllayAi.getItemDepositPosition: the note block the
// allay likes while it plays, or else the player it likes.
func (b *allayBehaviour) depositPosition(m *Mob) (positionTracker, bool) {
	if b.likedPos != nil {
		if b.shouldDepositAtNoteblock(m) {
			return newBlockPosTracker(b.likedPos.pos.Side(cube.FaceUp)), true
		}
		b.likedPos = nil
		m.brain().erase(memLikedNoteblockPosition)
	}
	if p := b.likedPlayer(m); p != nil {
		return newEntityTracker(p, true), true
	}
	return nil, false
}

// shouldDepositAtNoteblock ports AllayAi.shouldDepositItemsAtLikedNoteblock.
func (b *allayBehaviour) shouldDepositAtNoteblock(m *Mob) bool {
	l := b.likedPos
	if l.dim != m.tx.World().Dimension() || distSqrPos(l.pos, m.BlockPos()) >= 1024*1024 {
		return false
	}
	if _, ok := m.tx.Block(l.pos).(block.Note); !ok {
		return false
	}
	return m.brain().has(memLikedNoteblockCooldownTicks)
}

// hearNoteblock ports AllayAi.hearNoteblock: an allay likes the first note
// block it hears for 30 seconds after each note.
func (b *allayBehaviour) hearNoteblock(m *Mob, pos cube.Pos) {
	br := m.brain()
	dim := m.tx.World().Dimension()
	switch {
	case b.likedPos == nil:
		b.likedPos = &likedNoteblock{dim: dim, pos: pos}
		br.set(memLikedNoteblockPosition, pos)
		br.set(memLikedNoteblockCooldownTicks, 600)
	case *b.likedPos == (likedNoteblock{dim: dim, pos: pos}):
		br.set(memLikedNoteblockCooldownTicks, 600)
	}
}

// throwItem ports AllayAi.throwItem: an allay throws one of the items it
// gathered at where it delivers them.
func (b *allayBehaviour) throwItem(m *Mob, target mgl64.Vec3) {
	if b.inventory.Empty() {
		return
	}
	stack := b.inventory.Grow(1 - b.inventory.Count())
	b.inventory = b.inventory.Grow(-1)
	throwItemTowards(m, stack, target.Add(mgl64.Vec3{0, 1, 0}), mgl64.Vec3{float64(float32(.2)), float64(float32(.3)), float64(float32(.2))}, float64(float32(.2)))
	if m.tx.CurrentTick()%7 == 0 && m.Random().NextDouble() < .9 {
		pitches := [...]float64{.5625, .625, .75, .9375, 1, 1, 1.125, 1.25, 1.5, 1.875, 2, 2.25, 2.5, 3, 3.75, 4}
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.allay.item_thrown", Volume: 1, Pitch: pitches[m.Random().NextInt(int32(len(pitches)))]})
	}
}

// throwItemTowards ports BehaviorUtils.throwItem: the mob throws an item
// from its hand towards a target.
func throwItemTowards(m *Mob, stack item.Stack, target, velocity mgl64.Vec3, handBelowEye float64) {
	dir := target.Sub(m.data.Pos)
	if dir.LenSqr() > 0 {
		dir = dir.Normalize()
	}
	vel := mgl64.Vec3{dir[0] * velocity[0], dir[1] * velocity[1], dir[2] * velocity[2]}
	m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{m.data.Pos[0], m.EyeY() - handBelowEye, m.data.Pos[2]}, Velocity: vel}, stack))
}

// newAllayBrain ports AllayAi and the brain provider of Allay.
func newAllayBrain(b *allayBehaviour) *brain {
	br := newBrain(nearestLivingEntitySensor{}, playerSensor{}, hurtBySensor{}, nearestItemSensor{wants: func(m *Mob, s item.Stack) bool { return b.wantsToPickUp(m.tx, s) }})
	br.register(memLikedPlayer)
	br.register(memLikedNoteblockPosition)
	br.register(memLikedNoteblockCooldownTicks)
	br.addActivity(activityCore, 0,
		newSwim(.8),
		newAnimalPanic(2.5, nil, nil, nil),
		newLookAtTargetSink(45, 90),
		newMoveToTargetSink(150, 250, nil),
		newCountDownCooldownTicks(memLikedNoteblockCooldownTicks),
		newCountDownCooldownTicks(memItemPickupCooldownTicks),
	)
	hasWantedItem := func(m *Mob) bool { return m.brain().has(memNearestVisibleWantedItem) }
	br.addActivity(activityIdle, 0,
		newGoToWantedItem(nil, 1.75, true, 32, b.canPickUpLoot),
		newGoAndGiveItemsToTarget(b.depositPosition, 2.25, 20, b.throwItem, memItemPickupCooldownTicks, 60, func(*Mob) bool { return !b.inventory.Empty() }),
		newStayCloseToTarget(b.depositPosition, func(m *Mob) bool { return !hasWantedItem(m) }, 4, 16, 2.25),
		newSetEntityLookTargetSometimes(nil, 6, uniformInt{30, 60}),
		newRunOne(nil,
			weightedBehavior{newRandomFly(1), 2},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(1), 3), 2},
			weightedBehavior{newDoNothing(30, 60), 1},
		),
	)
	return br
}

// VibrationUser returns the allay as the user of its vibration listener.
func (a *allay) VibrationUser() world.VibrationUser { return allayVibrationUser{a} }

// VibrationData returns the vibration listener data of the allay.
func (a *allay) VibrationData() *world.VibrationData { return a.b().vibrations }

// VibrationListenerPosition returns the eyes of the allay, where its
// EntityPositionSource is.
func (a *allay) VibrationListenerPosition() mgl64.Vec3 {
	return a.Position().Add(mgl64.Vec3{0, a.H().Type().BBox(a).Height() * .85})
}

// allayVibrationUser ports Allay.VibrationUser: an allay hears note blocks
// within 16 blocks, the one it likes only once it likes one.
type allayVibrationUser struct{ a *allay }

func (allayVibrationUser) VibrationRadius() int { return 16 }
func (allayVibrationUser) ListensToGameEvent(e world.GameEvent) bool {
	return e == world.GameEventNoteBlockPlay
}
func (u allayVibrationUser) CanReceiveVibration(tx *world.Tx, _ cube.Pos, eventPos cube.Pos, _ world.GameEvent, _ world.GameEventContext) bool {
	b := u.a.b()
	if b.ai.NoAI {
		return false
	}
	if b.likedPos == nil {
		return true
	}
	return b.likedPos.dim == tx.World().Dimension() && distSqrPos(b.likedPos.pos, cube.PosFromVec3(u.a.Position())) < 1024*1024 && b.likedPos.pos == eventPos
}
func (u allayVibrationUser) ReceiveVibration(tx *world.Tx, _, eventPos cube.Pos, info world.VibrationInfo, _ float32) {
	if info.Event != world.GameEventNoteBlockPlay {
		return
	}
	b := u.a.b()
	b.hearNoteblock(b.ai.Mob(u.a.Ent, u.a), eventPos)
}
func (u allayVibrationUser) VibrationParticle(destination mgl64.Vec3, travelTicks int) world.Particle {
	return particle.VibrationSignal{Destination: destination, TravelTicks: travelTicks}
}

func (a *allay) HeldItems() (item.Stack, item.Stack) { return a.b().held, item.Stack{} }
func (a *allay) SetHeldItems(main, off item.Stack)   { a.setHeld(main) }

// setHeld changes the item the allay holds and shows viewers, which see it
// with MobEquipment as BDS sends it.
func (a *allay) setHeld(stack item.Stack) {
	a.b().held = stack
	a.updateState()
	viewEquipment(a.tx, a)
}

func (a *allay) Health() float64        { return a.b().health.Health() }
func (a *allay) MaxHealth() float64     { return a.b().health.MaxHealth() }
func (a *allay) SetMaxHealth(v float64) { a.b().health.SetMaxHealth(v) }
func (a *allay) Dead() bool             { return a.b().dead || a.Health() <= 0 }

// Hurt ports Allay.hurtServer: the player an allay likes cannot hurt it.
func (a *allay) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := a.b()
	if a.Dead() || d <= 0 {
		return 0, false
	}
	if killer := mobKiller(s); killer != nil && isPlayerEntity(killer) {
		if v, ok := b.br.get(memLikedPlayer); ok && v == killer.H().UUID() {
			return 0, false
		}
	}
	var ok bool
	if d, ok = b.ai.DamageCooldown(a.tx, d); !ok {
		return 0, false
	}
	if d, ok = mobEffectDamage(b.effects, d, s); !ok {
		return 0, false
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(s)
	a.viewAction(HurtAction{})
	onMobHurt(a.tx, a)
	if a.Dead() {
		a.die(s)
	}
	return d, true
}
func (a *allay) Heal(h float64, _ world.HealingSource) float64 {
	if a.Dead() || h <= 0 {
		return 0
	}
	old := a.Health()
	a.b().health.AddHealth(h)
	return a.Health() - old
}
func (a *allay) KnockBack(s mgl64.Vec3, f, h float64) {
	knockBack(a, s, f, h)
}
func (a *allay) AddEffect(e effect.Effect)  { a.b().effects.Add(e, a) }
func (a *allay) RemoveEffect(t effect.Type) { a.b().effects.Remove(t, a) }
func (a *allay) Effects() []effect.Effect   { return a.b().effects.Effects() }
func (a *allay) Speed() float64             { return a.b().speed }
func (a *allay) SetSpeed(v float64)         { a.b().speed = v }
func (a *allay) Dancing() bool              { return a.b().dancing }

// SetDancing ports Allay.setDancing: a panicking allay does not dance.
func (a *allay) SetDancing(v bool) {
	b := a.b()
	if b.ai.NoAI || v && a.Panicking() || b.dancing == v {
		return
	}
	b.dancing = v
	a.updateState()
}

// Panicking reports whether the allay flees from danger.
func (a *allay) Panicking() bool { return a.b().panicking() }

// SetJukeboxPlaying ports Allay.setJukeboxPlaying: an allay within earshot of
// a jukebox that starts playing dances, and stops when it stops.
func (a *allay) SetJukeboxPlaying(pos cube.Pos, playing bool) {
	b := a.b()
	if pos.Vec3Centre().Sub(a.VibrationListenerPosition()).LenSqr() > 10*10 {
		// JukeboxListener hears jukeboxes within the radius of
		// GameEvent.JUKEBOX_PLAY.
		return
	}
	if playing {
		if !b.dancing {
			b.jukebox = &pos
			a.SetDancing(true)
		}
	} else if b.jukebox == nil || *b.jukebox == pos {
		b.jukebox = nil
		a.SetDancing(false)
	}
}
func (a *allay) CanDuplicate() bool { return a.b().duplicationCooldown == 0 }

// AcceptItem ports Allay.mobInteract: a dancing allay given an amethyst
// shard duplicates, an allay without an item takes one from the player and
// likes them, and an allay with one gives it back with all it gathered.
func (a *allay) AcceptItem(from world.Entity, tx *world.Tx, ctx *item.UseContext) bool {
	u, ok := from.(item.User)
	if !ok {
		return false
	}
	main, off := u.HeldItems()
	stack := main
	offHand := false
	if stack.Empty() {
		stack = off
		offHand = true
	}
	b := a.b()
	if b.dancing && inItemTag(stack, "duplicates_allays") && a.CanDuplicate() {
		a.duplicate(tx)
		a.viewAction(LoveHeartsAction{})
		tx.PlaySound(a.Position(), sound.Custom{Name: "block.amethyst_block.chime", Volume: 2, Pitch: 1})
		ctx.SubtractFromCount(1)
		return true
	}
	if b.held.Empty() && !stack.Empty() {
		ctx.SubtractFromCount(1)
		tx.PlaySound(a.Position(), sound.Custom{Name: "entity.allay.item_given", Volume: 2, Pitch: 1})
		b.br.set(memLikedPlayer, from.H().UUID())
		a.setHeld(stack.Grow(1 - stack.Count()))
		return true
	}
	if !b.held.Empty() && !offHand && main.Empty() {
		held := b.held
		a.setHeld(item.Stack{})
		tx.PlaySound(a.Position(), sound.Custom{Name: "entity.allay.item_taken", Volume: 2, Pitch: 1})
		a.viewAction(SwingArmAction{})
		if !b.inventory.Empty() {
			m := b.ai.Mob(a.Ent, a)
			throwItemTowards(m, b.inventory, a.Position(), mgl64.Vec3{float64(float32(.3)), float64(float32(.3)), float64(float32(.3))}, float64(float32(.3)))
			b.inventory = item.Stack{}
		}
		b.br.erase(memLikedPlayer)
		ctx.NewItem = held
		ctx.ReplaceHeldItem = true
		return true
	}
	return false
}

// duplicate ports Allay.duplicateAllay.
func (a *allay) duplicate(tx *world.Tx) {
	tx.AddEntity(world.EntitySpawnOpts{Position: a.Position(), SpawnReason: "BREEDING", SpawnFinalized: true}.New(entityAllayType, allayBehaviourConfig{Cooldown: 6000}))
	a.b().duplicationCooldown = 6000
	a.updateState()
}

func (a *allay) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return a.AcceptItem(u, tx, ctx)
}

func (a *allay) viewAction(e world.EntityAction) {
	world.BroadcastEntityAction(a.tx, a, e)
}

// die ports Allay.dropEquipment: an allay drops what it gathered and what it
// holds.
func (a *allay) die(src world.DamageSource) {
	b := a.b()
	if b.dead {
		return
	}
	b.dead = true
	for _, s := range []item.Stack{b.inventory, b.held} {
		if !s.Empty() {
			a.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: a.Position()}, s))
		}
	}
	b.inventory, b.held = item.Stack{}, item.Stack{}
	a.viewAction(DeathAction{})
	dropAllDeathLoot(a.tx, a, src)
	_ = a.CloseIn(a.tx)
}
func (a *allay) DuplicationCooldown() time.Duration {
	return time.Duration(a.b().duplicationCooldown) * time.Second / 20
}

var _ Living = (*allay)(nil)
var _ item.Carrier = (*allay)(nil)
var _ ItemAcceptor = (*allay)(nil)
var _ world.EntityVibrationListener = (*allay)(nil)

func (b *allayBehaviour) effectManager() *EffectManager { return b.effects }
