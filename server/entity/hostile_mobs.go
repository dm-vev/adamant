package entity

import (
	"hash/fnv"
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

type hostileMobType struct {
	id                                   string
	width, height, health, speed, attack float64
	ranged                               bool
}

func (t hostileMobType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		hostileConfig{kind: t.id}.Apply(d)
	}
	m := &HostileMob{Ent: &Ent{tx: tx, handle: h, data: d}}
	if h != nil {
		id := h.UUID()
		hash := fnv.New64a()
		_, _ = hash.Write(id[:])
		d.Data.(*hostileState).random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
	}
	switch t.id {
	case "minecraft:zombie":
		return &Zombie{m}
	case "minecraft:zombie_villager":
		return &ZombieVillager{m}
	case "minecraft:husk":
		return &Husk{m}
	case "minecraft:skeleton":
		return &Skeleton{m}
	case "minecraft:parched":
		return &Parched{m}
	case "minecraft:wither_skeleton":
		return &WitherSkeleton{m}
	case "minecraft:stray":
		return &Stray{m}
	case "minecraft:bogged":
		return &Bogged{m}
	case "minecraft:evoker":
		return &Evoker{m}
	case "minecraft:vindicator":
		return &Vindicator{m}
	case "minecraft:pillager":
		return &Pillager{m}
	case "minecraft:vex":
		return &Vex{m}
	case "minecraft:ravager":
		return &Ravager{m}
	}
	return m
}
func (t hostileMobType) EncodeEntity() string { return t.id }
func (t hostileMobType) BBox(e world.Entity) cube.BBox {
	if m, ok := e.(interface{ state() *hostileState }); ok && m.state().baby {
		return cube.Box(-.245, 0, -.245, .245, .98, .245)
	}
	return cube.Box(-t.width/2, 0, -t.width/2, t.width/2, t.height, t.width/2)
}
func (t hostileMobType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := hostileConfig{kind: t.id}
	for _, attribute := range ReadNativeMobAttributes(n) {
		switch attribute.Name {
		case "minecraft:health":
			c.health, c.healthSet = attribute.Current, true
		case "minecraft:movement":
			if attribute.Current > 0 {
				c.speed, c.speedSet = attribute.Current, true
			}
		}
	}
	if c.health == 0 {
		if _, ok := n["Health"]; ok {
			c.health, c.healthSet = float64(nbtconv.Float32(n, "Health")), true
		}
	}
	c.baby, c.canBreakDoors, c.canPickupLoot = nbtconv.Bool(n, "IsBaby"), nbtconv.Bool(n, "CanBreakDoors"), nbtconv.Bool(n, "CanPickUpLoot")
	if java := ReadNativeMobJavaState(n); java != nil {
		c.conversionTime = int(nbtconv.Int32(java, "InWaterTime"))
		c.raidID, c.raidWave, c.raidLeader = nbtconv.Int64(java, "RaidID"), int(nbtconv.Int32(java, "RaidWave")), nbtconv.Bool(java, "RaidLeader")
		c.headDropGuaranteed = nbtconv.Bool(java, "RaidBannerDrop")
		c.patrolLeader, c.patrolling = nbtconv.Bool(java, "PatrolLeader"), nbtconv.Bool(java, "Patrolling")
		c.freezingTime = int(nbtconv.Int32(java, "FreezingTime"))
		if _, ok := java["StrayConversionTime"]; ok {
			if n := nbtconv.Int32(java, "StrayConversionTime"); n != -1 {
				c.strayConversionTime, c.strayConverting = int(n), true
			}
		}
		if target, ok := java["patrol_target"].([]int32); ok && len(target) == 3 {
			c.patrolTarget, c.hasPatrolTarget = cube.Pos{int(target[0]), int(target[1]), int(target[2])}, true
		}
		if data, ok := java["SpawnData"].(map[string]any); ok {
			c.spawnData = CloneNativeMobNBT(data)
		}
	}
	c.cureTicks = int(nbtconv.Int32(n, "ConversionTime"))
	if source, ok := n["equipment"].(map[string]any); ok {
		if hand, ok := source["mainhand"].(map[string]any); ok {
			c.main, _ = item.StackFromStructureNBT(hand)
		}
		if head, ok := source["head"].(map[string]any); ok {
			c.head, _ = item.StackFromStructureNBT(head)
		}
	}
	equipment := ReadNativeMobEquipment(n)
	if _, native := n["Mainhand"]; native {
		c.main = item.Stack{}
		if len(equipment.Mainhand) > 0 {
			c.main = item.ReadNBT(equipment.Mainhand[0].Raw, nil)
		}
	}
	if len(equipment.Armor) > 0 {
		c.head = hostileStackFromNative(equipment.Armor[0])
	}
	if c.head.Empty() {
		// Pre-1.21.5 Java ArmorItems lists feet, legs, chest and head.
		if armor, ok := n["ArmorItems"].([]any); ok && len(armor) == 4 {
			if raw, ok := armor[3].(map[string]any); ok {
				if stack, err := item.StackFromStructureNBT(raw); err == nil && !stack.Empty() {
					c.head = stack
				}
			}
		}
	}
	if c.raidLeader && !c.head.Empty() {
		c.headDropGuaranteed = true
	}
	if t.id == "minecraft:zombie_villager" {
		var villagerData world.EntityData
		VillagerType.DecodeNBT(n, &villagerData)
		if villager, ok := villagerData.Data.(*VillagerBehaviour); ok {
			c.profession, c.villagerType, c.villagerLevel, c.villagerXP, c.villagerAge = villager.profession, villager.villagerType, villager.level, villager.xp, villager.age
			c.offers = append([]MerchantOffer(nil), villager.offers...)
		}
	}
	c.Apply(d)
}
func (hostileMobType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*hostileState)
	t := hostileTypeFor(b.kind)
	attrs := []NativeMobAttribute{
		{Name: "minecraft:health", Base: t.health, Current: b.health.Health(), DefaultMin: 0, DefaultMax: t.health, Min: 0, Max: t.health},
		{Name: "minecraft:movement", Base: t.speed, Current: b.speed, DefaultMin: 0, DefaultMax: t.speed, Min: 0, Max: t.speed},
		{Name: "minecraft:attack_damage", Base: t.attack, Current: t.attack, DefaultMin: 0, DefaultMax: t.attack, Min: 0, Max: t.attack},
	}
	equipment := ReadNativeMobEquipment(d.NativeNBT)
	equipment.Mainhand = []NativeMobItem{nativeMobItemFromStack(b.main)}
	if !b.head.Empty() {
		var previous NativeMobItem
		if len(equipment.Armor) > 0 {
			previous = equipment.Armor[0]
		}
		armor := equipment.Armor
		if len(armor) < 4 {
			extended := make([]NativeMobItem, 4)
			copy(extended, armor)
			armor = extended
		}
		armor[0] = hostileNativeStack(b.head, previous)
		equipment.Armor = armor
	}
	native := CloneNativeMobNBT(d.NativeNBT)
	if native == nil {
		native = make(map[string]any)
	}
	native["Attributes"] = EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), attrs)
	native["IsBaby"], native["CanBreakDoors"], native["CanPickUpLoot"] = byte(boolByte(b.baby)), byte(boolByte(b.canBreakDoors)), byte(boolByte(b.canPickupLoot))
	for key, value := range EncodeNativeMobEquipment(equipment) {
		native[key] = value
	}
	javaState := map[string]any{"InWaterTime": int32(b.conversionTime), "RaidID": b.raidID, "RaidWave": int32(b.raidWave), "RaidLeader": b.raidLeader, "RaidBannerDrop": b.headDropGuaranteed,
		"PatrolLeader": b.patrolLeader, "Patrolling": b.patrolling, "FreezingTime": int32(b.freezingTime), "StrayConversionTime": int32(-1)}
	if b.strayConverting {
		javaState["StrayConversionTime"] = int32(b.strayConversionTime)
	}
	if b.hasPatrolTarget {
		javaState["patrol_target"] = []int32{int32(b.patrolTarget[0]), int32(b.patrolTarget[1]), int32(b.patrolTarget[2])}
	}
	if b.spawnData != nil {
		javaState["SpawnData"] = CloneNativeMobNBT(b.spawnData)
	}
	native = MergeNativeMobJavaState(native, javaState)
	if b.kind == "minecraft:zombie_villager" {
		native["ConversionTime"] = int32(b.cureTicks)
	}
	return native
}

