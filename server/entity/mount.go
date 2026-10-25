package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/loot"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Mount species share the native equine inventory, taming and passenger rules;
// species identity remains a distinct EntityType and is never encoded as a kind tag.
type mountKind uint8

const mountContainerChest byte = 11
const (
	mountHorse mountKind = iota
	mountDonkey
	mountMule
	mountSkeletonHorse
	mountZombieHorse
	mountLlama
	mountTraderLlama
	mountCamel
	mountCamelHusk
)

// MountConfig configures a horse, donkey, mule, skeleton or zombie horse,
// llama, trader llama, camel or camel husk. Zero attributes take the
// species' defaults.
type MountConfig struct {
	Kind                        uint8
	Tamed, Saddled, Chested     bool
	SkeletonTrap                bool
	Strength, Variant, AgeTicks int
	Health, MaxHealth           float64
	JumpStrength, Speed         float64
}

// mountAttributes returns the Java attribute defaults of a species before
// finalizeSpawn randomises them: max health, movement speed and jump
// strength.
func mountAttributes(kind mountKind) (health, speed, jump float64) {
	switch kind {
	case mountHorse:
		return 53, .225, .7
	case mountDonkey, mountMule, mountLlama, mountTraderLlama:
		return 53, .175, .5
	case mountSkeletonHorse:
		return 15, .2, .7
	case mountZombieHorse:
		return 25, .225, .7
	default:
		return 32, .09, .42
	}
}

