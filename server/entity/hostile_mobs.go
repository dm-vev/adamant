package entity

import (
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/google/uuid"
	"hash/fnv"
	"maps"
	"math/rand/v2"
	"time"

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
		if _, ok := java["DrownedConversionTime"]; ok {
			if n := nbtconv.Int32(java, "DrownedConversionTime"); n != -1 {
				c.drownedConversionTime, c.drownedConverting = int(n), true
			}
		}
		c.raidID, c.raidWave, c.raidLeader = nbtconv.Int64(java, "RaidID"), int(nbtconv.Int32(java, "RaidWave")), nbtconv.Bool(java, "RaidLeader")
		c.headDropGuaranteed = nbtconv.Bool(java, "RaidBannerDrop")
		c.patrolLeader, c.patrolling = nbtconv.Bool(java, "PatrolLeader"), nbtconv.Bool(java, "Patrolling")
		c.freezingTime = int(nbtconv.Int32(java, "FreezingTime"))
		if _, ok := java["StrayConversionTime"]; ok {
			if n := nbtconv.Int32(java, "StrayConversionTime"); n != -1 {
				c.strayConversionTime, c.strayConverting = int(n), true
			}
		}
		c.johnny = nbtconv.Bool(java, "Johnny")
		c.spellTicks = int(nbtconv.Int32(java, "SpellTicks"))
		c.ravagerAttack, c.ravagerStun, c.ravagerRoar = int(nbtconv.Int32(java, "AttackTick")), int(nbtconv.Int32(java, "StunTick")), int(nbtconv.Int32(java, "RoarTick"))
		if _, ok := java["life_ticks"]; ok {
			c.vexLimitedLife, c.vexLifeTicks = true, int(nbtconv.Int32(java, "life_ticks"))
		}
		if bound, ok := java["bound_pos"].([]int32); ok && len(bound) == 3 {
			c.vexBound, c.vexHasBound = cube.Pos{int(bound[0]), int(bound[1]), int(bound[2])}, true
		}
		if target, ok := java["patrol_target"].([]int32); ok && len(target) == 3 {
			c.patrolTarget, c.hasPatrolTarget = cube.Pos{int(target[0]), int(target[1]), int(target[2])}, true
		}
		if data, ok := java["SpawnData"].(map[string]any); ok {
			c.spawnData = CloneNativeMobNBT(data)
		}
	}
	c.cureTicks = int(nbtconv.Int32(n, "ConversionTime"))
	if java := ReadNativeMobJavaState(n); java != nil {
		c.cureStarter = nbtconv.String(java, "ConversionPlayer")
	}
	if source, ok := n["equipment"].(map[string]any); ok {
		for slot, name := range equipmentSlotNames {
			if raw, ok := source[name].(map[string]any); ok {
				*c.equipment(slot), _ = item.StackFromStructureNBT(raw)
			}
		}
	}
	equipment := ReadNativeMobEquipment(n)
	if _, native := n["Mainhand"]; native {
		c.main = item.Stack{}
		if len(equipment.Mainhand) > 0 {
			c.main = item.ReadNBT(equipment.Mainhand[0].Raw, nil)
		}
	}
	if _, native := n["Offhand"]; native {
		c.off = item.Stack{}
		if len(equipment.Offhand) > 0 {
			c.off = item.ReadNBT(equipment.Offhand[0].Raw, nil)
		}
	}
	for i, native := range equipment.Armor {
		if i < 4 {
			*c.equipment(slotHead - i) = hostileStackFromNative(native)
		}
	}
	// Pre-1.21.5 Java lists ArmorItems from the feet up and HandItems from
	// the main hand.
	for list, slots := range map[string][]int{"ArmorItems": {slotFeet, slotLegs, slotChest, slotHead}, "HandItems": {slotMainhand, slotOffhand}} {
		if raw, ok := n[list].([]any); ok && len(raw) == len(slots) {
			for i, slot := range slots {
				if m, ok := raw[i].(map[string]any); ok && c.equipment(slot).Empty() {
					if stack, err := item.StackFromStructureNBT(m); err == nil {
						*c.equipment(slot) = stack
					}
				}
			}
		}
	}
	c.dropChances = readDropChances(n)
	if java := ReadNativeMobJavaState(n); java != nil && c.dropChances == nil {
		c.dropChances = readDropChances(java)
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
	equipment.Offhand = []NativeMobItem{nativeMobItemFromStack(b.off)}
	armor := make([]NativeMobItem, 4)
	copy(armor, equipment.Armor)
	for i := range armor {
		if stack := *b.equipment(slotHead - i); stack.Empty() {
			armor[i] = nativeMobItemFromStack(stack)
		} else {
			armor[i] = hostileNativeStack(stack, armor[i])
		}
	}
	equipment.Armor = armor
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
		"PatrolLeader": b.patrolLeader, "Patrolling": b.patrolling, "FreezingTime": int32(b.freezingTime), "StrayConversionTime": int32(-1), "DrownedConversionTime": int32(-1)}
	if b.drownedConverting {
		javaState["DrownedConversionTime"] = int32(b.drownedConversionTime)
	}
	if b.cureStarter != "" {
		javaState["ConversionPlayer"] = b.cureStarter
	}
	if b.strayConverting {
		javaState["StrayConversionTime"] = int32(b.strayConversionTime)
	}
	if len(b.dropChances) != 0 {
		javaState["drop_chances"] = encodeDropChances(b.dropChances)
	}
	if b.hasPatrolTarget {
		javaState["patrol_target"] = []int32{int32(b.patrolTarget[0]), int32(b.patrolTarget[1]), int32(b.patrolTarget[2])}
	}
	if b.spawnData != nil {
		javaState["SpawnData"] = CloneNativeMobNBT(b.spawnData)
	}
	switch b.kind {
	case "minecraft:vindicator":
		if b.johnny {
			javaState["Johnny"] = true
		}
	case "minecraft:evoker":
		javaState["SpellTicks"] = int32(b.spellTicks)
	case "minecraft:ravager":
		javaState["AttackTick"], javaState["StunTick"], javaState["RoarTick"] = int32(b.ravagerAttack), int32(b.ravagerStun), int32(b.ravagerRoar)
	case "minecraft:vex":
		if b.vexLimitedLife {
			javaState["life_ticks"] = int32(b.vexLifeTicks)
		}
		if b.vexHasBound {
			javaState["bound_pos"] = []int32{int32(b.vexBound[0]), int32(b.vexBound[1]), int32(b.vexBound[2])}
		}
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
	cureStarter                                             string
	drownedConversionTime                                   int
	drownedConverting                                       bool
	main, off, feet, legs, chest, head                      item.Stack
	dropChances                                             map[int]float32
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
	johnny, vexLimitedLife, vexHasBound                     bool
	spellTicks, vexLifeTicks                                int
	ravagerAttack, ravagerStun, ravagerRoar                 int
	vexBound                                                cube.Pos
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
		conversionTime: c.conversionTime, drownedConversionTime: c.drownedConversionTime, drownedConverting: c.drownedConverting, cureTicks: c.cureTicks, cureStarter: c.cureStarter, main: c.main, off: c.off, feet: c.feet, legs: c.legs, chest: c.chest, head: c.head, dropChances: maps.Clone(c.dropChances), profession: c.profession, villagerType: c.villagerType,
		raidID: c.raidID, raidWave: c.raidWave, raidLeader: c.raidLeader, headDropGuaranteed: c.headDropGuaranteed, spawnData: CloneNativeMobNBT(c.spawnData),
		raiderState:  raiderState{patrolLeader: c.patrolLeader, patrolling: c.patrolling, patrolTarget: c.patrolTarget, hasPatrolTarget: c.hasPatrolTarget},
		freezingTime: c.freezingTime, strayConversionTime: c.strayConversionTime, strayConverting: c.strayConverting,
		johnny: c.johnny, spellTicks: c.spellTicks, vexLimitedLife: c.vexLimitedLife, vexLifeTicks: c.vexLifeTicks, vexBound: c.vexBound, vexHasBound: c.vexHasBound,
		ravagerAttack: c.ravagerAttack, ravagerStun: c.ravagerStun, ravagerRoar: c.ravagerRoar,
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
	EvokerType         = hostileMobType{id: "minecraft:evoker", width: .6, height: 1.95, health: 24, speed: .5, attack: 3}
	VindicatorType     = hostileMobType{id: "minecraft:vindicator", width: .6, height: 1.95, health: 24, speed: .35, attack: 5}
	PillagerType       = hostileMobType{id: "minecraft:pillager", width: .6, height: 1.95, health: 24, speed: .35, attack: 5, ranged: true}
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
	health                                 *HealthManager
	effects                                *EffectManager
	mc                                     *MovementComputer
	random                                 vanilla.Random
	speed, attack                          float64
	cureTicks                              int
	cureStarter                            string
	dead                                   bool
	kind                                   string
	baby, canBreakDoors, canPickupLoot     bool
	conversionTime                         int
	drownedConversionTime                  int
	drownedConverting                      bool
	main, off, feet, legs, chest, head     item.Stack
	dropChances                            map[int]float32
	profession, villagerType               string
	villagerLevel, villagerXP, villagerAge int
	offers                                 []MerchantOffer
	raidID                                 int64
	raidWave                               int
	raidLeader, headDropGuaranteed         bool
	raiderState
	spawnData                         map[string]any
	ridingLinkPending                 bool
	freezingTime, strayConversionTime int
	strayConverting                   bool
	ai                                MobAI
	weaponGoal                        Goal
	weaponSet, weaponBow, drawingBow  bool
	useTicks                          int

	// The state of the Raider kinds: celebrating a won raid, the crossbow
	// of a pillager, the spells of an evoker, the owner and life of a vex
	// and the attacks of a ravager.
	johnny                                   bool
	chargingCrossbow, crossbowCharged        bool
	spellTicks                               int
	spell                                    illagerSpell
	wololoTarget                             *world.EntityHandle
	vexOwner                                 *world.EntityHandle
	vexBound                                 cube.Pos
	vexHasBound, vexLimitedLife, vexCharging bool
	vexLifeTicks                             int
	vexMove                                  vexMoveControl
	ravagerAttack, ravagerStun, ravagerRoar  int
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
func (m *HostileMob) UsingItem() bool {
	return m.state().drawingBow || !m.state().ai.useItem.Empty()
}
func (m *HostileMob) SpeciesType() world.EntityType { return hostileTypeFor(m.state().kind) }

func (m *HostileMob) RaidID() int64      { return m.state().raidID }
func (m *HostileMob) RaidWave() int      { return m.state().raidWave }
func (m *HostileMob) IsRaidLeader() bool { return m.state().raidLeader }
func (m *HostileMob) CloseIn(tx *world.Tx) error {
	tx.RaiderDied(m)
	return m.Ent.CloseIn(tx)
}
func (m *HostileMob) HeldItems() (item.Stack, item.Stack) {
	return m.state().main, m.state().off
}
func (m *HostileMob) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	b := m.state()
	return b.head, b.chest, b.legs, b.feet
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
	// ZombieVillager.startConverting: the cure takes three to five minutes,
	// during which the zombie villager is strong.
	b.cureTicks = 3600 + int(b.random.NextInt(2401))
	b.cureStarter = from.H().UUID().String()
	m.RemoveEffect(effect.Weakness)
	m.AddEffect(effect.New(effect.Strength, 1, time.Duration(b.cureTicks)*time.Second/20))
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.zombie_villager.cure", Volume: 1 + float64(b.random.NextFloat()), Pitch: float64(b.random.NextFloat())*.7 + .3})
	m.updateState()
	return true
}
func (m *HostileMob) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return m.AcceptItem(u, tx, ctx)
}