type hostileConfig struct {
	kind                                                    string
	health, speed                                           float64
	healthSet, speedSet, baby, canBreakDoors, canPickupLoot bool
	conversionTime, cureTicks                               int
	main, head                                              item.Stack
	profession, villagerType                                string
	villagerLevel, villagerXP, villagerAge                  int
	offers                                                  []MerchantOffer
	raidID                                                  int64
	raidWave                                                int
	raidLeader, headDropGuaranteed                          bool
	patrolLeader, patrolling, hasPatrolTarget               bool
	patrolTarget                                            cube.Pos
	spawnData                                               map[string]any
	freezingTime, strayConversionTime                       int
	strayConverting                                         bool
}

func (c hostileConfig) Apply(d *world.EntityData) {
	t := hostileTypeFor(c.kind)
	health := c.health
	if !c.healthSet && health <= 0 {
		health = t.health
	}
	speed := c.speed
	if speed <= 0 {
		speed = t.speed
	}
	if c.baby && !c.speedSet {
		speed *= 1.5
	}
	gravity := .08
	if c.kind == "minecraft:vex" {
		gravity = 0
	}
	d.Data = &hostileState{
		health: NewHealthManager(health, t.health), effects: NewEffectManager(), mc: &MovementComputer{Gravity: gravity, Drag: .02, DragBeforeGravity: true}, random: vanilla.NewLegacyRandom(0),
		speed: speed, attack: t.attack, kind: c.kind, baby: c.baby, canBreakDoors: c.canBreakDoors, canPickupLoot: c.canPickupLoot,
		conversionTime: c.conversionTime, cureTicks: c.cureTicks, main: c.main, head: c.head, profession: c.profession, villagerType: c.villagerType,
		raidID: c.raidID, raidWave: c.raidWave, raidLeader: c.raidLeader, headDropGuaranteed: c.headDropGuaranteed, spawnData: CloneNativeMobNBT(c.spawnData),
		patrolLeader: c.patrolLeader, patrolling: c.patrolling, patrolTarget: c.patrolTarget, hasPatrolTarget: c.hasPatrolTarget,
		freezingTime: c.freezingTime, strayConversionTime: c.strayConversionTime, strayConverting: c.strayConverting,
	}
}

var (
	ZombieType         = hostileMobType{id: "minecraft:zombie", width: .6, height: 1.95, health: 20, speed: .23, attack: 3}
	ZombieVillagerType = hostileMobType{id: "minecraft:zombie_villager", width: .6, height: 1.95, health: 20, speed: .23, attack: 3}
	HuskType           = hostileMobType{id: "minecraft:husk", width: .6, height: 1.95, health: 20, speed: .23, attack: 3}
	SkeletonType       = hostileMobType{id: "minecraft:skeleton", width: .6, height: 1.99, health: 20, speed: .25, attack: 2, ranged: true}
	WitherSkeletonType = hostileMobType{id: "minecraft:wither_skeleton", width: .7, height: 2.4, health: 20, speed: .25, attack: 4}
	StrayType          = hostileMobType{id: "minecraft:stray", width: .6, height: 1.99, health: 20, speed: .25, attack: 2, ranged: true}
	ParchedType        = hostileMobType{id: "minecraft:parched", width: .6, height: 1.99, health: 16, speed: .25, attack: 2, ranged: true}
	BoggedType         = hostileMobType{id: "minecraft:bogged", width: .6, height: 1.99, health: 16, speed: .25, attack: 2, ranged: true}
	EvokerType         = hostileMobType{id: "minecraft:evoker", width: .6, height: 1.95, health: 24, speed: .25, attack: 3}
	VindicatorType     = hostileMobType{id: "minecraft:vindicator", width: .6, height: 1.95, health: 24, speed: .25, attack: 5}
	PillagerType       = hostileMobType{id: "minecraft:pillager", width: .6, height: 1.95, health: 24, speed: .25, attack: 2, ranged: true}
	VexType            = hostileMobType{id: "minecraft:vex", width: .4, height: .8, health: 14, speed: .35, attack: 4}
	RavagerType        = hostileMobType{id: "minecraft:ravager", width: 1.95, height: 2.2, health: 100, speed: .3, attack: 12}
)