func (c MountConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c MountConfig) New() *MountBehaviour {
	kind := mountKind(c.Kind)
	health, speed, jump := mountAttributes(kind)
	if c.MaxHealth <= 0 {
		c.MaxHealth = health
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	if c.Speed <= 0 {
		c.Speed = speed
	}
	if c.JumpStrength <= 0 {
		c.JumpStrength = jump
	}
	b := &MountBehaviour{kind: kind, tamed: c.Tamed || kind == mountCamel || kind == mountCamelHusk, saddled: c.Saddled, chested: c.Chested, trap: c.SkeletonTrap, strength: max(1, min(c.Strength, 5)), variant: c.Variant, ageTicks: c.AgeTicks,
		jumpStrength: c.JumpStrength, movementSpeed: c.Speed, health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager()}
	if b.camel() {
		b.br = newCamelBrain(b)
	}
	return b
}

type MountBehaviour struct {
	kind                             mountKind
	tamed, saddled, chested, sitting bool
	strength, variant, ageTicks      int
	jumpStrength                     float64
	movementSpeed                    float64
	health                           *HealthManager
	effects                          *EffectManager
	passengers                       [2]*world.EntityHandle
	strafe, forward                  float64
	direction                        mgl64.Vec3
	dashCooldown                     int
	chestItems                       []item.Stack
	armor                            item.Stack
	chestInv                         *inventory.Inventory
	gearInv                          *inventory.Inventory
	trap                             bool
	trapTime                         int
	traderDespawn                    int
	lastPoseTick                     int64
	llamaState                       llamaState
	breeding                         breeding
	animalAI
	// br is the brain of camels.
	br *Brain
	// temper, owner and bred are AbstractHorse's Temper, Owner and Bred.
	temper int
	owner  string
	bred   bool
	// eating and standCounter are the eating and standing flags of
	// AbstractHorse.
	eating                      bool
	eatingCounter, standCounter int
}

func (b *MountBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	m := &Mount{Ent: e}
	if b.passengers[0] == nil || (b.camel() && b.passengers[1] == nil) {
		b.restorePassengers(e, tx)
	}
	if b.dashCooldown > 0 {
		b.dashCooldown--
	}
	if b.camel() && b.sitting {
		if _, inWater := tx.Liquid(cube.PosFromVec3(e.Position())); inWater {
			// Camel.tick: a camel in water stands up at once.
			b.standUpInstantly(m)
		}
	}
	b.effects.Tick(m, tx)
	if b.kind == mountZombieHorse && sunBurnTick(e, tx) && b.armor.Empty() {
		// Mob.burnUndead: the armour a zombie horse wears on its body,
		// ZombieHorse.sunProtectionSlot, shades it.
		if e.OnFireDuration() < 8*time.Second {
			e.SetOnFire(8 * time.Second)
		}
	}
	if b.kind == mountSkeletonHorse && b.trap {
		// SkeletonHorse.aiStep: an unsprung trap leaves after 15 minutes.
		if b.trapTime >= 18000 {
			_ = e.CloseIn(tx)
			return nil
		}
		b.trapTime++
	}
	if b.kind == mountTraderLlama && b.traderLlamaDespawn(e, tx) {
		return nil
	}
	ai := &b.ai
	ai.MovementSpeed, ai.JumpStrength, ai.StepHeight = b.movementSpeed, b.jumpStrength, 1
	if b.camel() {
		ai.StepHeight = 1.5
	}
	// The eyes of every species sit at 0.95 times its height.
	ai.EyeHeight = .95 * e.H().Type().BBox(m).Height()
	mv := animalTick(e, tx, m, &b.animalAI, b.registerGoals)
	b.tickHorse(m, tx)
	b.updatePassengers(e, tx)
	return mv
}
func (b *MountBehaviour) updatePassengers(e *Ent, tx *world.Tx) {
	for i, h := range b.passengers {
		if h == nil {
			continue
		}
		p, ok := h.Entity(tx)
		if !ok {
			b.passengers[i] = nil
			continue
		}
		r, ok := p.(Rider)
		if !ok || r.Riding() != e.H() {
			b.passengers[i] = nil
			continue
		}
		offset := 1.35
		seat := mgl64.Vec3{}
		if b.camel() {
			offset = 2.05
			if i == 1 {
				seat[0] = .45
			} else {
				seat[0] = -.45
			}
		}
		if b.llama() {
			offset = 1.55
		}
		seat[1] = offset
		if m, ok := p.(interface{ Teleport(mgl64.Vec3) }); ok {
			m.Teleport(e.Position().Add(seat))
		}
	}
}
func (b *MountBehaviour) restorePassengers(e *Ent, tx *world.Tx) {
	// Entity.Riding is persisted on the passenger; rebuilding local seat links
	// from that authoritative relationship restores the mount after reload.
	box := cube.Box(-2, 0, -2, 2, 3, 2).Translate(e.Position())
	for rider := range tx.EntitiesWithin(box) {
		r, ok := rider.(Rider)
		if !ok || r.Riding() != e.H() {
			continue
		}
		if b.passengers[0] == nil {
			b.passengers[0] = rider.H()
		} else if b.passengers[1] == nil && rider.H() != b.passengers[0] {
			b.passengers[1] = rider.H()
		}
	}
}

// leashedTrader returns the wandering trader whose lead holds the mob.
func leashedTrader(e *Ent, tx *world.Tx) (*WanderingTrader, bool) {
	id := e.HolderLeashID()
	if id == 0 {
		return nil, false
	}
	for candidate := range tx.Entities() {
		if candidate.H().UniqueID() == id {
			trader, ok := candidate.(*WanderingTrader)
			return trader, ok
		}
	}
	return nil, false
}

// traderLlamaDespawn ports TraderLlama.maybeDespawn: an untamed, unnamed
// trader llama that no player rides and that is free or on its trader's
// lead leaves together with its trader, or 47999 ticks after it lost it.
func (b *MountBehaviour) traderLlamaDespawn(e *Ent, tx *world.Tx) bool {
	trader, leashedToTrader := leashedTrader(e, tx)
	if b.tamed || e.data.Name != "" || e.HolderLeashID() != 0 && !leashedToTrader || b.riddenByPlayer() {
		return false
	}
	if b.traderDespawn == 0 {
		b.traderDespawn = 47999
	}
	if leashedToTrader {
		b.traderDespawn = trader.DespawnDelay() - 1
	} else {
		b.traderDespawn--
	}
	if b.traderDespawn > 0 {
		return false
	}
	e.DetachLeash(tx, false)
	_ = e.CloseIn(tx)
	return true
}

func (b *MountBehaviour) riddenByPlayer() bool {
	return b.passengers[0] != nil && b.passengers[0].Type().EncodeEntity() == "minecraft:player"
}

func (b *MountBehaviour) DismountAll(e *Ent, tx *world.Tx) {
	m := &Mount{Ent: e}
	for i, h := range b.passengers {
		if h != nil {
			if p, ok := h.Entity(tx); ok {
				if r, ok := p.(Rider); ok {
					r.SetRiding(nil)
				}
			}
			m.broadcastLink(tx, h, false)
			b.passengers[i] = nil
		}
	}
	m.updateState()
}

type Mount struct{ *Ent }
type Horse struct{ *Mount }
type Donkey struct{ *Mount }
type Mule struct{ *Mount }
type SkeletonHorse struct{ *Mount }
type ZombieHorse struct{ *Mount }
type Llama struct{ *Mount }
type TraderLlama struct{ *Mount }
type Camel struct{ *Mount }
type CamelHusk struct{ *Mount }

func (m *Mount) b() *MountBehaviour { return m.Behaviour().(*MountBehaviour) }

// updateState shows viewers the mount's flags and inventory.
func (m *Mount) updateState() {
	viewers := m.tx.Viewers(m.Position())
	for _, v := range viewers {
		v.ViewEntityState(m)
	}
	m.tx.ReleaseViewers(viewers)
}
func (m *Mount) SetVehicleInput(strafe, forward float64) {
	b := m.b()
	b.strafe = strafe
	b.forward = forward
	b.direction = m.Rotation().Vec3()
}
func (m *Mount) SetVehicleInputFrom(passenger world.Entity, strafe, forward float64) {
	m.SetVehicleInput(strafe, forward)
	if passenger != nil {
		m.b().direction = passenger.Rotation().Vec3()
	}
}
func (m *Mount) ControlsVehicle(passenger world.Entity) bool {
	b := m.b()
	return passenger != nil && b.passengers[0] != nil && passenger.H() == b.passengers[0] && b.controlled()
}
func (m *Mount) SetJumpStrength(v float64) {
	b := m.b()
	if m.b().saddled {
		b.jumpStrength = v
	}
}

// JumpStrength returns the horse jump strength attribute.
func (m *Mount) JumpStrength() float64 { return m.b().jumpStrength }

// Jump ports AbstractHorse.executeRidersJump at full charge: a saddled horse
// on the ground jumps with its jump strength and leaps forward if its rider
// moves forward.
func (m *Mount) Jump() bool {
	b := m.b()
	if !b.saddled || b.dashCooldown > 0 || b.sitting || !b.ai.onGround {
		return false
	}
	mob := b.ai.Mob(m.Ent, m)
	v := m.Velocity()
	v[1] = mountJumpPower(mob, b.jumpStrength, 1)
	if b.forward > 0 {
		sin, cos := yawSinCos(m.Rotation().Yaw())
		v[0] += -.4 * sin
		v[2] += .4 * cos
	}
	m.SetVelocity(v)
	return true
}
func (m *Mount) RideJump() bool {
	if m.b().camel() {
		return true
	}
	return m.Jump()
}

// Dash ports Camel.executeRidersJump at full charge: a saddled camel on the
// ground leaps forwards the way it looks and cannot dash again for 55 ticks.
func (m *Mount) Dash(_ *world.Tx, passenger world.Entity) bool {
	b := m.b()
	if passenger == nil || !m.ControlsVehicle(passenger) || !b.camel() || b.dashCooldown > 0 || b.refuseToMove(m.tx) || !b.ai.onGround {
		return false
	}
	mob := b.ai.Mob(m.Ent, m)
	look := m.Rotation().Vec3()
	look[1] = 0
	if look.Len() > 0 {
		look = look.Normalize()
	}
	m.SetVelocity(m.Velocity().Add(look.Mul(22.2222 * b.movementSpeed * blockSpeedFactor(mob))).Add(mgl64.Vec3{0, 1.4285 * mountJumpPower(mob, b.jumpStrength, 1)}))
	b.dashCooldown = 55
	return true
}

// steed returns the mount itself. Horses, donkeys, camels and the other
// species embed *Mount in a field named Mount, which hides the Mount method;
// steed reaches it through them.
func (m *Mount) steed() *Mount { return m }

// MountVehicle mounts passenger on vehicle, reporting whether it could. It
// reaches the Mount method of every vehicle, including horse species.
func MountVehicle(tx *world.Tx, vehicle, passenger world.Entity) bool {
	if s, ok := vehicle.(interface{ steed() *Mount }); ok {
		return s.steed().Mount(tx, passenger)
	}
	m, ok := vehicle.(interface {
		Mount(*world.Tx, world.Entity) bool
	})
	return ok && m.Mount(tx, passenger)
}

// Mount ports AbstractHorse.doPlayerRide: the passenger mounts even an
// untamed horse, which stops eating and rearing. Camels carry two.
func (m *Mount) Mount(tx *world.Tx, passenger world.Entity) bool {
	b := m.b()
	if passenger == nil {
		return false
	}
	r, ok := passenger.(Rider)
	if !ok || r.Riding() != nil {
		return false
	}
	seat := 0
	if b.passengers[0] != nil {
		if !b.camel() || b.passengers[1] != nil {
			return false
		}
		seat = 1
	}
	b.eating, b.eatingCounter, b.standCounter = false, 0, 0
	b.passengers[seat] = passenger.H()
	seatRider(r, m)
	m.broadcastLink(tx, passenger.H(), true)
	tx.PlaySound(m.Position(), sound.NativeEvent{SoundType: "mount", EntityType: m.H().Type().EncodeEntity(), ExtraData: -1})
	m.updateState()
	return true
}

// addMobPassenger seats a mob that starts riding the mount, as
// Entity.startRiding does without the player mounting a mount does.
func (m *Mount) addMobPassenger(tx *world.Tx, passenger world.Entity, announce bool) bool {
	b := m.b()
	r, ok := passenger.(Rider)
	if !ok {
		return false
	}
	seat := 0
	if b.passengers[0] != nil {
		if !b.camel() || b.passengers[1] != nil {
			return false
		}
		seat = 1
	}
	b.passengers[seat] = passenger.H()
	seatRider(r, m)
	if announce {
		m.broadcastLink(tx, passenger.H(), true)
	}
	m.updateState()
	return true
}

// RiderSeat returns the seat of the species' Bedrock rideable component:
// camels seat their second rider behind the first.
func (m *Mount) RiderSeat(rider world.Entity) world.RiderSeat {
	b := m.b()
	pos := mgl64.Vec3{0, 1.1, -0.2}
	switch b.kind {
	case mountDonkey:
		pos[1] = 0.925
	case mountMule:
		pos[1] = 0.975
	case mountLlama, mountTraderLlama:
		pos = mgl64.Vec3{0, 1.17, -0.3}
	case mountCamel, mountCamelHusk:
		pos = mgl64.Vec3{0, 1.905, 0.5}
		if b.passengers[1] == rider.H() {
			pos[2] = -0.5
		}
	}
	return seat{pos: pos}.riderSeat(rider)
}
func (m *Mount) Dismount(tx *world.Tx, passenger world.Entity) {
	b := m.b()
	for i, h := range b.passengers {
		if h != nil && passenger != nil && h == passenger.H() {
			b.passengers[i] = nil
			if r, ok := passenger.(Rider); ok {
				r.SetRiding(nil)
			}
			m.broadcastLink(tx, h, false)
		}
	}
	m.updateState()
}
func (m *Mount) broadcastLink(tx *world.Tx, h *world.EntityHandle, link bool) {
	linkType := LinkPassenger
	if m.b().passengers[0] == h {
		linkType = LinkRider
	}
	viewers := tx.Viewers(m.Position())
	for _, v := range viewers {
		if lv, ok := v.(interface {
			ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool)
		}); ok {
			lv.ViewEntityLinkType(m.H(), h, linkType, link)
		} else if lv, ok := v.(interface {
			ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool)
		}); ok {
			lv.ViewEntityLink(m.H(), h, link)
		}
	}
	tx.ReleaseViewers(viewers)
}