// tickCure ports ZombieVillager.tick while it converts: the cure goes
// faster near beds and iron bars, and turns the zombie villager into the
// villager it was.
func (m *HostileMob) tickCure(tx *world.Tx) bool {
	b := m.state()
	if b.kind != "minecraft:zombie_villager" || b.cureTicks <= 0 || m.Dead() {
		return false
	}
	if b.cureTicks -= m.cureProgress(tx); b.cureTicks > 0 {
		return false
	}
	m.finishCure(tx)
	return true
}

// cureProgress ports ZombieVillager.getConversionProgress: now and then each
// of up to 14 blocks near that speed curing up speeds it up.
func (m *HostileMob) cureProgress(tx *world.Tx) int {
	b, amount := m.state(), 1
	if b.random.NextFloat() >= .01 {
		return amount
	}
	p, special := m.Position(), 0
	for x := int(p[0]) - 4; x < int(p[0])+4 && special < 14; x++ {
		for y := int(p[1]) - 4; y < int(p[1])+4 && special < 14; y++ {
			for z := int(p[2]) - 4; z < int(p[2])+4 && special < 14; z++ {
				if blockInTag(tx.Block(cube.Pos{x, y, z}), "speeds_up_zombie_villager_curing") {
					if b.random.NextFloat() < .3 {
						amount++
					}
					special++
				}
			}
		}
	}
	return amount
}