func hostileTypeFor(id string) hostileMobType {
	switch id {
	case "minecraft:zombie":
		return ZombieType
	case "minecraft:zombie_villager":
		return ZombieVillagerType
	case "minecraft:husk":
		return HuskType
	case "minecraft:skeleton":
		return SkeletonType
	case "minecraft:parched":
		return ParchedType
	case "minecraft:wither_skeleton":
		return WitherSkeletonType
	case "minecraft:stray":
		return StrayType
	case "minecraft:bogged":
		return BoggedType
	case "minecraft:evoker":
		return EvokerType
	case "minecraft:vindicator":
		return VindicatorType
	case "minecraft:pillager":
		return PillagerType
	case "minecraft:vex":
		return VexType
	case "minecraft:ravager":
		return RavagerType
	}
	return hostileMobType{}
}

type hostileState struct {
	health                                                                        *HealthManager
	effects                                                                       *EffectManager
	mc                                                                            *MovementComputer
	random                                                                        vanilla.Random
	speed, attack                                                                 float64
	cooldown, wander, bowCooldown, spellCooldown, hoverTime, doorTicks, cureTicks int
	navigator                                                                     Navigator
	doorPos                                                                       cube.Pos
	dead                                                                          bool
	kind                                                                          string
	baby, canBreakDoors, canPickupLoot                                            bool
	conversionTime                                                                int
	main, head                                                                    item.Stack
	profession, villagerType                                                      string
	villagerLevel, villagerXP, villagerAge                                        int
	offers                                                                        []MerchantOffer
	anchor                                                                        mgl64.Vec3
	anchorSet                                                                     bool
	raidID                                                                        int64
	raidWave                                                                      int
	raidLeader, headDropGuaranteed                                                bool
	patrolLeader, patrolling, hasPatrolTarget                                     bool
	patrolTarget                                                                  cube.Pos
	patrolCooldown                                                                int
	spawnData                                                                     map[string]any
	ridingLinkPending                                                             bool
	freezingTime, strayConversionTime                                             int
	strayConverting                                                               bool
	ai                                                                            MobAI
	weaponGoal                                                                    Goal
	weaponSet, weaponBow, drawingBow                                              bool
	useTicks                                                                      int
}
type HostileMob struct{ *Ent }
type Zombie struct{ *HostileMob }
type ZombieVillager struct{ *HostileMob }
type Skeleton struct{ *HostileMob }
type WitherSkeleton struct{ *HostileMob }
type Stray struct{ *HostileMob }
type Bogged struct{ *HostileMob }
type Evoker struct{ *HostileMob }
type Vindicator struct{ *HostileMob }
type Pillager struct{ *HostileMob }
type Vex struct{ *HostileMob }
type Parched struct{ *HostileMob }
type Husk struct{ *HostileMob }
type Ravager struct{ *HostileMob }

func NewZombie(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ZombieType, hostileConfig{kind: ZombieType.id})
}
func NewZombieVillager(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ZombieVillagerType, hostileConfig{kind: ZombieVillagerType.id})
}
func NewSkeleton(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(SkeletonType, hostileConfig{kind: SkeletonType.id})
}
func NewWitherSkeleton(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(WitherSkeletonType, hostileConfig{kind: WitherSkeletonType.id})
}
func NewHusk(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(HuskType, hostileConfig{kind: HuskType.id})
}
func NewParched(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(ParchedType, hostileConfig{kind: ParchedType.id})
}
func NewStray(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(StrayType, hostileConfig{kind: StrayType.id})
}
func NewBogged(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(BoggedType, hostileConfig{kind: BoggedType.id})
}
func NewEvoker(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(EvokerType, hostileConfig{kind: EvokerType.id})
}
func NewVindicator(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(VindicatorType, hostileConfig{kind: VindicatorType.id})
}
func NewPillager(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(PillagerType, hostileConfig{kind: PillagerType.id})
}
func NewVex(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(VexType, hostileConfig{kind: VexType.id})
}
func NewRavager(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(RavagerType, hostileConfig{kind: RavagerType.id})
}

type hostileNBTConfig struct {
	id   string
	data map[string]any
}

func (c hostileNBTConfig) Apply(data *world.EntityData) {
	hostileTypeFor(c.id).DecodeNBT(c.data, data)
	data.Data.(*hostileState).spawnData = CloneNativeMobNBT(c.data)
}

func hostileConfigFromNBT(id string, spawnData map[string]any) world.EntityConfig {
	if id == "" || hostileTypeFor(id).id != id {
		return nil
	}
	return hostileNBTConfig{id: id, data: CloneNativeMobNBT(spawnData)}
}

// NewHostileWithNBT creates a hostile mob with the selected Java entity spawn-data compound.
func NewHostileWithNBT(opts world.EntitySpawnOpts, id string, spawnData map[string]any) *world.EntityHandle {
	conf := hostileConfigFromNBT(id, spawnData)
	if conf == nil {
		return nil
	}
	return opts.New(hostileTypeFor(id), conf)
}

func (m *HostileMob) state() *hostileState { return m.Behaviour().(*hostileState) }

// UsingItem reports if the mob draws its bow.
func (m *HostileMob) UsingItem() bool               { return m.state().drawingBow }
func (m *HostileMob) SpeciesType() world.EntityType { return hostileTypeFor(m.state().kind) }