// Interact ports mobInteract of the horse species.
func (m *Mount) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if user == nil {
		return false
	}
	b := m.b()
	held, _ := user.HeldItems()
	sneaking := false
	if s, ok := user.(interface{ Sneaking() bool }); ok {
		sneaking = s.Sneaking()
	}
	baby := b.ageTicks < 0
	switch b.kind {
	case mountSkeletonHorse:
		// SkeletonHorse.mobInteract: untamed skeleton horses ignore players.
		return b.tamed && m.interactAbstractHorse(tx, user, ctx, held, sneaking)
	case mountCamel, mountCamelHusk:
		return m.interactCamel(tx, user, ctx, held, sneaking)
	}
	// Horse, AbstractChestedHorse and ZombieHorse.mobInteract.
	if !b.vehicle() && !(!baby && b.tamed && sneaking) && (!baby || !itemNamed(held, "minecraft:golden_dandelion")) && !held.Empty() {
		if m.breedingFood(held) {
			return m.fedFood(user, held, ctx)
		}
		if !b.tamed {
			b.makeMad(m)
			return true
		}
		if _, chest := held.Item().(block.Chest); chest && !b.chested && (b.kind == mountDonkey || b.kind == mountMule || b.llama()) {
			b.chested = true
			b.chestItems = make([]item.Stack, b.chestSize())
			b.chestInv = nil
			ctx.SubtractFromCount(1)
			tx.PlaySound(m.Position(), sound.Custom{Name: "mob.horse.armor", Volume: 1, Pitch: 1})
			m.updateState()
			return true
		}
	}
	return m.interactAbstractHorse(tx, user, ctx, held, sneaking)
}

// interactAbstractHorse ports AbstractHorse.mobInteract: a tamed adult opens
// its inventory to a sneaking player and takes a saddle or body armour, and
// otherwise the player gets on.
func (m *Mount) interactAbstractHorse(tx *world.Tx, user item.User, ctx *item.UseContext, held item.Stack, sneaking bool) bool {
	b := m.b()
	if b.vehicle() || b.ageTicks < 0 {
		return feedBreeder(m, user, ctx)
	}
	if b.tamed && sneaking {
		if opener, ok := user.(ContainerOpener); ok {
			if b.chested {
				opener.OpenEntityContainer(m, b.chestInventory(), mountContainerChest, tx)
			} else {
				opener.OpenEntityContainer(m, m.gearInventory(), mountContainerChest, tx)
			}
			return true
		}
	}
	if !held.Empty() && m.equip(tx, held, ctx) {
		return true
	}
	return m.doPlayerRide(tx, user)
}

// interactCamel ports Camel.mobInteract.
func (m *Mount) interactCamel(tx *world.Tx, user item.User, ctx *item.UseContext, held item.Stack, sneaking bool) bool {
	b := m.b()
	baby := b.ageTicks < 0
	if sneaking && !baby {
		if opener, ok := user.(ContainerOpener); ok {
			opener.OpenEntityContainer(m, m.gearInventory(), mountContainerChest, tx)
			return true
		}
	}
	if !held.Empty() && m.equip(tx, held, ctx) {
		return true
	}
	if m.breedingFood(held) {
		return m.fedFood(user, held, ctx)
	}
	if (b.passengers[0] == nil || b.passengers[1] == nil) && !baby {
		return m.doPlayerRide(tx, user)
	}
	return baby && itemNamed(held, "minecraft:golden_dandelion") && feedBreeder(m, user, ctx)
}