// finishCure ports ZombieVillager.finishConversion: the villager keeps the
// data, trades and experience of the zombie villager, drops only the items
// it picked up, and the player who cured it gains its favour.
func (m *HostileMob) finishCure(tx *world.Tx) {
	b := m.state()
	for slot := range equipmentSlots {
		if stack := *b.equipment(slot); !stack.Empty() && b.dropChance(slot) > 1 {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.Position()}, stack))
		}
		*b.equipment(slot) = item.Stack{}
	}
	config := VillagerBehaviourConfig{XP: b.villagerXP, Level: b.villagerLevel, Age: b.villagerAge, Profession: b.profession, Type: b.villagerType, Offers: append([]MerchantOffer(nil), b.offers...)}
	opts := world.EntitySpawnOpts{Position: m.Position(), Rotation: m.Rotation(), SpawnReason: "conversion"}
	converted := convertMob(tx, m, opts.New(VillagerType, config), conversion{})
	villager, ok := converted.(*Villager)
	if !ok {
		return
	}
	if id, err := uuid.Parse(b.cureStarter); err == nil {
		if p, ok := tx.EntityByUUID(id); ok && isPlayerEntity(p) {
			villager.OnReputationEventFrom("zombie_villager_cured", p)
		}
	}
	villager.AddEffect(effect.New(effect.Nausea, 1, 10*time.Second))
	tx.PlaySound(m.Position(), sound.Custom{Name: "entity.zombie_villager.converted", Volume: 1, Pitch: 1})
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
	if b.kind == "minecraft:zombie" || b.kind == "minecraft:zombie_villager" || b.kind == "minecraft:skeleton" || b.kind == "minecraft:stray" || b.kind == "minecraft:bogged" || b.kind == "minecraft:parched" {
		b.burnUndead(e, tx)
	}
	if b.kind == "minecraft:skeleton" && b.tickStrayConversion(e, tx) {
		return nil
	}
	if (b.kind == "minecraft:zombie" || b.kind == "minecraft:husk" || b.kind == "minecraft:zombie_villager") && b.tickDrowning(e, tx) {
		return nil
	}
	b.joinRaid(e, tx)
	return b.aiTick(e)
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
		// Raiders stay with a raid they won to celebrate it.
		if raid.Status() == world.RaidDefeat {
			b.raidID = raid.ID()
			return
		}
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
func (m *HostileMob) Health() float64        { return m.state().health.Health() }
func (m *HostileMob) MaxHealth() float64     { return m.state().health.MaxHealth() }
func (m *HostileMob) SetMaxHealth(v float64) { m.state().health.SetMaxHealth(v) }
func (m *HostileMob) Dead() bool             { return m.state().dead || m.Health() <= 0 }
func (m *HostileMob) Hurt(d float64, s world.DamageSource) (float64, bool) {
	if m.Dead() || d <= 0 || (s.Fire() && (m.hasEffect(effect.FireResistance) || m.state().kind == "minecraft:wither_skeleton")) {
		return 0, false
	}
	b := m.state()
	d, ok := b.ai.DamageCooldown(m.tx, d)
	if !ok {
		return 0, false
	}
	d = mobArmourAbsorb(b, hostileBaseArmour(b.kind), d, s)
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	d = mobProtectionAbsorb(b, d, s)
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

// hostileBaseArmour is the armour attribute the kind has without equipment:
// zombies have 2.
func hostileBaseArmour(kind string) float64 {
	switch kind {
	case "minecraft:zombie", "minecraft:husk", "minecraft:zombie_villager":
		return 2
	}
	return 0
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
	if m.state().kind == "minecraft:ravager" {
		// KNOCKBACK_RESISTANCE of 0.75.
		f, h = f*.25, h*.25
	}
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

// dropCustomDeathLoot ports Mob.dropCustomDeathLoot for the equipment of
// the hostile mob.
func (m *HostileMob) dropCustomDeathLoot(killedByPlayer bool, looting int) {
	dropEquipmentOnDeath(m.tx, m, m.state(), killedByPlayer, looting)
}

// experienceEquipment counts the worn items that may drop, which
// Mob.getBaseExperienceReward rewards.
func (m *HostileMob) experienceEquipment() int {
	b, n := m.state(), 0
	for slot := range equipmentSlots {
		if !b.equipment(slot).Empty() && b.dropChance(slot) <= 1 {
			n++
		}
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
	// Mob.convertTo does not finalize the spawn of the mob it makes.
	opts := world.EntitySpawnOpts{Position: e.Position(), Rotation: e.Rotation(), SpawnReason: "conversion", SpawnFinalized: true}
	convertMob(tx, hostileSelf(e, b.kind), opts.New(StrayType, hostileConfig{kind: StrayType.id}), conversion{keepEquipment: true, preserveCanPickUpLoot: true})
	tx.PlaySound(e.Position(), sound.NativeEvent{SoundType: "convert_to_stray", ExtraData: -1})
	return true
}

// Shaking reports whether the mob shakes while it converts: a skeleton to a
// stray, a zombie to a drowned, a zombie villager being cured.
func (m *HostileMob) Shaking() bool {
	b := m.state()
	return b.strayConverting || b.drownedConverting || b.kind == "minecraft:zombie_villager" && b.cureTicks > 0
}

// tickDrowning ports the drowning ConversionTracker of Zombie: after 600
// ticks with its eyes under water a zombie shakes for 300 ticks and then
// becomes a drowned, or a husk a zombie, keeping its equipment. It reports
// whether the zombie converted.
func (b *hostileState) tickDrowning(e *Ent, tx *world.Tx) bool {
	if b.ai.NoAI || b.dead || !b.ai.started {
		return false
	}
	self := hostileSelf(e, b.kind)
	m := b.ai.Mob(e, self)
	if !eyeInWater(m) {
		b.conversionTime = -1
		if b.drownedConverting {
			b.drownedConverting = false
			e.updateState()
		}
		return false
	}
	if !b.drownedConverting {
		if b.conversionTime++; b.conversionTime >= 600 {
			b.drownedConversionTime, b.drownedConverting = 300, true
			e.updateState()
		}
		return false
	}
	if b.drownedConversionTime--; b.drownedConversionTime >= 0 {
		return false
	}
	opts := world.EntitySpawnOpts{Position: e.Position(), Rotation: e.Rotation(), SpawnReason: "conversion", SpawnFinalized: true}
	c := conversion{keepEquipment: true, preserveCanPickUpLoot: true}
	if b.kind == "minecraft:husk" {
		convertMob(tx, self, opts.New(ZombieType, hostileConfig{kind: ZombieType.id, baby: b.baby}), c)
		tx.PlaySound(e.Position(), sound.NativeEvent{SoundType: "mob.husk.convert_to_zombie", ExtraData: -1})
		return true
	}
	convertMob(tx, self, opts.New(DrownedType, oceanConfig{kind: oceanDrowned, baby: b.baby}), c)
	tx.PlaySound(e.Position(), sound.NativeEvent{SoundType: "convert_to_drowned", ExtraData: -1})
	return true
}

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
	// Zombie.convertVillagerToZombieVillager finalizes the spawn of the
	// zombie villager as a conversion that is no baby of its own accord.
	opts := world.EntitySpawnOpts{Position: target.Position(), Rotation: target.Rotation(), SpawnReason: "conversion",
		SpawnGroup: &world.EntitySpawnGroup{Data: &zombieSpawnGroupData{canSpawnJockey: true}}}
	config := hostileConfig{kind: ZombieVillagerType.id, baby: v.age < 0, profession: v.profession, villagerType: v.villagerType, villagerLevel: v.level, villagerXP: v.xp, villagerAge: v.age, offers: append([]MerchantOffer(nil), v.offers...)}
	convertMob(tx, target, opts.New(ZombieVillagerType, config), conversion{keepEquipment: true, preserveCanPickUpLoot: true})
}

type zombieSpawnGroupData struct {
	baby                  bool
	canSpawnJockey        bool
	triedToSpawnCamelHusk bool
}

// chickenJockey is how Zombie.finalizeSpawn makes a baby zombie a chicken
// jockey.
type chickenJockey int

const (
	noChickenJockey chickenJockey = iota
	// nearbyChickenJockey rides a chicken near that nothing rides.
	nearbyChickenJockey
	// newChickenJockey rides a chicken that spawns with it.
	newChickenJockey
)

// finalizeZombieSpawn ports what Zombie.finalizeSpawn decides for all
// zombies. populate fills in the equipment of the kind, as its
// populateDefaultEquipmentSlots does, before its spawn equipment may be
// enchanted and, on Halloween, a pumpkin put on its head.
func finalizeZombieSpawn(reason string, difficulty world.LocalDifficulty, r *vanilla.Random, group *world.EntitySpawnGroup, h equipmentHolder, populate func()) (baby, canBreakDoors, canPickupLoot bool, jockey chickenJockey) {
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
		if r.NextFloat() < .05 {
			jockey = nearbyChickenJockey
		} else if r.NextFloat() < .05 {
			jockey = newChickenJockey
		}
	}
	canBreakDoors = float64(r.NextFloat()) < float64(difficulty.SpecialMultiplier())*.1
	if reason != "conversion" {
		populate()
		populateDefaultEnchantments(h, r, difficulty.SpecialMultiplier())
	}
	halloweenHead(h, r)
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
	var difficulty world.LocalDifficulty
	if m.tx != nil {
		difficulty = m.tx.LocalDifficulty(cube.PosFromVec3(m.Position()))
	}
	special, hard := difficulty.SpecialMultiplier(), difficulty.Base == world.DifficultyHard
	switch b.kind {
	case "minecraft:zombie", "minecraft:zombie_villager", "minecraft:husk":
		if m.tx == nil {
			return
		}
		baby, doors, pickup, jockey := finalizeZombieSpawn(reason, difficulty, r, m.H().SpawnGroup(), b, func() {
			// Zombie.populateDefaultEquipmentSlots: armour, then now and
			// then an iron weapon.
			populateDefaultArmour(b, r, special, hard)
			chance := float32(.01)
			if hard {
				chance = .05
			}
			if r.NextFloat() < chance {
				switch r.NextInt(6) {
				case 0:
					b.main = item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
				case 1:
					b.main = item.NewStack(item.Spear{Tier: item.ToolTierIron}, 1)
				default:
					b.main = item.NewStack(item.Shovel{Tier: item.ToolTierIron}, 1)
				}
			}
		})
		// Zombie.finalizeSpawn only ever makes a zombie a baby.
		b.baby, b.canBreakDoors = b.baby || baby, doors
		if reason != "conversion" {
			b.canPickupLoot = pickup
		}
		rideChickenJockey(m.tx, m, jockey)
		if b.baby {
			b.speed = hostileTypeFor(b.kind).speed * 1.5
		}
		if b.kind == "minecraft:husk" {
			if reason != "conversion" {
				b.canPickupLoot = r.NextFloat() < .55*special
			}
			m.finalizeHuskCamel(reason, r)
		}
	case "minecraft:skeleton", "minecraft:stray", "minecraft:bogged", "minecraft:parched":
		// AbstractSkeleton.finalizeSpawn.
		if reason != "conversion" {
			populateDefaultArmour(b, r, special, hard)
			b.main = item.NewStack(item.Bow{}, 1)
			populateDefaultEnchantments(b, r, special)
			b.canPickupLoot = r.NextFloat() < .55*special
			halloweenHead(b, r)
		}
	case "minecraft:wither_skeleton":
		// WitherSkeleton.populateDefaultEquipmentSlots gives it a stone
		// sword and neither armour nor enchantments.
		if reason != "conversion" {
			b.main = item.NewStack(item.Sword{Tier: item.ToolTierStone}, 1)
			b.canPickupLoot = r.NextFloat() < .55*special
			halloweenHead(b, r)
		}
	case "minecraft:pillager":
		b.main = item.NewStack(item.Crossbow{}, 1)
		populateDefaultEnchantments(b, r, special)
		// Pillager.enchantSpawnedWeapon: one in 300 crossbows pierce.
		if r.NextInt(300) == 0 {
			b.main = b.main.WithEnchantments(item.NewEnchantment(enchantment.Piercing, 1))
		}
	case "minecraft:vindicator":
		b.main = item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)
	case "minecraft:vex":
		b.main = item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
		b.setDropChance(slotMainhand, 0)
		populateDefaultEnchantments(b, r, special)
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
	if b.kind == "minecraft:vindicator" {
		// Vindicator.finalizeSpawn equips it after PatrollingMonster's.
		populateDefaultEnchantments(b, r, special)
	}
	if b.raidLeader || b.patrolLeader {
		b.head = ominousBannerStack()
		b.headDropGuaranteed = true
	}
}

// rideChickenJockey makes a baby zombie a chicken jockey as
// Zombie.finalizeSpawn does: it gets on the first chicken near that nothing
// rides, or on one that spawns with it.
func rideChickenJockey(tx *world.Tx, rider world.Entity, jockey chickenJockey) {
	var chicken *Chicken
	switch jockey {
	case nearbyChickenJockey:
		box := rider.H().Type().BBox(rider).Translate(rider.Position()).GrowVec3(mgl64.Vec3{5, 3, 5})
		for e := range tx.EntitiesWithin(box) {
			if c, ok := e.(*Chicken); ok && !c.Dead() && len(passengersOf(tx, c)) == 0 {
				chicken = c
				break
			}
		}
	case newChickenJockey:
		opts := world.EntitySpawnOpts{Position: rider.Position(), Rotation: cube.Rotation{rider.Rotation().Yaw(), 0}, SpawnReason: "jockey"}
		chicken, _ = tx.AddEntity(NewChicken(opts, ChickenBehaviourConfig{})).(*Chicken)
	}
	if chicken == nil {
		return
	}
	chicken.behaviour().jockey = true
	mobStartRiding(tx, rider, chicken, false)
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