func (m *HostileMob) RaidID() int64      { return m.state().raidID }
func (m *HostileMob) RaidWave() int      { return m.state().raidWave }
func (m *HostileMob) IsRaidLeader() bool { return m.state().raidLeader }
func (m *HostileMob) CloseIn(tx *world.Tx) error {
	tx.RaiderDied(m)
	return m.Ent.CloseIn(tx)
}
func (m *HostileMob) HeldItems() (item.Stack, item.Stack) {
	return m.state().main, item.Stack{}
}
func (m *HostileMob) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	return m.state().head, item.Stack{}, item.Stack{}, item.Stack{}
}
func (m *HostileMob) AcceptItem(from world.Entity, tx *world.Tx, ctx *item.UseContext) bool {
	b := m.state()
	if b.kind != "minecraft:zombie_villager" || b.cureTicks > 0 || !m.hasEffect(effect.Weakness) {
		return false
	}
	u, ok := from.(item.User)
	if !ok {
		return false
	}
	main, _ := u.HeldItems()
	if main.Empty() {
		return false
	}
	if _, ok := main.Item().(item.GoldenApple); !ok {
		return false
	}
	ctx.SubtractFromCount(1)
	m.RemoveEffect(effect.Weakness)
	b.cureTicks = 3600 + int(b.random.NextInt(2400))
	return true
}
func (m *HostileMob) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return m.AcceptItem(u, tx, ctx)
}
func (m *HostileMob) tickCure(tx *world.Tx) bool {
	b := m.state()
	if b.kind != "minecraft:zombie_villager" || b.cureTicks <= 0 {
		return false
	}
	b.cureTicks--
	if b.cureTicks > 0 {
		return false
	}
	config := VillagerBehaviourConfig{Health: b.health.Health(), XP: b.villagerXP, Level: b.villagerLevel, Age: b.villagerAge, Profession: b.profession, Type: b.villagerType, Offers: append([]MerchantOffer(nil), b.offers...)}
	opts := world.EntitySpawnOpts{Position: m.Position(), Rotation: m.Rotation()}
	tx.AddEntity(opts.New(VillagerType, config))
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.zombie_villager.converted", Volume: 1, Pitch: 1})
	_ = m.CloseIn(tx)
	return true
}
func (m *HostileMob) Tick(tx *world.Tx, n int64) {
	b := m.state()
	b.effects.Tick(m, tx)
	if b.ridingLinkPending {
		if riding := m.Riding(); riding != nil {
			if vehicle, ok := riding.Entity(tx); ok {
				if camel, ok := vehicle.(*CamelHusk); ok {
					camel.broadcastLink(tx, m.H(), true)
					b.ridingLinkPending = false
				}
			}
		}
	}
	if !m.tickCure(tx) {
		m.Ent.Tick(tx, n)
	}
}
func (b *hostileState) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.cooldown > 0 {
		b.cooldown--
	}
	if b.bowCooldown > 0 {
		b.bowCooldown--
	}
	if b.spellCooldown > 0 {
		b.spellCooldown--
	}
	if b.kind == "minecraft:zombie" || b.kind == "minecraft:zombie_villager" || b.kind == "minecraft:skeleton" || b.kind == "minecraft:stray" || b.kind == "minecraft:bogged" || b.kind == "minecraft:parched" {
		b.burnUndead(e, tx)
	}
	if b.kind == "minecraft:skeleton" && b.tickStrayConversion(e, tx) {
		return nil
	}
	if b.kind == "minecraft:zombie" || b.kind == "minecraft:husk" {
		p := cube.PosFromVec3(e.Position())
		liquid, wet := tx.Liquid(p)
		if wet && liquid.LiquidType() == "water" {
			b.conversionTime++
			if b.conversionTime >= 600 {
				opts := world.EntitySpawnOpts{Position: e.Position(), Rotation: e.Rotation(), SpawnReason: "conversion"}
				if b.kind == "minecraft:husk" {
					tx.AddEntity(NewZombie(opts))
				} else {
					tx.AddEntity(NewDrowned(opts))
				}
				_ = e.CloseIn(tx)
				return nil
			}
		} else if b.conversionTime > 0 {
			b.conversionTime = max(0, b.conversionTime-2)
		}
	}
	b.joinRaid(e, tx)
	if hostileUsesAI(b.kind) {
		return b.aiTick(e)
	}
	var target world.Entity
	best := math.MaxFloat64
	box := cube.Box(-24, -8, -24, 24, 8, 24).Translate(e.Position())
	for candidate := range tx.EntitiesWithin(box) {
		if candidate == e {
			continue
		}
		id := candidate.H().Type().EncodeEntity()
		hostileTarget := id == "minecraft:player"
		if id == "minecraft:villager" || id == "minecraft:wandering_trader" || id == "minecraft:iron_golem" {
			hostileTarget = true
		}
		if !hostileTarget {
			continue
		}
		l, ok := candidate.(Living)
		if !ok || l.Dead() {
			continue
		}
		d := candidate.Position().Sub(e.Position()).Len()
		if d < best && d < 24 {
			target = candidate
			best = d
		}
	}
	if target == nil {
		if b.kind == "minecraft:vex" {
			if !b.anchorSet {
				b.anchor = e.Position()
				b.anchorSet = true
			}
			b.hoverTime++
			p := b.anchor.Add(mgl64.Vec3{math.Sin(float64(b.hoverTime) / 40), .4 + math.Sin(float64(b.hoverTime)/20)*.25, math.Cos(float64(b.hoverTime) / 40)})
			b.move(e, tx, p)
			return nil
		}
		if b.patrolTick(e, tx) {
			return nil
		}
		b.wanderTick(e, tx)
		return nil
	}
	if (b.kind == "minecraft:zombie" || b.kind == "minecraft:husk") && b.canBreakDoors && tx.LocalDifficulty(cube.PosFromVec3(e.Position())).Base == world.DifficultyHard {
		if b.breakDoor(e, tx, target) {
			return nil
		}
	} else {
		b.doorTicks = 0
	}
	if b.kind == "minecraft:evoker" && best <= 12 && b.spellCooldown == 0 {
		if best > 6 {
			for range 3 {
				offset := mgl64.Vec3{float64(b.random.NextFloat())*2 - 1, 0, float64(b.random.NextFloat())*2 - 1}
				tx.AddEntity(NewVex(world.EntitySpawnOpts{Position: e.Position().Add(offset)}))
			}
			b.spellCooldown = 120
		} else {
			spawnEvokerFangs(e, tx, target.Position())
			b.spellCooldown = 60
		}
	}
	ranged := b.kind == "minecraft:skeleton" || b.kind == "minecraft:stray" || b.kind == "minecraft:bogged" || b.kind == "minecraft:parched" || b.kind == "minecraft:pillager"
	if ranged && best > 4 && best < 16 && b.bowCooldown == 0 {
		b.shoot(e, tx, target)
		interval := 40
		difficulty := tx.LocalDifficulty(cube.PosFromVec3(e.Position())).Base
		if difficulty == world.DifficultyHard {
			interval = 20
		}
		if b.kind == "minecraft:bogged" || b.kind == "minecraft:parched" {
			interval = 70
			if difficulty == world.DifficultyHard {
				interval = 50
			}
		}
		b.bowCooldown = interval
	}
	if ranged && best > 1.6 && best < 8 {
		d := e.Position().Sub(target.Position())
		d[1] = 0
		if d.Len() > 0 {
			d = d.Normalize().Mul(b.speed)
		}
		v := e.data.Vel
		v[0], v[2] = d[0], d[2]
		m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, e.data.Rot, tx)
		e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
		return m
	}
	if best < 1.6 && b.cooldown == 0 {
		damage := b.attack
		if w, ok := b.main.Item().(item.Weapon); ok {
			damage += w.AttackDamage()
		}
		if l, ok := target.(Living); ok {
			dealt, vulnerable := l.Hurt(damage, AttackDamageSource{Attacker: e})
			if l.Dead() && (b.kind == "minecraft:zombie" || b.kind == "minecraft:zombie_villager" || b.kind == "minecraft:husk") {
				(&HostileMob{Ent: e}).convertVillager(tx, target)
			}
			if b.kind == "minecraft:husk" && b.main.Empty() && vulnerable && dealt > 0 {
				ticks := 140 * int(tx.LocalDifficulty(cube.PosFromVec3(e.Position())).Effective)
				l.AddEffect(effect.New(effect.Hunger, 0, time.Duration(ticks)*time.Second/20))
			}
			force, height := .4, .2
			if b.kind == "minecraft:ravager" {
				force, height = 1.2, .6
			}
			l.KnockBack(e.Position(), force, height)
		}
		b.cooldown = 20
	}
	if best > 1.4 {
		b.move(e, tx, target.Position())
	} else {
		b.stop(e, tx)
	}
	return nil
}