// fedFood ports AbstractHorse.fedFood: food the mount eats is used up.
func (m *Mount) fedFood(user item.User, held item.Stack, ctx *item.UseContext) bool {
	if !m.b().handleEating(m, user, held) {
		return false
	}
	if ctx != nil {
		ctx.SubtractFromCount(1)
	}
	return true
}

// doPlayerRide gets the user on the mount, unless it is a trader llama its
// trader leads.
func (m *Mount) doPlayerRide(tx *world.Tx, user item.User) bool {
	if _, led := leashedTrader(m.Ent, tx); led && m.b().kind == mountTraderLlama {
		return false
	}
	return m.Mount(tx, user)
}

// equip ports the saddle and body armour Equippable components: a tamed
// adult takes a saddle, a horse or zombie horse horse armour and a llama a
// carpet.
func (m *Mount) equip(tx *world.Tx, held item.Stack, ctx *item.UseContext) bool {
	b := m.b()
	if !b.tamed || b.ageTicks < 0 {
		return false
	}
	if _, saddle := held.Item().(item.Saddle); saddle {
		if b.saddled || b.llama() {
			return false
		}
		b.saddled = true
		if b.gearInv != nil {
			_ = b.gearInv.SetItem(0, item.NewStack(item.Saddle{}, 1))
		}
		ctx.SubtractFromCount(1)
		tx.PlaySound(m.Position(), sound.NativeEvent{SoundType: "saddle", EntityType: m.H().Type().EncodeEntity(), ExtraData: -1})
		m.updateState()
		return true
	}
	if !b.armor.Empty() || !b.wearable(held) {
		return false
	}
	b.armor = held.Grow(1 - held.Count())
	if b.gearInv != nil {
		_ = b.gearInv.SetItem(1, b.armor)
	}
	ctx.SubtractFromCount(1)
	tx.PlaySound(m.Position(), sound.NativeEvent{SoundType: "armor", EntityType: m.H().Type().EncodeEntity(), ExtraData: -1})
	m.viewArmour(tx)
	return true
}

// wearable reports whether the species wears the stack as body armour.
func (b *MountBehaviour) wearable(s item.Stack) bool {
	switch {
	case b.kind == mountHorse || b.kind == mountZombieHorse:
		return mountArmor(s)
	case b.llama():
		_, carpet := s.Item().(block.Carpet)
		return carpet
	}
	return false
}

// breedingFood ports isFood of the horse species: the food tag each eats.
func (m *Mount) breedingFood(stack item.Stack) bool {
	switch m.b().kind {
	case mountZombieHorse:
		return itemNamed(stack, "minecraft:red_mushroom")
	case mountLlama, mountTraderLlama:
		return itemNamed(stack, "minecraft:wheat", "minecraft:hay_block")
	case mountCamel:
		return itemNamed(stack, "minecraft:cactus")
	case mountCamelHusk:
		return itemNamed(stack, "minecraft:rabbit_foot")
	}
	return itemNamed(stack, "minecraft:wheat", "minecraft:sugar", "minecraft:hay_block", "minecraft:apple", "minecraft:carrot", "minecraft:golden_carrot", "minecraft:golden_apple", "minecraft:enchanted_golden_apple")
}

// InLove reports whether the mount is in love, looking for a partner.
func (m *Mount) InLove() bool             { return m.b().breeding.InLove() }
func (m *Mount) currentTx() *world.Tx     { return m.tx }
func (m *Mount) breedingState() *breeding { return &m.b().breeding }
func (m *Mount) ageTicks() *int           { return &m.b().ageTicks }
func (m *Mount) Panicking() bool {
	if b := m.b(); b.br != nil {
		return b.br.has(memIsPanicking)
	}
	return m.b().panicking()
}
func mountTypeFor(kind mountKind) world.EntityType {
	switch kind {
	case mountHorse:
		return HorseType
	case mountDonkey:
		return DonkeyType
	case mountMule:
		return MuleType
	case mountSkeletonHorse:
		return SkeletonHorseType
	case mountZombieHorse:
		return ZombieHorseType
	case mountLlama:
		return LlamaType
	case mountTraderLlama:
		return TraderLlamaType
	case mountCamel:
		return CamelType
	default:
		return CamelHuskType
	}
}
func mountFromEntity(e world.Entity) *Mount {
	if m, ok := e.(interface{ mount() *Mount }); ok {
		return m.mount()
	}
	return nil
}

// skeletonTrapGoal ports SkeletonTrapGoal: a skeleton trap horse springs
// once a living player comes within ten blocks.
type skeletonTrapGoal struct {
	goalFlags
	mount *MountBehaviour
}

func (g *skeletonTrapGoal) CanUse(m *Mob) bool {
	return g.mount.trap && nearbyAlivePlayer(m.tx, m.data.Pos, 10)
}

func (g *skeletonTrapGoal) Tick(m *Mob) { g.mount.springSkeletonTrap(m) }

// nearbyAlivePlayer ports Level.hasNearbyAlivePlayer.
func nearbyAlivePlayer(tx *world.Tx, pos mgl64.Vec3, dist float64) bool {
	for p := range tx.Players() {
		if !spectator(p) && entityAlive(p) && p.Position().Sub(pos).LenSqr() < dist*dist {
			return true
		}
	}
	return false
}

