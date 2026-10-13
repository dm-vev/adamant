package entity

import (
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

type MountConfig struct {
	Kind                        uint8
	Tamed, Saddled, Chested     bool
	SkeletonTrap                bool
	Strength, Variant, AgeTicks int
	Health, MaxHealth           float64
	JumpStrength                float64
}

func (c MountConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c MountConfig) New() *MountBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 30
	}
	if c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	if c.Strength < 1 {
		c.Strength = 1
	}
	if c.Strength > 5 {
		c.Strength = 5
	}
	if c.JumpStrength <= 0 {
		c.JumpStrength = .7
	}
	b := &MountBehaviour{kind: mountKind(c.Kind), tamed: c.Tamed, saddled: c.Saddled, chested: c.Chested, trap: c.SkeletonTrap, strength: c.Strength, variant: c.Variant, ageTicks: c.AgeTicks, jumpStrength: c.JumpStrength, movementSpeed: .225, health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager()}
	b.base = PassiveBehaviourConfig{Gravity: .08, Drag: .02}.New()
	return b
}

type MountBehaviour struct {
	base                             *PassiveBehaviour
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
	dashCooldown, standTicks, trust  int
	chestItems                       []item.Stack
	armor                            item.Stack
	loveTicks                        int
	chestInv                         *inventory.Inventory
	gearInv                          *inventory.Inventory
	trap                             bool
	trapTime                         int
	caravanLeader                    *world.EntityHandle
	caravanLeaderUUID                string
	caravanHead                      bool
	traderDespawn                    int
	lastPoseTick                     int64
	spitCooldown                     int
}