func hostileRaidMob(kind string) bool {
	switch kind {
	case "minecraft:evoker", "minecraft:vindicator", "minecraft:pillager", "minecraft:ravager":
		return true
	}
	return false
}

func (b *hostileState) joinRaid(e *Ent, tx *world.Tx) {
	raid := tx.RaidForRaider(e)
	if raid != nil {
		if !raid.Active() {
			tx.RaiderDied(e)
			b.raidID, b.raidWave, b.raidLeader = 0, 0, false
			return
		}
		b.raidID = raid.ID()
		return
	}
	if b.raidID == 0 && hostileRaidMob(b.kind) {
		if nearby := tx.RaidAt(cube.PosFromVec3(e.Position())); nearby != nil {
			b.raidID = nearby.ID()
		}
	}
	if b.raidID == 0 {
		return
	}
	if tx.RegisterRaider(b.raidID, e) {
		return
	}
	b.raidID, b.raidWave, b.raidLeader = 0, 0, false
}
func (b *hostileState) shoot(e *Ent, tx *world.Tx, target world.Entity) {
	d := target.Position().Add(mgl64.Vec3{0, .9, 0}).Sub(e.Position().Add(mgl64.Vec3{0, 1.4, 0}))
	if d.Len() == 0 {
		return
	}
	d = d.Normalize().Mul(1.6)
	opts := world.EntitySpawnOpts{Position: e.Position().Add(mgl64.Vec3{0, 1.4, 0}), Velocity: d}
	switch b.kind {
	case "minecraft:stray":
		tx.AddEntity(NewTippedArrow(opts, e, potion.Slowness()))
	case "minecraft:bogged":
		tx.AddEntity(NewTippedArrow(opts, e, potion.Poison()))
	case "minecraft:parched":
		tx.AddEntity(NewTippedArrow(opts, e, potion.LongWeakness()))
	default:
		tx.AddEntity(NewArrow(opts, e))
	}
}
func (b *hostileState) move(e *Ent, tx *world.Tx, p mgl64.Vec3) {
	d := p.Sub(e.data.Pos)
	if b.kind != "minecraft:vex" {
		d[1] = 0
	}
	if d.Len() > 0 {
		d = d.Normalize()
	}
	v := e.data.Vel
	v[0], v[2] = d[0]*b.speed, d[2]*b.speed
	if b.kind == "minecraft:vex" {
		v[1] = d[1] * b.speed
	}
	r := e.data.Rot
	r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, r, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
}
func (b *hostileState) stop(e *Ent, tx *world.Tx) {
	v := e.data.Vel
	v[0], v[2] = 0, 0
	m := b.mc.TickMovement(e, e.data.Pos, v, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
}
func (b *hostileState) breakDoor(e *Ent, tx *world.Tx, target world.Entity) bool {
	d := target.Position().Sub(e.Position())
	dx, dz := 0, 0
	if d[0] > .2 {
		dx = 1
	} else if d[0] < -.2 {
		dx = -1
	}
	if d[2] > .2 {
		dz = 1
	} else if d[2] < -.2 {
		dz = -1
	}
	p := cube.PosFromVec3(e.Position()).Add(cube.Pos{dx, 0, dz})
	for _, check := range []cube.Pos{p, p.Side(cube.FaceUp)} {
		door, ok := tx.Block(check).(block.WoodDoor)
		if !ok || door.Open {
			continue
		}
		lower := check
		if door.Top {
			lower = check.Side(cube.FaceDown)
		}
		if lower != b.doorPos {
			b.doorPos = lower
			b.doorTicks = 0
		}
		b.doorTicks++
		if b.doorTicks >= 240 {
			tx.SetBlock(lower, nil, nil)
			tx.SetBlock(lower.Side(cube.FaceUp), nil, nil)
			b.doorTicks = 0
			return true
		}
		b.stop(e, tx)
		return true
	}
	b.doorTicks = 0
	return false
}
func (b *hostileState) wanderTick(e *Ent, tx *world.Tx) {
	if b.wander > 0 {
		b.wander--
	}
	if b.navigator.Index >= len(b.navigator.Path) && b.wander == 0 {
		b.wander = 50
		start := cube.PosFromVec3(e.Position())
		goal := start.Add(cube.Pos{int(b.random.NextInt(11)) - 5, 0, int(b.random.NextInt(11)) - 5})
		b.navigator.MoveTo(tx, start, goal, GroundProfile{MaxVisitedNodes: 100}, navigatorTick(e))
	}
	if p, ok := b.navigator.Tick(e.Position(), navigatorTick(e), .6); ok {
		if p.Sub(e.Position()).Len() < .3 {
			b.navigator.Index++
		} else {
			b.move(e, tx, p)
		}
	} else {
		b.stop(e, tx)
	}
}
func (m *HostileMob) Health() float64        { return m.state().health.Health() }
func (m *HostileMob) MaxHealth() float64     { return m.state().health.MaxHealth() }
func (m *HostileMob) SetMaxHealth(v float64) { m.state().health.SetMaxHealth(v) }
func (m *HostileMob) Dead() bool             { return m.state().dead || m.Health() <= 0 }
func (m *HostileMob) Hurt(d float64, s world.DamageSource) (float64, bool) {
	if m.Dead() || d <= 0 || (s.Fire() && (m.hasEffect(effect.FireResistance) || m.state().kind == "minecraft:wither_skeleton")) {
		return 0, false
	}
	b := m.state()
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	b.ai.Hurt(s)
	m.viewAction(HurtAction{})
	onMobHurt(m.tx, m)
	if m.Dead() {
		b.dead = true
		m.viewAction(DeathAction{})
		m.tx.RaiderKilled(m, raidDamageAttacker(s))
		m.dropLoot(s)
		_ = m.CloseIn(m.tx)
	}
	return d, true
}
func (m *HostileMob) Heal(h float64, _ world.HealingSource) float64 {
	if h <= 0 || m.Dead() {
		return 0
	}
	old := m.Health()
	m.state().health.AddHealth(h)
	return m.Health() - old
}
func (m *HostileMob) KnockBack(src mgl64.Vec3, f, h float64) {
	d := m.Position().Sub(src)
	d[1] = 0
	if d.Len() > 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] = h
	m.SetVelocity(d)
}
func (m *HostileMob) AddEffect(e effect.Effect) {
	b := m.state()
	if (e.Type() == effect.Poison || e.Type() == effect.FatalPoison) && hostileUndead(b.kind) {
		return
	}
	if b.kind == "minecraft:parched" && e.Type() == effect.Weakness {
		return
	}
	b.effects.Add(e, m)
}
func hostileUndead(kind string) bool {
	switch kind {
	case "minecraft:zombie", "minecraft:zombie_villager", "minecraft:husk", "minecraft:skeleton", "minecraft:wither_skeleton", "minecraft:stray", "minecraft:bogged", "minecraft:parched", "minecraft:vex", "minecraft:drowned":
		return true
	}
	return false
}
func (m *HostileMob) RemoveEffect(t effect.Type)   { m.state().effects.Remove(t, m) }
func (m *HostileMob) Effects() []effect.Effect     { return m.state().effects.Effects() }
func (m *HostileMob) Speed() float64               { return m.state().speed }
func (m *HostileMob) SetSpeed(v float64)           { m.state().speed = v }
func (m *HostileMob) hasEffect(t effect.Type) bool { _, ok := m.state().effects.Effect(t); return ok }
func (m *HostileMob) viewAction(a world.EntityAction) {
	v := m.tx.Viewers(m.Position())
	for _, x := range v {
		x.ViewEntityAction(m, a)
	}
	m.tx.ReleaseViewers(v)
}
func (m *HostileMob) dropLoot(src world.DamageSource) {
	dropAllDeathLoot(m.tx, m, src)
}