// springSkeletonTrap ports SkeletonTrapGoal.tick: the horse is tamed and,
// under a lightning bolt that only flashes, it and three more skeleton
// horses spawn ridden by armoured skeletons with enchanted gear.
func (b *MountBehaviour) springSkeletonTrap(m *Mob) {
	tx := m.tx
	difficulty := tx.LocalDifficulty(m.BlockPos())
	b.trap, b.tamed, b.ageTicks = false, true, 0
	pos := m.data.Pos
	tx.AddEntity(NewVisualLightning(world.EntitySpawnOpts{Position: pos}))
	skeleton := trapSkeleton(tx, difficulty, pos)
	if skeleton == nil {
		return
	}
	mobStartRiding(tx, skeleton, m.Self, true)
	r := m.Random()
	for range 3 {
		horse, ok := tx.AddEntity(NewSkeletonHorse(world.EntitySpawnOpts{Position: pos, SpawnReason: "triggered"})).(*SkeletonHorse)
		if !ok {
			continue
		}
		horse.b().tamed, horse.b().ageTicks = true, 0
		horse.SetPersistenceRequired(true)
		if ai, _ := horse.Ent.mobAI(); ai != nil {
			ai.setInvulnerableTime(tx, 60)
		}
		rider := trapSkeleton(tx, difficulty, pos)
		if rider == nil {
			continue
		}
		mobStartRiding(tx, rider, horse, true)
		// RandomSource.triangle(0, 1.1485) on both axes.
		horse.SetVelocity(horse.Velocity().Add(mgl64.Vec3{1.1485 * (r.NextDouble() - r.NextDouble()), 0, 1.1485 * (r.NextDouble() - r.NextDouble())}))
	}
}

// trapSkeleton ports SkeletonTrapGoal.createSkeleton: a skeleton in an iron
// helmet unless it wears another, whose weapon and helmet are enchanted as
// mob spawn equipment.
func trapSkeleton(tx *world.Tx, difficulty world.LocalDifficulty, pos mgl64.Vec3) *Skeleton {
	skeleton, ok := tx.AddEntity(NewSkeleton(world.EntitySpawnOpts{Position: pos, SpawnReason: "triggered"})).(*Skeleton)
	if !ok {
		return nil
	}
	skeleton.SetPersistenceRequired(true)
	if ai, _ := skeleton.Ent.mobAI(); ai != nil {
		ai.setInvulnerableTime(tx, 60)
	}
	s := skeleton.state()
	if s.head.Empty() {
		s.head = item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1)
	}
	m, _ := mobOf(skeleton)
	enchant := func(stack item.Stack) item.Stack {
		if stack.Empty() || m == nil {
			return stack
		}
		var types []item.EnchantmentType
		for _, e := range stack.Enchantments() {
			types = append(types, e.Type())
		}
		enchanted, err := loot.EnchantMobSpawnEquipment(stack.WithoutEnchantments(types...), difficulty.SpecialMultiplier(), m.Random())
		if err != nil {
			return stack
		}
		return enchanted
	}
	s.main, s.head = enchant(s.main), enchant(s.head)
	viewers := tx.Viewers(pos)
	for _, v := range viewers {
		v.ViewEntityItems(skeleton)
		v.ViewEntityArmour(skeleton)
	}
	tx.ReleaseViewers(viewers)
	return skeleton
}

func mountArmor(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	switch s.Item().(type) {
	case item.LeatherHorseArmour, item.CopperHorseArmour, item.IronHorseArmour, item.GoldenHorseArmour, item.DiamondHorseArmour, item.NetheriteHorseArmour:
		return true
	default:
		return false
	}
}
func (m *Mount) Trap() bool { return m.b().trap }
func (m *Mount) SetTrap(v bool) {
	if m.b().kind == mountSkeletonHorse {
		m.b().trap = v
		if v {
			m.b().trapTime = 0
		}
	}
}
func (m *Mount) Armor() item.Stack { return m.b().armor }
func (m *Mount) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	return item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{}
}

// BodyItem returns the horse armour or llama carpet the mount wears.
func (m *Mount) BodyItem() item.Stack { return m.b().armor }
func (m *Mount) viewArmour(tx *world.Tx) {
	viewers := tx.Viewers(m.Position())
	for _, viewer := range viewers {
		viewer.ViewEntityArmour(m)
	}
	tx.ReleaseViewers(viewers)
}
func (m *Mount) Inventory() *inventory.Inventory { return m.b().chestInventory() }
func (m *Mount) InventorySize() int              { return len(m.b().chestItems) }
func (m *Mount) InventoryItem(slot int) (item.Stack, bool) {
	b := m.b()
	if slot < 0 || slot >= len(b.chestItems) {
		return item.Stack{}, false
	}
	return b.chestItems[slot], true
}
func (m *Mount) SetInventoryItem(slot int, s item.Stack) bool {
	b := m.b()
	if slot < 0 || slot >= len(b.chestItems) {
		return false
	}
	return b.chestInventory().SetItem(slot, s) == nil
}
func (m *Mount) gearInventory() *inventory.Inventory {
	b := m.b()
	if b.gearInv == nil {
		b.gearInv = inventory.New(2, func(slot int, _, after item.Stack) {
			if slot == 0 {
				b.saddled = !after.Empty()
			} else if slot == 1 {
				b.armor = after
			}
		})
		b.gearInv.SlotValidatorFunc(func(s item.Stack, slot int) bool {
			if s.Empty() {
				return true
			}
			if slot == 0 {
				_, ok := s.Item().(item.Saddle)
				return ok && !b.llama()
			}
			return b.wearable(s)
		})
		if b.saddled {
			_ = b.gearInv.SetItem(0, item.NewStack(item.Saddle{}, 1))
		}
		if !b.armor.Empty() {
			_ = b.gearInv.SetItem(1, b.armor)
		}
	}
	return b.gearInv
}
func (m *Mount) CanEquipHarness(world.Entity) bool                                 { return false }
func (m *Mount) EquipHarness(_ *world.Tx, _ world.Entity, harness world.Item) bool { return false }
func (m *Mount) Tamed() bool                                                       { return m.b().tamed }
func (m *Mount) Saddled() bool                                                     { return m.b().saddled }
func (m *Mount) Chested() bool                                                     { return m.b().chested }
func (m *Mount) Strength() int                                                     { return m.b().strength }

// Temper returns how used to riders an untamed horse is.
func (m *Mount) Temper() int { return m.b().temper }

// Variant returns the colour of a horse or llama.
func (m *Mount) Variant() int32 { return int32(m.b().variant & 0xff) }

// MarkVariant returns the markings of a horse.
func (m *Mount) MarkVariant() int32 {
	if m.b().kind != mountHorse {
		return 0
	}
	return int32(m.b().variant >> 8 & 0xff)
}
func (m *Mount) Sitting() bool { return m.b().sitting }

func (m *Mount) moveFlagHeld() bool { return m.b().sitting }

// SetSitting makes a camel sit down or stand up.
func (m *Mount) SetSitting(v bool) {
	switch b := m.b(); {
	case !b.camel():
	case v:
		b.sitDown(m)
	default:
		b.standUp(m)
	}
}
func (m *Mount) AgeTicks() int { return m.b().ageTicks }