func (b *MountBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.passengers[0] == nil || ((b.kind == mountCamel || b.kind == mountCamelHusk) && b.passengers[1] == nil) {
		b.restorePassengers(e, tx)
	}
	if b.dashCooldown > 0 {
		b.dashCooldown--
	}
	if b.kind == mountCamel || b.kind == mountCamelHusk {
		if b.sitting && b.forward != 0 {
			b.sitting = false
		}
	}
	if b.passengers[0] != nil && b.saddled && (b.kind != mountLlama && b.kind != mountTraderLlama) {
		direction := b.direction
		if direction.LenSqr() == 0 {
			direction = e.Rotation().Vec3()
		}
		e.data.Vel[0] += direction[0]*b.forward*.055 + direction[2]*b.strafe*.035
		e.data.Vel[2] += direction[2]*b.forward*.055 - direction[0]*b.strafe*.035
	}
	m := b.base.Tick(e, tx)
	b.effects.Tick(&Mount{Ent: e}, tx)
	if b.kind == mountSkeletonHorse && b.trap {
		b.trapTime++
		if b.triggerSkeletonTrap(e, tx) {
			return m
		}
		if b.trapTime >= 18000 {
			_ = e.CloseIn(tx)
			return nil
		}
	}
	b.defendWithSpit(e, tx)
	if b.loveTicks > 0 {
		b.loveTicks--
		b.breedNearby(e, tx)
	}
	if b.kind == mountTraderLlama {
		if b.traderDespawn == 0 {
			b.traderDespawn = 47999
		}
		b.traderDespawn--
		if b.traderDespawn <= 0 {
			_ = e.CloseIn(tx)
			return nil
		}
		b.followTrader(e, tx)
	}
	b.followCaravan(e, tx)
	b.updatePassengers(e, tx)
	return m
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
		if b.kind == mountCamel || b.kind == mountCamelHusk {
			offset = 2.05
			if i == 1 {
				seat[0] = .45
			} else {
				seat[0] = -.45
			}
		}
		if b.kind == mountLlama || b.kind == mountTraderLlama {
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
func (b *MountBehaviour) followTrader(e *Ent, tx *world.Tx) {
	box := cube.Box(-32, -12, -32, 32, 12, 32).Translate(e.Position())
	var target world.Entity
	nearest := 32.0
	for candidate := range tx.EntitiesWithin(box) {
		if candidate.H().Type().EncodeEntity() != "minecraft:wandering_trader" {
			continue
		}
		d := candidate.Position().Sub(e.Position())
		d[1] = 0
		length := d.Len()
		if length < nearest {
			target = candidate
			nearest = length
		}
	}
	if target != nil && nearest > 5 {
		d := target.Position().Sub(e.Position())
		d[1] = 0
		e.data.Vel = e.data.Vel.Add(d.Normalize().Mul(.025))
	}
}
func (b *MountBehaviour) defendWithSpit(e *Ent, tx *world.Tx) {
	if b.kind != mountLlama && b.kind != mountTraderLlama {
		return
	}
	if b.spitCooldown > 0 {
		b.spitCooldown--
		return
	}
	box := cube.Box(-16, -8, -16, 16, 8, 16).Translate(e.Position())
	var target world.Entity
	best := 256.0
	traderNearby := false
	if b.kind == mountTraderLlama {
		for candidate := range tx.EntitiesWithin(box) {
			if candidate.H().Type().EncodeEntity() == "minecraft:wandering_trader" {
				traderNearby = true
				break
			}
		}
	}
	for candidate := range tx.EntitiesWithin(box) {
		id := candidate.H().Type().EncodeEntity()
		if id == "minecraft:wandering_trader" {
			traderNearby = true
			continue
		}
		eligible := false
		if b.kind == mountLlama && id == "minecraft:wolf" {
			if wolf, ok := candidate.(*Wolf); ok {
				eligible = !wolf.Tamed()
			}
		}
		if b.kind == mountTraderLlama && traderNearby && (id == "minecraft:zombie" || id == "minecraft:husk" || id == "minecraft:drowned" || id == "minecraft:evoker" || id == "minecraft:pillager" || id == "minecraft:vindicator") {
			eligible = true
		}
		if !eligible {
			continue
		}
		d := candidate.Position().Sub(e.Position()).LenSqr()
		if d < best {
			target = candidate
			best = d
		}
	}
	if target != nil && (&Mount{Ent: e}).SpitAt(tx, target) {
		b.spitCooldown = 60
	}
}
func (b *MountBehaviour) followCaravan(e *Ent, tx *world.Tx) {
	if b.kind != mountLlama && b.kind != mountTraderLlama {
		return
	}
	if b.caravanLeader == nil && b.caravanLeaderUUID != "" {
		box := cube.Box(-32, -8, -32, 32, 8, 32).Translate(e.Position())
		for candidate := range tx.EntitiesWithin(box) {
			if candidate.H().UUID().String() == b.caravanLeaderUUID {
				b.caravanLeader = candidate.H()
				break
			}
		}
	}
	if b.caravanLeader != nil {
		leader, ok := b.caravanLeader.Entity(tx)
		if !ok {
			b.caravanLeader = nil
			return
		}
		d := leader.Position().Sub(e.Position())
		d[1] = 0
		if d.Len() > 3 {
			e.data.Vel = e.data.Vel.Add(d.Normalize().Mul(.025))
		}
		return
	}
	if !b.caravanHead {
		box := cube.Box(-9, -4, -9, 9, 4, 9).Translate(e.Position())
		var loose *Mount
		for candidate := range tx.EntitiesWithin(box) {
			other := mountFromEntity(candidate)
			if other == nil || candidate.H() == e.H() {
				continue
			}
			ob := other.b()
			if ob.kind != mountLlama && ob.kind != mountTraderLlama {
				continue
			}
			if ob.caravanHead || ob.caravanLeader != nil {
				b.caravanLeader = candidate.H()
				b.caravanLeaderUUID = candidate.H().UUID().String()
				return
			}
			if loose == nil {
				loose = other
			}
		}
		if loose != nil {
			b.caravanHead = true
			loose.b().caravanLeader = e.H()
			loose.b().caravanLeaderUUID = e.H().UUID().String()
		}
		return
	}
	if !b.caravanHead {
		return
	}
	box := cube.Box(-9, -4, -9, 9, 4, 9).Translate(e.Position())
	for candidate := range tx.EntitiesWithin(box) {
		other := mountFromEntity(candidate)
		if other == nil || candidate.H() == e.H() {
			continue
		}
		ob := other.b()
		if ob.kind != mountLlama && ob.kind != mountTraderLlama || ob.caravanLeader != nil || ob.caravanHead {
			continue
		}
		if candidate.Position().Sub(e.Position()).LenSqr() <= 81 {
			ob.caravanLeader = e.H()
			return
		}
	}
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
	return passenger != nil && len(m.b().passengers) > 0 && m.b().passengers[0] != nil && passenger.H() == m.b().passengers[0]
}
func (m *Mount) SetJumpStrength(v float64) {
	b := m.b()
	if m.b().saddled {
		b.jumpStrength = v
	}
}
func (m *Mount) Jump() bool {
	b := m.b()
	if !b.saddled || b.dashCooldown > 0 || b.sitting {
		return false
	}
	v := m.Velocity()
	v[1] = b.jumpStrength
	m.SetVelocity(v)
	return true
}
func (m *Mount) RideJump() bool {
	b := m.b()
	if b.kind == mountCamel || b.kind == mountCamelHusk {
		return true
	}
	return m.Jump()
}
func (m *Mount) Dash(_ *world.Tx, passenger world.Entity) bool {
	b := m.b()
	if passenger == nil || !m.ControlsVehicle(passenger) || (b.kind != mountCamel && b.kind != mountCamelHusk) || b.dashCooldown > 0 || b.sitting {
		return false
	}
	v := m.Velocity()
	direction := b.direction
	if direction.LenSqr() == 0 {
		direction = passenger.Rotation().Vec3()
	}
	v[0] += direction[0] * .8
	v[2] += direction[2] * .8
	m.SetVelocity(v)
	b.dashCooldown = 55
	return true
}
func (m *Mount) Mount(tx *world.Tx, passenger world.Entity) bool {
	b := m.b()
	if passenger == nil {
		return false
	}
	r, ok := passenger.(Rider)
	if !ok || r.Riding() != nil {
		return false
	}
	if !b.tamed {
		b.trust++
		if b.trust >= 3 {
			b.tamed = true
		}
		return true
	}
	seat := 0
	if b.passengers[0] != nil {
		if b.kind != mountCamel && b.kind != mountCamelHusk {
			return false
		}
		seat = 1
	}
	if b.kind != mountLlama && b.kind != mountTraderLlama && !b.saddled {
		return false
	}
	b.passengers[seat] = passenger.H()
	r.SetRiding(m.H())
	m.broadcastLink(tx, passenger.H(), true)
	return true
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
}
func (m *Mount) broadcastLink(tx *world.Tx, h *world.EntityHandle, link bool) {
	viewers := tx.Viewers(m.Position())
	for _, v := range viewers {
		if lv, ok := v.(interface {
			ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool)
		}); ok {
			lv.ViewEntityLinkType(m.H(), h, LinkPassenger, link)
		} else if lv, ok := v.(interface {
			ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool)
		}); ok {
			lv.ViewEntityLink(m.H(), h, link)
		}
	}
	tx.ReleaseViewers(viewers)
}
func (m *Mount) Interact(tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if user == nil {
		return false
	}
	b := m.b()
	held, _ := user.HeldItems()
	if !held.Empty() && m.Feed(held, ctx) {
		return true
	}
	if _, ok := held.Item().(item.Saddle); ok && (b.kind != mountSkeletonHorse || b.tamed) && !b.saddled {
		b.saddled = true
		if b.gearInv != nil {
			_ = b.gearInv.SetItem(0, item.NewStack(item.Saddle{}, 1))
		}
		if ctx != nil {
			ctx.SubtractFromCount(1)
		}
		return true
	}
	if b.kind == mountHorse && mountArmor(held) && b.tamed && b.armor.Empty() {
		b.armor = held
		if b.gearInv != nil {
			_ = b.gearInv.SetItem(1, held)
		}
		if ctx != nil {
			ctx.SubtractFromCount(1)
		}
		m.viewArmour(tx)
		return true
	}
	if b.kind == mountDonkey || b.kind == mountMule || b.kind == mountLlama || b.kind == mountTraderLlama {
		// AbstractChestedHorse.mobInteract: a tamed adult takes a chest.
		if _, chest := held.Item().(block.Chest); chest && !b.chested && b.tamed && b.ageTicks >= 0 {
			b.chested = true
			b.chestItems = make([]item.Stack, b.chestSize())
			if ctx != nil {
				ctx.SubtractFromCount(1)
			}
			tx.PlaySound(m.Position(), sound.Custom{Name: "mob.horse.armor", Volume: 1, Pitch: 1})
			return true
		}
	}
	if b.kind == mountHorse && held.Empty() && b.tamed {
		if sneaking, ok := user.(interface{ Sneaking() bool }); ok && sneaking.Sneaking() {
			if opener, ok := user.(ContainerOpener); ok {
				opener.OpenEntityContainer(m, m.gearInventory(), mountContainerChest, tx)
				return true
			}
		}
	}
	if b.chested && held.Empty() {
		if opener, ok := user.(ContainerOpener); ok {
			opener.OpenEntityContainer(m, b.chestInventory(), mountContainerChest, tx)
			return true
		}
	}
	if held.Empty() && !b.tamed {
		b.trust++
		if b.trust >= 3 {
			b.tamed = true
		}
		return true
	}
	if b.tamed && (b.saddled || b.kind == mountLlama || b.kind == mountTraderLlama) {
		return m.Mount(tx, user)
	}
	return false
}
func (m *Mount) Feed(stack item.Stack, ctx *item.UseContext) bool {
	if stack.Empty() {
		return false
	}
	name, _ := stack.Item().EncodeItem()
	b := m.b()
	heal := 0.0
	growth := 0
	switch b.kind {
	case mountCamel:
		if name == "minecraft:cactus" {
			heal, growth = 2, 10
		}
	case mountCamelHusk:
		if name == "minecraft:rabbit_foot" {
			heal, growth = 2, 10
		}
	case mountLlama, mountTraderLlama:
		switch name {
		case "minecraft:wheat":
			heal, growth = 2, 10
		case "minecraft:hay_block":
			heal, growth = 10, 90
		}
	case mountHorse, mountDonkey, mountMule:
		switch name {
		case "minecraft:sugar":
			heal, growth = 1, 30
		case "minecraft:wheat":
			heal, growth = 2, 20
		case "minecraft:apple":
			heal, growth = 3, 60
		case "minecraft:golden_carrot":
			heal, growth = 4, 90
		case "minecraft:golden_apple", "minecraft:enchanted_golden_apple":
			heal, growth = 10, 240
		}
	}
	if heal == 0 {
		return false
	}
	m.Heal(heal, FoodHealingSource{})
	if b.ageTicks < 0 {
		b.ageTicks = min(0, b.ageTicks+growth)
	}
	breedingFood := false
	switch b.kind {
	case mountHorse, mountDonkey, mountMule:
		breedingFood = name == "minecraft:golden_carrot" || name == "minecraft:golden_apple" || name == "minecraft:enchanted_golden_apple"
	case mountLlama, mountTraderLlama:
		breedingFood = name == "minecraft:hay_block"
	case mountCamel:
		breedingFood = name == "minecraft:cactus"
	case mountCamelHusk:
		breedingFood = name == "minecraft:rabbit_foot"
	}
	if breedingFood && b.ageTicks >= 0 && b.tamed {
		b.loveTicks = 600
	}
	if ctx != nil {
		ctx.SubtractFromCount(1)
	}
	return true
}
func (m *Mount) CanBreedWith(other *Mount) bool {
	if other == nil || other == m || m.b().ageTicks < 0 || other.b().ageTicks < 0 {
		return false
	}
	a, c := m.b().kind, other.b().kind
	return (a == c && (a == mountHorse || a == mountDonkey || a == mountLlama || a == mountTraderLlama || a == mountCamel || a == mountCamelHusk)) || (a == mountHorse && c == mountDonkey) || (a == mountDonkey && c == mountHorse)
}
func (m *Mount) BreedWith(other *Mount, opts world.EntitySpawnOpts) *world.EntityHandle {
	if !m.CanBreedWith(other) {
		return nil
	}
	kind := m.b().kind
	if kind == mountHorse && other.b().kind == mountDonkey {
		kind = mountMule
	}
	if kind == mountDonkey && other.b().kind == mountHorse {
		kind = mountMule
	}
	config := defaultMountConfig(kind)
	config.Tamed = true
	config.AgeTicks = -24000
	config.Strength = m.b().strength
	config.Variant = m.b().variant
	return opts.New(mountTypeFor(kind), config)
}
func mountTypeFor(kind mountKind) world.EntityType {
	switch kind {
	case mountHorse:
		return HorseType
	case mountDonkey:
		return DonkeyType
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
func (b *MountBehaviour) breedNearby(e *Ent, tx *world.Tx) {
	own := &Mount{Ent: e}
	box := cube.Box(-8, -4, -8, 8, 4, 8).Translate(e.Position())
	for candidate := range tx.EntitiesWithin(box) {
		other := mountFromEntity(candidate)
		if other == nil || other.b().loveTicks <= 0 || !own.CanBreedWith(other) {
			continue
		}
		child := own.BreedWith(other, world.EntitySpawnOpts{Position: e.Position(), SpawnReason: "BREEDING"})
		if child == nil {
			continue
		}
		b.loveTicks = 0
		other.b().loveTicks = 0
		tx.AddEntity(child)
		return
	}
}
func mountFromEntity(e world.Entity) *Mount {
	switch v := e.(type) {
	case *Mount:
		return v
	case *Horse:
		return v.Mount
	case *Donkey:
		return v.Mount
	case *Mule:
		return v.Mount
	case *SkeletonHorse:
		return v.Mount
	case *ZombieHorse:
		return v.Mount
	case *Llama:
		return v.Mount
	case *TraderLlama:
		return v.Mount
	case *Camel:
		return v.Mount
	case *CamelHusk:
		return v.Mount
	default:
		return nil
	}
}
func (b *MountBehaviour) triggerSkeletonTrap(e *Ent, tx *world.Tx) bool {
	for player := range tx.Players() {
		if player.Position().Sub(e.Position()).LenSqr() > 100 {
			continue
		}
		b.trap = false
		b.tamed = true
		b.ageTicks = 0
		spawnPair := func(horse *Mount) {
			riderEntity := tx.AddEntity(NewSkeleton(world.EntitySpawnOpts{Position: horse.Position(), SpawnReason: "TRIGGERED"}))
			rider, ok := riderEntity.(Rider)
			if !ok {
				return
			}
			horse.b().passengers[0] = riderEntity.H()
			rider.SetRiding(horse.H())
			horse.broadcastLink(tx, riderEntity.H(), true)
		}
		spawnPair(&Mount{Ent: e})
		for i := range 3 {
			pos := e.Position().Add(mgl64.Vec3{float64(i+1) * 1.2, 0, float64(i%2) * 1.2})
			entity := tx.AddEntity(NewSkeletonHorse(world.EntitySpawnOpts{Position: pos, SpawnReason: "TRIGGERED"}))
			horse := mountFromEntity(entity)
			if horse == nil {
				continue
			}
			horse.b().tamed = true
			horse.b().ageTicks = 0
			spawnPair(horse)
		}
		return true
	}
	return false
}
func mountArmor(s item.Stack) bool {
	if s.Empty() {
		return false
	}
	switch s.Item().(type) {
	case item.LeatherHorseArmour, item.CopperHorseArmour, item.IronHorseArmour, item.GoldenHorseArmour, item.DiamondHorseArmour:
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
func (m *Mount) BodyItem() item.Stack {
	if m.b().kind != mountHorse {
		return item.Stack{}
	}
	return m.b().armor
}
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
				return ok
			}
			return b.kind == mountHorse && mountArmor(s)
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
func (m *Mount) Variant() int                                                      { return m.b().variant }
func (m *Mount) Sitting() bool                                                     { return m.b().sitting }
func (m *Mount) SetSitting(v bool) {
	if m.b().kind == mountCamel || m.b().kind == mountCamelHusk {
		m.b().sitting = v
	}
}
func (m *Mount) AgeTicks() int { return m.b().ageTicks }
func (m *Mount) StartCaravan() bool {
	b := m.b()
	if b.kind != mountLlama && b.kind != mountTraderLlama || b.caravanLeader != nil {
		return false
	}
	b.caravanHead = true
	return true
}
func (m *Mount) JoinCaravan(leader world.Entity) bool {
	if leader == nil || leader.H() == m.H() {
		return false
	}
	lead := mountFromEntity(leader)
	if lead == nil || (lead.b().kind != mountLlama && lead.b().kind != mountTraderLlama) {
		return false
	}
	if m.b().kind != mountLlama && m.b().kind != mountTraderLlama {
		return false
	}
	m.b().caravanLeader = leader.H()
	m.b().caravanLeaderUUID = leader.H().UUID().String()
	m.b().caravanHead = false
	return true
}
func (m *Mount) LeaveCaravan()                      { m.b().caravanLeader = nil; m.b().caravanHead = false }
func (m *Mount) CaravanLeader() *world.EntityHandle { return m.b().caravanLeader }
func (m *Mount) SpitAt(tx *world.Tx, target world.Entity) bool {
	if target == nil || (m.b().kind != mountLlama && m.b().kind != mountTraderLlama) {
		return false
	}
	delta := target.Position().Sub(m.Position())
	delta[1] += math.Sqrt(delta[0]*delta[0]+delta[2]*delta[2]) * .2
	if delta.LenSqr() == 0 {
		return false
	}
	velocity := delta.Normalize().Mul(1.5)
	tx.AddEntity(NewLlamaSpit(world.EntitySpawnOpts{Position: m.Position().Add(mgl64.Vec3{0, 1.2, 0}), Velocity: velocity}, m))
	return true
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
	if b.kind == mountLlama || b.kind == mountTraderLlama {
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
func (t mountType) BBox(world.Entity) cube.BBox {
	return cube.Box(-t.width/2, 0, -t.width/2, t.width/2, t.height, t.width/2)
}
func (t mountType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := defaultMountConfig(t.kind)
	state := ReadNativeMobJavaState(n)
	if state == nil {
		state = n
	}
	if _, ok := state["Tamed"]; ok {
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
	d.Data = c.New()
	b := d.Data.(*MountBehaviour)
	// AbstractHorse.finalizeSpawn replaces the max-health base after loading
	// Health; Source does not clamp that already-loaded health a second time.
	if _, loadedHealth := state["Health"]; loadedHealth {
		b.health.health = c.Health
	}
	b.sitting = nbtconv.Bool(state, "Sitting")
	b.lastPoseTick = nbtconv.Int64(state, "LastPoseTick")
	if speed, ok := nativeMobAttribute(n, "minecraft:movement"); ok {
		b.movementSpeed = speed.Base
	}
	if jump, ok := nativeMobAttribute(n, "minecraft:horse.jump_strength"); ok {
		b.jumpStrength = jump.Base
	}
	b.trust = int(nbtconv.Int32(state, "Trust"))
	b.trap = nbtconv.Bool(state, "SkeletonTrap")
	b.trapTime = int(nbtconv.Int32(state, "SkeletonTrapTime"))
	b.caravanHead = nbtconv.Bool(state, "CaravanHead")
	b.traderDespawn = int(nbtconv.Int32(state, "TraderDespawn"))
	b.caravanLeaderUUID, _ = state["CaravanLeaderUUID"].(string)
	b.armor = mountStackFromNative(ReadNativeMobEquipment(n).Armor)
	if b.chested {
		b.chestItems = mountStacksFromNative(ReadNativeMobEquipment(n).ChestItems)
	}
}
func defaultMountConfig(kind mountKind) MountConfig {
	c := MountConfig{Kind: uint8(kind), Strength: 1, JumpStrength: .7}
	switch kind {
	case mountSkeletonHorse, mountZombieHorse:
		c.MaxHealth = 15
		c.Tamed = true
	case mountCamel, mountCamelHusk:
		c.MaxHealth = 32
		c.JumpStrength = .42
		c.Tamed = true
	case mountHorse, mountDonkey, mountMule, mountLlama, mountTraderLlama:
		c.MaxHealth = 30
	}
	c.Health = c.MaxHealth
	return c
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
	state := map[string]any{"Tamed": b.tamed, "Saddled": b.saddled, "ChestedHorse": b.chested, "Strength": int32(b.strength), "Variant": int32(b.variant), "Age": int32(b.ageTicks), "Health": float32(b.health.Health()), "Sitting": b.sitting, "JumpStrength": b.jumpStrength, "DashCooldown": int32(b.dashCooldown), "Trust": int32(b.trust), "SkeletonTrap": b.trap, "SkeletonTrapTime": int32(b.trapTime), "CaravanHead": b.caravanHead, "CaravanLeaderUUID": b.caravanLeaderUUID, "TraderDespawn": int32(b.traderDespawn), "LastPoseTick": b.lastPoseTick}
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
func (m *Mount) Hurt(v float64, src world.DamageSource) (float64, bool) {
	if v <= 0 || m.Dead() {
		return 0, false
	}
	b := m.b()
	old := b.health.Health()
	b.health.AddHealth(-v)
	m.viewAction(HurtAction{})
	if m.Dead() {
		m.die(src)
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
	d := m.Position().Sub(src)
	d[1] = 0
	if d.LenSqr() == 0 {
		return
	}
	m.SetVelocity(m.Velocity().Add(d.Normalize().Mul(force).Add(mgl64.Vec3{0, height, 0})))
}
func (m *Mount) Speed() float64             { return m.b().movementSpeed }
func (m *Mount) SetSpeed(v float64)         { m.b().movementSpeed = v }
func (m *Mount) AddEffect(e effect.Effect)  { m.b().effects.Add(e, m) }
func (m *Mount) RemoveEffect(e effect.Type) { m.b().effects.Remove(e, m) }
func (m *Mount) Effects() []effect.Effect   { return m.b().effects.Effects() }

type mountSpawnGroup struct {
	kind              mountKind
	variant, strength int
}

func (m *Mount) FinalizeSpawn(reason string, random *java26.Random) {
	b := m.b()
	if reason == "breeding" {
		b.ageTicks = -24000
		return
	}
	if random == nil {
		return
	}
	if group := m.H().SpawnGroup(); group != nil {
		if data, ok := group.Data.(mountSpawnGroup); ok && data.kind == b.kind {
			b.variant, b.strength = data.variant, data.strength
			return
		}
		switch b.kind {
		case mountHorse:
			b.variant = int(random.NextInt(7))
		case mountLlama, mountTraderLlama:
			b.variant = int(random.NextInt(4))
			b.strength = int(random.NextInt(5)) + 1
		}
		group.Data = mountSpawnGroup{kind: b.kind, variant: b.variant, strength: b.strength}
		return
	}
	switch b.kind {
	case mountHorse:
		b.variant = int(random.NextInt(7))
	case mountLlama, mountTraderLlama:
		b.variant = int(random.NextInt(4))
		b.strength = int(random.NextInt(5)) + 1
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