// headDropChance is the Java drop chance of the head slot: a raid banner is
// preserved, other headgear uses the default 8.5%.
func (m *HostileMob) headDropChance() float32 {
	if m.state().headDropGuaranteed {
		return 2
	}
	return .085
}

// dropCustomDeathLoot ports Mob.dropCustomDeathLoot for the mainhand and
// head slots the hostile mob represents.
func (m *HostileMob) dropCustomDeathLoot(killedByPlayer bool, looting int) {
	b := m.state()
	dropMobEquipment(m.tx, m, b.main, .085, killedByPlayer, looting)
	dropMobEquipment(m.tx, m, b.head, m.headDropChance(), killedByPlayer, looting)
}

// experienceEquipment counts the worn items that may drop, which
// Mob.getBaseExperienceReward rewards.
func (m *HostileMob) experienceEquipment() int {
	b, n := m.state(), 0
	if !b.main.Empty() {
		n++
	}
	if !b.head.Empty() && m.headDropChance() <= 1 {
		n++
	}
	return n
}

// VillagerData returns the profession and biome type a zombie villager keeps
// from the villager it was.
func (m *HostileMob) VillagerData() (profession, villagerType string) {
	b := m.state()
	return b.profession, b.villagerType
}

// IsCaptain ports Raider.isCaptain: a raid or patrol leader wearing the
// ominous banner.
func (m *HostileMob) IsCaptain() bool {
	b := m.state()
	if !b.raidLeader && !b.patrolLeader || b.head.Empty() {
		return false
	}
	return b.head.Comparable(ominousBannerStack())
}