// MountState is the state Bedrock clients show of a mount.
type MountState struct {
	Saddled, Tamed, Chested, Sitting bool
	// Controlled is set on a saddled mount a player can steer, which can
	// then power jump, or dash for camels.
	Controlled, PowerJump, Dash bool
	// Rearing is set for the species that rear and eat grass, whose
	// eating and standing flags HorseFlags holds.
	Rearing    bool
	HorseFlags int64
	// ContainerSize is the size of the horse inventory of a tamed mount.
	ContainerSize, StrengthModifier int32
	// Strength and MaxStrength are the strength of a llama.
	Strength, MaxStrength int32
}

// MountState returns the state Bedrock clients show of the mount.
func (m *Mount) MountState() MountState {
	b := m.b()
	s := MountState{Saddled: b.saddled, Tamed: b.tamed, Chested: b.chested, Sitting: b.sitting, Controlled: b.saddled, Rearing: b.horseRearing()}
	if s.Controlled {
		s.PowerJump, s.Dash = !b.camel(), b.camel()
	}
	if b.eating {
		s.HorseFlags |= horseFlagEating
	}
	if b.standCounter > 0 {
		s.HorseFlags |= horseFlagStanding
	}
	if b.tamed {
		switch {
		case b.kind == mountHorse || b.kind == mountSkeletonHorse || b.kind == mountZombieHorse:
			s.ContainerSize = 2
		case b.camel():
			s.ContainerSize = 5
		default:
			s.ContainerSize = 16
		}
		if b.llama() {
			s.StrengthModifier = 3
		}
	}
	if b.llama() {
		s.Strength, s.MaxStrength = int32(b.strength), 5
	}
	return s
}
func (b *MountBehaviour) chestInventory() *inventory.Inventory {
	if b.chestInv == nil && b.chested {
		size := max(b.chestSize(), len(b.chestItems), 1)
		if len(b.chestItems) < size {
			b.chestItems = append(b.chestItems, make([]item.Stack, size-len(b.chestItems))...)
		}
		b.chestInv = inventory.New(size, func(slot int, _, after item.Stack) { b.chestItems[slot] = after })
		for slot, s := range b.chestItems {
			_ = b.chestInv.SetItem(slot, s)
		}
	}
	return b.chestInv
}

// chestSize ports AbstractChestedHorse.getInventoryColumns: llamas carry three
// slots per strength, donkeys and mules five columns of three.
func (b *MountBehaviour) chestSize() int {
	if b.llama() {
		return b.strength * 3
	}
	return 15
}

func mountSpecies(kind mountKind, native string, width, height float64) world.EntityType {
	return mountType{kind: kind, native: native, width: width, height: height}
}

type mountType struct {
	kind          mountKind
	native        string
	width, height float64
}

func (t mountType) EncodeEntity() string { return "minecraft:" + t.native }
func (t mountType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		defaultMountConfig(t.kind).Apply(d)
	}
	m := &Mount{Ent: &Ent{tx: tx, handle: h, data: d}}
	switch t.kind {
	case mountHorse:
		return &Horse{m}
	case mountDonkey:
		return &Donkey{m}
	case mountMule:
		return &Mule{m}
	case mountSkeletonHorse:
		return &SkeletonHorse{m}
	case mountZombieHorse:
		return &ZombieHorse{m}
	case mountLlama:
		return &Llama{m}
	case mountTraderLlama:
		return &TraderLlama{m}
	case mountCamel:
		return &Camel{m}
	default:
		return &CamelHusk{m}
	}
}

// BBox returns the box of the species, scaled down for young: foals of
// horses to 70%, of donkeys, mules and llamas to half and camel calves to
// 0.95 by 1.4 blocks. Sitting camels are 1.43 blocks lower.
func (t mountType) BBox(e world.Entity) cube.BBox {
	w, h := t.width, t.height
	if m := mountFromEntity(e); m != nil {
		b := m.b()
		baby := b.ageTicks < 0
		switch {
		case t.kind == mountCamel || t.kind == mountCamelHusk:
			switch {
			case baby && b.sitting:
				w, h = .95, .425
			case baby:
				w, h = .95, 1.4
			case b.sitting:
				h -= 1.43
			}
		case baby && (t.kind == mountDonkey || t.kind == mountMule || t.kind == mountLlama || t.kind == mountTraderLlama):
			w, h = w*.5, h*.5
		case baby:
			w, h = w*.7, h*.7
		}
	}
	return cube.Box(-w/2, 0, -w/2, w/2, h, w/2)
}
func (t mountType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := defaultMountConfig(t.kind)
	state := ReadNativeMobJavaState(n)
	if state == nil {
		state = n
	}
	if _, ok := state["Tame"]; ok {
		c.Tamed = nbtconv.Bool(state, "Tame")
	} else if _, ok := state["Tamed"]; ok {
		c.Tamed = nbtconv.Bool(state, "Tamed")
	}
	if _, ok := state["Saddled"]; ok {
		c.Saddled = nbtconv.Bool(state, "Saddled")
	}
	if _, ok := state["ChestedHorse"]; ok {
		c.Chested = nbtconv.Bool(state, "ChestedHorse")
	} else if _, ok := state["Chested"]; ok {
		c.Chested = nbtconv.Bool(state, "Chested")
	}
	if _, ok := state["Strength"]; ok {
		c.Strength = int(nbtconv.Int32(state, "Strength"))
	}
	c.Variant = int(nbtconv.Int32(state, "Variant"))
	c.AgeTicks = int(nbtconv.Int32(state, "Age"))
	if _, ok := state["Health"]; ok {
		c.Health = float64(nbtconv.Float32(state, "Health"))
	}
	if b, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.MaxHealth = b.Base
		if c.Health == 0 {
			c.Health = b.Current
		}
	}
	if speed, ok := nativeMobAttribute(n, "minecraft:movement"); ok {
		c.Speed = speed.Base
	}
	if jump, ok := nativeMobAttribute(n, "minecraft:horse.jump_strength"); ok {
		c.JumpStrength = jump.Base
	}
	d.Data = c.New()
	b := d.Data.(*MountBehaviour)
	// AbstractHorse.finalizeSpawn replaces the max-health base after loading
	// Health; Source does not clamp that already-loaded health a second time.
	if _, loadedHealth := state["Health"]; loadedHealth {
		b.health.health = c.Health
	}
	b.sitting = nbtconv.Bool(state, "Sitting")
	b.lastPoseTick = nbtconv.Int64(state, "LastPoseTick")
	b.temper = int(nbtconv.Int32(state, "Temper"))
	b.bred = nbtconv.Bool(state, "Bred")
	b.eating = nbtconv.Bool(state, "EatingHaystack")
	b.owner = javaUUIDString(state["Owner"])
	b.trap = nbtconv.Bool(state, "SkeletonTrap")
	b.trapTime = int(nbtconv.Int32(state, "SkeletonTrapTime"))
	if _, ok := state["DespawnDelay"]; ok {
		b.traderDespawn = int(nbtconv.Int32(state, "DespawnDelay"))
	}
	b.armor = mountStackFromNative(ReadNativeMobEquipment(n).Armor)
	if b.chested {
		b.chestItems = mountStacksFromNative(ReadNativeMobEquipment(n).ChestItems)
	}
	b.breeding.decodeNBT(state)
}
func defaultMountConfig(kind mountKind) MountConfig {
	return MountConfig{Kind: uint8(kind), Strength: 1}
}
func (t mountType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*MountBehaviour)
	native := map[string]any{}
	native["Attributes"] = EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.health.MaxHealth(), Current: b.health.Health(), DefaultMin: 0, DefaultMax: b.health.MaxHealth(), Min: 0, Max: b.health.MaxHealth()}, {Name: "minecraft:movement", Base: b.movementSpeed, Current: b.movementSpeed, DefaultMin: 0, DefaultMax: b.movementSpeed, Min: 0, Max: b.movementSpeed}, {Name: "minecraft:horse.jump_strength", Base: b.jumpStrength, Current: b.jumpStrength, DefaultMin: 0, DefaultMax: 2, Min: 0, Max: 2}})
	equipment := ReadNativeMobEquipment(d.NativeNBT)
	if !b.armor.Empty() {
		equipment.Armor = []NativeMobItem{mountNativeStack(b.armor, NativeMobItem{})}
	} else if equipment.Armor != nil {
		equipment.Armor = []NativeMobItem{}
	}
	if b.chested {
		equipment.ChestItems = make([]NativeMobItem, len(b.chestItems))
		for i, s := range b.chestItems {
			slot := int32(i)
			equipment.ChestItems[i] = mountNativeStack(s, NativeMobItem{Slot: &slot})
		}
	}
	for k, v := range EncodeNativeMobEquipment(equipment) {
		native[k] = v
	}
	state := b.breeding.encodeNBT(map[string]any{"Tame": b.tamed, "Saddled": b.saddled, "ChestedHorse": b.chested, "Strength": int32(b.strength), "Variant": int32(b.variant), "Age": int32(b.ageTicks), "Health": float32(b.health.Health()), "Sitting": b.sitting, "JumpStrength": b.jumpStrength, "DashCooldown": int32(b.dashCooldown), "Temper": int32(b.temper), "Bred": b.bred, "EatingHaystack": b.eating, "SkeletonTrap": b.trap, "SkeletonTrapTime": int32(b.trapTime), "LastPoseTick": b.lastPoseTick})
	if b.owner != "" {
		state["Owner"] = javaUUIDIntArray(b.owner)
	}
	if b.kind == mountTraderLlama {
		state["DespawnDelay"] = int32(b.traderDespawn)
	}
	return MergeNativeMobJavaState(MergeNativeMobNBT(d.NativeNBT, native), state)
}
func mountNativeStack(s item.Stack, previous NativeMobItem) NativeMobItem {
	out := NativeMobItem{Raw: previous.Raw, Slot: previous.Slot}
	if s.Empty() {
		return out
	}
	name, damage := s.Item().EncodeItem()
	out.Name = name
	out.Count = byte(s.Count())
	out.Damage = int32(damage)
	if s.MaxDurability() > 0 {
		out.Damage = int32(s.MaxDurability() - s.Durability())
	}
	return out
}
func mountStackFromNative(items []NativeMobItem) item.Stack {
	for _, v := range items {
		if v.Name != "" && v.Count > 0 {
			if i, ok := world.ItemByName(v.Name, int16(v.Damage)); ok {
				return item.NewStack(i, int(v.Count))
			}
		}
	}
	return item.Stack{}
}
func mountStacksFromNative(items []NativeMobItem) []item.Stack {
	out := make([]item.Stack, len(items))
	for i, v := range items {
		if v.Name != "" && v.Count > 0 {
			if it, ok := world.ItemByName(v.Name, int16(v.Damage)); ok {
				out[i] = item.NewStack(it, int(v.Count))
			}
		}
	}
	return out
}

func (m *Mount) Health() float64        { return m.b().health.Health() }
func (m *Mount) MaxHealth() float64     { return m.b().health.MaxHealth() }
func (m *Mount) SetMaxHealth(v float64) { m.b().health.SetMaxHealth(v) }
func (m *Mount) Dead() bool             { return m.Health() <= 0 }

// Hurt damages the mount. A hurt camel stands up and a hurt horse rears up
// one time in three.
func (m *Mount) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if v <= 0 || m.Dead() {
		return 0, false
	}
	b := m.b()
	var ok bool
	if v, ok = b.ai.DamageCooldown(m.tx, v); !ok {
		return 0, false
	}
	v = damageAfterBodyArmour(v, src, b.armor)
	if v, ok = mobEffectDamage(b.effects, v, src); !ok {
		return 0, false
	}
	if b.camel() {
		// Camel.actuallyHurt stands the camel up at once.
		b.standUpInstantly(m)
	}
	old := b.health.Health()
	b.health.AddHealth(-v)
	b.ai.Hurt(src)
	resetLove(m)
	m.viewAction(HurtAction{})
	onMobHurt(m.tx, m)
	if m.Dead() {
		m.die(src)
	} else if rand.IntN(3) == 0 {
		b.standIfPossible(m)
	}
	return old - b.health.Health(), true
}

// die ejects the riders, drops the mount's loot and equipment and removes it.
func (m *Mount) die(src world.DamageSource) {
	tx := m.tx
	for _, h := range m.b().passengers {
		if h == nil {
			continue
		}
		if passenger, ok := h.Entity(tx); ok {
			m.Dismount(tx, passenger)
		}
	}
	m.viewAction(DeathAction{})
	dropAllDeathLoot(tx, m, src)
	_ = m.CloseIn(tx)
}