// tickStrayConversion ports the skeleton's freezing ConversionTracker: after
// 140 ticks in powder snow a skeleton shakes for 300 ticks and then becomes a
// stray that keeps its equipment. It reports whether the skeleton converted.
func (b *hostileState) tickStrayConversion(e *Ent, tx *world.Tx) bool {
	if !e.data.WasInPowderSnow {
		b.freezingTime = -1
		if b.strayConverting {
			b.strayConverting = false
			e.updateState()
		}
		return false
	}
	if !b.strayConverting {
		if b.freezingTime++; b.freezingTime >= 140 {
			b.strayConversionTime, b.strayConverting = 300, true
			e.updateState()
		}
		return false
	}
	if b.strayConversionTime--; b.strayConversionTime >= 0 {
		return false
	}
	opts := world.EntitySpawnOpts{Position: e.Position(), Rotation: e.Rotation(), SpawnReason: "conversion"}
	tx.AddEntity(opts.New(StrayType, hostileConfig{kind: StrayType.id, main: b.main, head: b.head, canPickupLoot: b.canPickupLoot}))
	tx.PlaySound(e.Position(), sound.NativeEvent{SoundType: "convert_to_stray", ExtraData: -1})
	_ = e.CloseIn(tx)
	return true
}

// Shaking reports whether the skeleton shakes while it converts to a stray.
func (m *HostileMob) Shaking() bool { return m.state().strayConverting }

// burnUndead ports Mob.burnUndead: in a sun burn tick a helmet takes the heat
// and wears down, otherwise the mob catches fire for eight seconds.
func (b *hostileState) burnUndead(e *Ent, tx *world.Tx) {
	if !sunBurnTick(e, tx) {
		return
	}
	if b.head.Empty() {
		if e.OnFireDuration() < 8*time.Second {
			e.SetOnFire(8 * time.Second)
		}
		return
	}
	if _, durable := b.head.Item().(item.Durable); durable {
		b.head = b.head.Damage(rand.IntN(2))
	}
}

// sunBurnTick ports Mob.isSunBurnTick: in daylight a mob that sees the sky and
// is not in water, rain or powder snow burns with a chance growing with the
// brightness at its eyes.
func sunBurnTick(e *Ent, tx *world.Tx) bool {
	if tx.World().Dimension() != world.Overworld || tx.IsDarkOutside() {
		return false
	}
	eye := cube.PosFromVec3(EyePosition(e))
	brightness := float32(max(int(tx.SkyLight(eye))-tx.SkyDarken(), int(tx.Light(eye)))) / 15
	brightness /= 4 - 3*brightness
	if brightness <= 0.5 || rand.Float32()*30 >= (brightness-0.4)*2 {
		return false
	}
	feet := cube.PosFromVec3(e.Position())
	if liquid, ok := tx.Liquid(feet); ok && liquid.LiquidType() == "water" || tx.RainingAt(feet) || e.data.InPowderSnow || e.data.WasInPowderSnow {
		return false
	}
	return tx.SkyLight(eye) >= 15
}
func (m *HostileMob) convertVillager(tx *world.Tx, target world.Entity) {
	if target.H().Type().EncodeEntity() != "minecraft:villager" {
		return
	}
	difficulty := tx.LocalDifficulty(cube.PosFromVec3(target.Position())).Base
	if difficulty != world.DifficultyNormal && difficulty != world.DifficultyHard {
		return
	}
	if difficulty == world.DifficultyNormal && !m.state().random.NextBoolean() {
		return
	}
	villager, ok := target.(*Villager)
	if !ok {
		return
	}
	v := villager.behaviour()
	opts := world.EntitySpawnOpts{Position: target.Position(), Rotation: target.Rotation(), SpawnReason: "conversion"}
	config := hostileConfig{kind: ZombieVillagerType.id, profession: v.profession, villagerType: v.villagerType, villagerLevel: v.level, villagerXP: v.xp, villagerAge: v.age, offers: append([]MerchantOffer(nil), v.offers...)}
	tx.AddEntity(opts.New(ZombieVillagerType, config))
}

type zombieSpawnGroupData struct {
	baby                  bool
	canSpawnJockey        bool
	triedToSpawnCamelHusk bool
}

func finalizeZombieSpawn(reason string, difficulty world.LocalDifficulty, r *vanilla.Random, withEquipment bool, group *world.EntitySpawnGroup) (baby, canBreakDoors, canPickupLoot bool, main item.Stack) {
	if r == nil {
		return
	}
	if reason != "conversion" {
		canPickupLoot = float64(r.NextFloat()) < .55*float64(difficulty.SpecialMultiplier())
	}
	data := (*zombieSpawnGroupData)(nil)
	if group != nil {
		data, _ = group.Data.(*zombieSpawnGroupData)
	}
	if data == nil {
		data = &zombieSpawnGroupData{baby: r.NextFloat() < .05, canSpawnJockey: true}
		if group != nil {
			group.Data = data
		}
	}
	baby = data.baby
	if baby && data.canSpawnJockey {
		if r.NextFloat() >= .05 {
			_ = r.NextFloat()
		}
	}
	canBreakDoors = float64(r.NextFloat()) < float64(difficulty.SpecialMultiplier())*.1
	if withEquipment && reason != "conversion" {
		chance := .01
		if difficulty.Base == world.DifficultyHard {
			chance = .05
		}
		if float64(r.NextFloat()) < chance {
			switch r.NextInt(6) {
			case 0:
				main = item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
			case 1:
				main = item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1)
			default:
				main = item.NewStack(item.Shovel{Tier: item.ToolTierIron}, 1)
			}
		}
	}
	return
}
func raidDamageAttacker(src world.DamageSource) world.Entity {
	switch source := src.(type) {
	case AttackDamageSource:
		return source.Attacker
	case SpearDamageSource:
		return source.Attacker
	case MaceSmashDamageSource:
		return source.Attacker
	case ProjectileDamageSource:
		return source.Owner
	case *AttackDamageSource:
		if source != nil {
			return source.Attacker
		}
	case *SpearDamageSource:
		if source != nil {
			return source.Attacker
		}
	case *MaceSmashDamageSource:
		if source != nil {
			return source.Attacker
		}
	case *ProjectileDamageSource:
		if source != nil {
			return source.Owner
		}
	}
	return nil
}

func (m *HostileMob) FinalizeSpawn(reason string, r *vanilla.Random) {
	if r == nil {
		return
	}
	b := m.state()
	switch b.kind {
	case "minecraft:zombie", "minecraft:zombie_villager", "minecraft:husk":
		if m.tx == nil {
			return
		}
		difficulty := m.tx.LocalDifficulty(cube.PosFromVec3(m.Position()))
		baby, doors, pickup, main := finalizeZombieSpawn(reason, difficulty, r, true, m.H().SpawnGroup())
		b.baby, b.canBreakDoors, b.canPickupLoot = baby, doors, pickup
		if !main.Empty() {
			b.main = main
		}
		if b.baby {
			b.speed = hostileTypeFor(b.kind).speed * 1.5
		}
		if b.kind == "minecraft:husk" {
			if reason != "conversion" {
				b.canPickupLoot = float64(r.NextFloat()) < .55*float64(difficulty.SpecialMultiplier())
			}
			m.finalizeHuskCamel(reason, r)
		}
	case "minecraft:skeleton", "minecraft:stray", "minecraft:bogged", "minecraft:parched":
		if reason != "conversion" {
			if m.tx != nil {
				b.canPickupLoot = float64(r.NextFloat()) < .55*float64(m.tx.LocalDifficulty(cube.PosFromVec3(m.Position())).SpecialMultiplier())
			}
			b.main = item.NewStack(item.Bow{}, 1)
		}
	case "minecraft:pillager":
		b.main = item.NewStack(item.Crossbow{}, 1)
	case "minecraft:wither_skeleton":
		b.main = item.NewStack(item.Sword{Tier: item.ToolTierStone}, 1)
	case "minecraft:vindicator":
		b.main = item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)
	}
	// PatrollingMonster.finalizeSpawn: outside patrols, events and
	// structures, 6% of raiders that can lead become patrol leaders; patrol
	// spawns patrol.
	if hostileRaidMob(b.kind) && b.kind != "minecraft:ravager" && reason != "patrol" && reason != "event" && reason != "structure" && r.NextFloat() < .06 {
		b.patrolLeader = true
	}
	if reason == "patrol" {
		b.patrolling = true
	}
	if b.raidLeader || b.patrolLeader {
		b.head = ominousBannerStack()
		b.headDropGuaranteed = true
	}
}

func (m *HostileMob) finalizeHuskCamel(reason string, r *vanilla.Random) {
	group := m.H().SpawnGroup()
	if group == nil {
		return
	}
	data, ok := group.Data.(*zombieSpawnGroupData)
	if !ok || data == nil {
		return
	}
	if reason != "natural" {
		data.triedToSpawnCamelHusk = true
	}
	if data.triedToSpawnCamelHusk {
		return
	}
	pos := m.Position()
	if !naturalCollisionFree(m.tx, CamelHuskType.BBox(nil).Translate(pos)) {
		return
	}
	data.triedToSpawnCamelHusk = true
	if r.NextFloat() >= .1 {
		return
	}
	m.state().main = item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1)
	opts := world.EntitySpawnOpts{Position: pos, Rotation: m.Rotation(), SpawnReason: "natural"}
	entity := m.tx.AddEntity(NewCamelHusk(opts))
	var camel *Mount
	switch v := entity.(type) {
	case *CamelHusk:
		camel = v.Mount
	case *Mount:
		camel = v
	}
	if camel == nil {
		return
	}
	camel.b().passengers[0] = m.H()
	seatRider(m, camel)
	m.state().ridingLinkPending = true
	rider := m.tx.AddEntity(NewParched(opts))
	if parched, ok := rider.(*Parched); ok {
		camel.b().passengers[1] = parched.H()
		seatRider(parched, camel)
		parched.state().ridingLinkPending = true
	}
}
func hostileNativeStack(s item.Stack, previous NativeMobItem) NativeMobItem {
	entry := mountNativeStack(s, previous)
	if banner, ok := s.Item().(block.Banner); ok {
		if entry.Raw == nil {
			entry.Raw = make(map[string]any)
		}
		for key, value := range banner.EncodeNBT() {
			if key != "id" {
				entry.Raw[key] = value
			}
		}
	}
	return entry
}
func hostileStackFromNative(native NativeMobItem) item.Stack {
	if native.Name == "" || native.Count == 0 {
		return item.Stack{}
	}
	it, ok := world.ItemByName(native.Name, int16(native.Damage))
	if !ok {
		return item.Stack{}
	}
	if banner, ok := it.(block.Banner); ok {
		raw := native.Raw
		if tag, ok := native.Raw["tag"].(map[string]any); ok {
			raw = tag
		}
		if _, patterns := raw["Patterns"]; patterns {
			banner = banner.DecodeNBT(raw).(block.Banner)
		}
		it = banner
	}
	return item.NewStack(it, int(native.Count))
}

func ominousBannerStack() item.Stack {
	return item.NewStack(block.Banner{Colour: item.ColourWhite(), Illager: true, Patterns: []block.BannerPatternLayer{
		{Type: block.BorderBannerPattern(), Colour: item.ColourLightGrey()},
		{Type: block.StripeMiddleBannerPattern(), Colour: item.ColourBlack()},
		{Type: block.HalfHorizontalBannerPattern(), Colour: item.ColourLightGrey()},
		{Type: block.CircleBannerPattern(), Colour: item.ColourLightGrey()},
		{Type: block.BorderBannerPattern(), Colour: item.ColourBlack()},
	}}, 1)
}

var _ world.EntityType = hostileMobType{}
var _ Living = (*HostileMob)(nil)