func (m *Mount) viewAction(a world.EntityAction) {
	viewers := m.tx.Viewers(m.Position())
	for _, v := range viewers {
		v.ViewEntityAction(m, a)
	}
	m.tx.ReleaseViewers(viewers)
}

// dropCustomDeathLoot drops the saddle and body armour or carpet, which the
// player equipped and Java therefore always drops.
func (m *Mount) dropCustomDeathLoot(bool, int) {
	b := m.b()
	if b.saddled {
		dropMobEquipment(m.tx, m, item.NewStack(item.Saddle{}, 1), 2, false, 0)
	}
	dropMobEquipment(m.tx, m, b.armor, 2, false, 0)
}

// dropEquipment ports AbstractChestedHorse.dropEquipment: the chest and its
// contents drop.
func (m *Mount) dropEquipment() {
	b := m.b()
	if !b.chested {
		return
	}
	for _, stack := range b.chestItems {
		if !stack.Empty() {
			m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, stack))
		}
	}
	m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, item.NewStack(block.Chest{}, 1)))
	b.chested, b.chestItems, b.chestInv = false, nil, nil
}
func (m *Mount) Heal(v float64, _ world.HealingSource) float64 {
	if v <= 0 || m.Dead() {
		return 0
	}
	old := m.Health()
	m.b().health.AddHealth(v)
	return m.Health() - old
}
func (m *Mount) KnockBack(src mgl64.Vec3, force, height float64) {
	// Knockback resistance, such as netherite horse armour's, weakens the
	// push.
	resistance := 0.
	if a, ok := m.b().armor.Item().(bodyArmour); ok && !m.b().armor.Empty() {
		resistance = a.KnockBackResistance()
	}
	knockBackResisted(m, src, force, height, resistance)
}
func (m *Mount) Speed() float64             { return m.b().movementSpeed }
func (m *Mount) SetSpeed(v float64)         { m.b().movementSpeed = v }
func (m *Mount) AddEffect(e effect.Effect)  { m.b().effects.Add(e, m) }
func (m *Mount) RemoveEffect(e effect.Type) { m.b().effects.Remove(e, m) }
func (m *Mount) Effects() []effect.Effect   { return m.b().effects.Effects() }

// mountSpawnGroup is the SpawnGroupData of horses and llamas: the colour the
// group shares and AgeableMobGroupData's group size.
type mountSpawnGroup struct {
	kind          mountKind
	variant, size int
}

// FinalizeSpawn ports finalizeSpawn of the horse species.
func (m *Mount) FinalizeSpawn(reason string, random *vanilla.Random) {
	if random == nil {
		return
	}
	b := m.b()
	if b.kind == mountZombieHorse && reason == "natural" && m.tx != nil {
		// ZombieHorse.finalizeSpawn: zombie horses spawn ridden by a zombie
		// with an iron spear.
		opts := world.EntitySpawnOpts{Position: m.Position(), Rotation: cube.Rotation{m.Rotation().Yaw(), 0}, SpawnReason: reason}
		if zombie, ok := m.tx.AddEntity(NewZombie(opts)).(*Zombie); ok {
			zombie.state().main = item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1)
			m.addMobPassenger(m.tx, zombie, false)
		}
	}
	b.randomizeHorse(m, random)
	if b.camel() {
		// Camel.resetLastPoseChangeTickToFullStand.
		b.lastPoseTick = max(0, m.tx.CurrentTick()-52-1)
	}
}

var (
	HorseType         = mountSpecies(mountHorse, "horse", 1.3965, 1.6)
	DonkeyType        = mountSpecies(mountDonkey, "donkey", 1.3965, 1.5)
	MuleType          = mountSpecies(mountMule, "mule", 1.3965, 1.6)
	SkeletonHorseType = mountSpecies(mountSkeletonHorse, "skeleton_horse", 1.3965, 1.6)
	ZombieHorseType   = mountSpecies(mountZombieHorse, "zombie_horse", 1.3965, 1.6)
	LlamaType         = mountSpecies(mountLlama, "llama", .9, 1.87)
	TraderLlamaType   = mountSpecies(mountTraderLlama, "trader_llama", .9, 1.87)
	CamelType         = mountSpecies(mountCamel, "camel", 1.7, 2.375)
	CamelHuskType     = mountSpecies(mountCamelHusk, "camel_husk", 1.7, 2.375)
)

func NewHorse(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(HorseType, defaultMountConfig(mountHorse))
}
func NewDonkey(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(DonkeyType, defaultMountConfig(mountDonkey))
}
func NewMule(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(MuleType, defaultMountConfig(mountMule))
}
func NewSkeletonHorse(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(SkeletonHorseType, defaultMountConfig(mountSkeletonHorse))
}
func NewSkeletonHorseTrap(o world.EntitySpawnOpts) *world.EntityHandle {
	c := defaultMountConfig(mountSkeletonHorse)
	c.SkeletonTrap = true
	return o.New(SkeletonHorseType, c)
}
func NewZombieHorse(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ZombieHorseType, defaultMountConfig(mountZombieHorse))
}
func NewLlama(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(LlamaType, defaultMountConfig(mountLlama))
}
func NewTraderLlama(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(TraderLlamaType, defaultMountConfig(mountTraderLlama))
}
func NewCamel(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(CamelType, defaultMountConfig(mountCamel))
}
func NewCamelHusk(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(CamelHuskType, defaultMountConfig(mountCamelHusk))
}

type llamaSpitType struct{}

var LlamaSpitType llamaSpitType

func NewLlamaSpit(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := ProjectileBehaviourConfig{Gravity: .03, Drag: .01, Damage: 1}
	if owner != nil {
		conf.Owner = owner.H()
	}
	return opts.New(LlamaSpitType, conf)
}
func (llamaSpitType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		d.Data = ProjectileBehaviourConfig{Gravity: .03, Drag: .01, Damage: 1}.New()
	}
	return &Ent{tx: tx, handle: h, data: d}
}
func (llamaSpitType) EncodeEntity() string { return "minecraft:llama_spit" }
func (llamaSpitType) BBox(world.Entity) cube.BBox {
	return cube.Box(-.125, -.125, -.125, .125, .125, .125)
}
func (llamaSpitType) DecodeNBT(_ map[string]any, d *world.EntityData) {
	d.Data = ProjectileBehaviourConfig{Gravity: .03, Drag: .01, Damage: 1}.New()
}
func (llamaSpitType) EncodeNBT(*world.EntityData) map[string]any { return nil }
