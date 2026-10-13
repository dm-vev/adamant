package entity

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// VillagerType stores a Java villager's trade career and villager identity.
var VillagerType villagerType

type villagerType struct{}

func (villagerType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		VillagerBehaviourConfig{}.Apply(data)
	}
	return &Villager{Ent: &Ent{tx: tx, handle: h, data: data}}
}
func (villagerType) EncodeEntity() string { return "minecraft:villager" }
func (villagerType) BBox(e world.Entity) cube.BBox {
	if v, ok := e.(*Villager); ok && v.Baby() {
		return cube.Box(-.245, 0, -.245, .245, .98, .245)
	}
	return cube.Box(-.3, 0, -.3, .3, 1.95, .3)
}
func (villagerType) DecodeNBT(nbt map[string]any, data *world.EntityData) {
	source := ReadNativeMobJavaState(nbt)
	if source == nil {
		source = nbt
	}
	c := VillagerBehaviourConfig{}
	if _, ok := source["Health"]; ok {
		c.Health, c.healthSet = float64(nbtconv.Float32(source, "Health")), true
	}
	if health, ok := nativeMobAttribute(nbt, "minecraft:health"); ok {
		c.Health, c.healthSet = health.Current, true
	}
	if _, ok := source["Xp"]; ok {
		c.XP = int(nbtconv.Int32(source, "Xp"))
	}
	if _, ok := source["Age"]; ok {
		c.Age = int(nbtconv.Int32(source, "Age"))
	}
	c.Finalized = nbtconv.Bool(source, "VillagerDataFinalized")
	c.FoodLevel = int(nbtconv.Uint8(source, "FoodLevel"))
	c.LastGossipDecay = nbtconv.Int64(source, "LastGossipDecay")
	c.LastRestock = nbtconv.Int64(source, "LastRestock")
	c.RestocksToday = int(nbtconv.Int32(source, "RestocksToday"))
	c.LastSlept = nbtconv.Int64(source, "VillagerLastSlept")
	c.HasSlept = nbtconv.Bool(source, "VillagerHasSlept")
	c.GolemDetectedUntil = nbtconv.Int64(source, "VillagerGolemDetectedUntil")
	c.JobSite, c.HasJobSite = villagerReadPos(source["VillagerJobSite"])
	c.Home, c.HasHome = villagerReadPos(source["VillagerHome"])
	c.PotentialJobSite, c.HasPotentialJobSite = villagerReadPos(source["VillagerPotentialJobSite"])
	c.MeetingPoint, c.HasMeetingPoint = villagerReadPos(source["VillagerMeetingPoint"])
	for _, raw := range nbtconv.Slice(source, "Inventory") {
		row, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		stack := item.ReadNBT(row, nil)
		slot := int(nbtconv.Uint8(row, "Slot"))
		if slot >= 0 && slot < 8 && !stack.Empty() {
			c.Inventory = append(c.Inventory, villagerInventorySlot{Slot: slot, Stack: stack})
		}
	}
	if d, ok := source["VillagerData"].(map[string]any); ok {
		c.Type = villagerString(d, "type", "minecraft:plains")
		c.Profession = villagerString(d, "profession", "minecraft:none")
		c.Level = int(nbtconv.Int32(d, "level"))
		if _, hasType := d["type"]; hasType {
			if _, hasProfession := d["profession"]; hasProfession {
				if _, hasLevel := d["level"]; hasLevel {
					c.Finalized = true
				}
			}
		}
	}
	if raw, ok := source["Gossips"]; ok {
		for _, row := range nbtconv.Slice(map[string]any{"Gossips": raw}, "Gossips") {
			g, ok := row.(map[string]any)
			if !ok {
				continue
			}
			target := villagerUUIDString(g["Target"])
			kind := villagerString(g, "Type", "")
			value := int(nbtconv.Int32(g, "Value"))
			if target != "" && value >= 2 && villagerGossipType(kind) {
				c.Gossips = append(c.Gossips, villagerGossip{target, kind, value})
			}
		}
	}
	if o, ok := source["Offers"].(map[string]any); ok {
		for _, raw := range nbtconv.Slice(o, "Recipes") {
			m, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			a, err := villagerStack(m["buy"])
			if err != nil || a.Empty() {
				continue
			}
			b, err := villagerStack(m["sell"])
			if err != nil || b.Empty() {
				continue
			}
			var second item.Stack
			if rawSecond, exists := m["buyB"]; exists {
				second, _ = villagerStack(rawSecond)
			}
			c.Offers = append(c.Offers, MerchantOffer{Input: a, SecondaryInput: second, Output: b,
				Uses: int(nbtconv.Int32(m, "uses")), MaxUses: int(nbtconv.Int32(m, "maxUses")),
				XP: int(nbtconv.Int32(m, "xp")), RewardXP: nbtconv.Bool(m, "rewardExp"),
				Demand: int(nbtconv.Int32(m, "demand")), SpecialPrice: int(nbtconv.Int32(m, "specialPrice")),
				PriceMultiplier: nbtconv.Float32(m, "priceMultiplier")})
		}
	}
	if _, exists := source["OffersGenerated"]; exists {
		c.OffersGenerated = nbtconv.Bool(source, "OffersGenerated")
	} else {
		c.OffersGenerated = len(c.Offers) > 0
	}
	data.Data = c.New()
}
func (villagerType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*VillagerBehaviour)
	rows := make([]any, 0, len(b.offers))
	for _, offer := range b.offers {
		row := map[string]any{"buy": villagerWriteStack(offer.Input), "sell": villagerWriteStack(offer.Output),
			"uses": int32(offer.Uses), "maxUses": int32(offer.MaxUses), "xp": int32(offer.XP),
			"rewardExp": boolByte(offer.RewardXP), "demand": int32(offer.Demand), "specialPrice": int32(offer.SpecialPrice),
			"priceMultiplier": offer.PriceMultiplier}
		if !offer.SecondaryInput.Empty() {
			row["buyB"] = villagerWriteStack(offer.SecondaryInput)
		}
		rows = append(rows, row)
	}
	gossips := make([]any, 0)
	inventoryRows := make([]any, 0)
	for slot, stack := range b.inventory.Slots() {
		if stack.Empty() {
			continue
		}
		row := item.WriteNBT(stack, true)
		row["Slot"] = uint8(slot)
		inventoryRows = append(inventoryRows, row)
	}
	for target, entries := range b.gossips {
		for kind, value := range entries {
			gossips = append(gossips, map[string]any{"Target": villagerUUIDNBT(target), "Type": kind, "Value": int32(value)})
		}
	}
	result := map[string]any{"Health": float32(b.health.Health()), "Xp": int32(b.xp), "FoodLevel": uint8(b.foodLevel),
		"Age": int32(b.age), "VillagerDataFinalized": boolByte(b.finalized), "OffersGenerated": boolByte(b.offersGenerated),
		"VillagerData": map[string]any{"type": b.villagerType, "profession": b.profession, "level": int32(b.level)},
		"Inventory":    inventoryRows, "Offers": map[string]any{"Recipes": rows}, "Gossips": gossips, "LastGossipDecay": b.lastGossipDecay,
		"LastRestock": b.lastRestock, "RestocksToday": int32(b.restocksToday), "VillagerLastSlept": b.lastSlept,
		"VillagerHasSlept": boolByte(b.hasSlept), "VillagerGolemDetectedUntil": b.golemDetectedUntil}
	if b.hasJobSite {
		result["VillagerJobSite"] = villagerPosNBT(b.jobSite)
	}
	if b.hasHome {
		result["VillagerHome"] = villagerPosNBT(b.home)
	}
	if b.hasPotentialJobSite {
		result["VillagerPotentialJobSite"] = villagerPosNBT(b.potentialJobSite)
	}
	if b.hasMeetingPoint {
		result["VillagerMeetingPoint"] = villagerPosNBT(b.meetingPoint)
	}
	health := b.health
	maxHealth := health.MaxHealth()
	attributes := EncodeNativeMobAttributes(ReadNativeMobAttributes(data.NativeNBT), []NativeMobAttribute{
		{Name: "minecraft:health", Base: maxHealth, Current: health.Health(), DefaultMin: 0, DefaultMax: maxHealth, Min: 0, Max: maxHealth},
	})
	native := MergeNativeMobNBT(data.NativeNBT, map[string]any{"Health": float32(health.Health()), "Attributes": attributes})
	javaState := MergeNativeMobNBT(ReadNativeMobJavaState(data.NativeNBT), result)
	if !b.hasJobSite {
		delete(javaState, "VillagerJobSite")
	}
	if !b.hasHome {
		delete(javaState, "VillagerHome")
	}
	if !b.hasPotentialJobSite {
		delete(javaState, "VillagerPotentialJobSite")
	}
	if !b.hasMeetingPoint {
		delete(javaState, "VillagerMeetingPoint")
	}
	return MergeNativeMobJavaState(native, javaState)
}
func villagerUUIDNBT(target string) []int32 {
	id, err := uuid.Parse(target)
	if err != nil {
		return nil
	}
	bytes := id[:]
	return []int32{int32(binary.BigEndian.Uint32(bytes[0:4])), int32(binary.BigEndian.Uint32(bytes[4:8])), int32(binary.BigEndian.Uint32(bytes[8:12])), int32(binary.BigEndian.Uint32(bytes[12:16]))}
}
func villagerPosNBT(pos cube.Pos) []int32 {
	return []int32{int32(pos[0]), int32(pos[1]), int32(pos[2])}
}
func villagerReadPos(raw any) (cube.Pos, bool) {
	coordinates, ok := raw.([]int32)
	if !ok || len(coordinates) != 3 {
		return cube.Pos{}, false
	}
	return cube.Pos{int(coordinates[0]), int(coordinates[1]), int(coordinates[2])}, true
}
func villagerString(data map[string]any, key, fallback string) string {
	if s, ok := data[key].(string); ok && s != "" {
		return s
	}
	return fallback
}
func villagerStack(raw any) (item.Stack, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return item.Stack{}, nil
	}
	stack := item.ReadNBT(m, nil)
	if mapItem, ok := stack.Item().(item.FilledMap); ok {
		if raw, exists := stack.Value("adamant:java_map_variant"); exists {
			switch variant := raw.(type) {
			case int32:
				mapItem.Variant = item.FilledMapVariant(variant)
			case int:
				mapItem.Variant = item.FilledMapVariant(variant)
			}
			if mapItem.Variant.Valid() {
				stack = stack.WithItem(mapItem)
			}
		}
	}
	return stack, nil
}
func villagerWriteStack(s item.Stack) map[string]any {
	if mapItem, ok := s.Item().(item.FilledMap); ok {
		s = s.WithValue("adamant:java_map_variant", int32(mapItem.Variant))
	}
	return item.WriteNBT(s, true)
}
func villagerUUIDString(raw any) string {
	if id, ok := raw.(string); ok {
		parsed, err := uuid.Parse(id)
		if err == nil {
			return parsed.String()
		}
		return ""
	}
	parts, ok := raw.([]int32)
	if !ok || len(parts) != 4 {
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

type villagerGossip struct {
	target, kind string
	value        int
}

func villagerGossipType(kind string) bool {
	switch kind {
	case "major_negative", "minor_negative", "minor_positive", "major_positive", "trading":
		return true
	}
	return false
}

type villagerInventorySlot struct {
	Slot  int
	Stack item.Stack
}

// VillagerBehaviourConfig stores Java villager state decoded from NBT.
type VillagerBehaviourConfig struct {
	Health                                                                                                     float64
	XP, Level, FoodLevel, RestocksToday, Age                                                                   int
	Profession, Type                                                                                           string
	Offers                                                                                                     []MerchantOffer
	Inventory                                                                                                  []villagerInventorySlot
	Gossips                                                                                                    []villagerGossip
	LastGossipDecay, LastRestock                                                                               int64
	LastSlept, GolemDetectedUntil                                                                              int64
	JobSite, PotentialJobSite, Home, MeetingPoint                                                              cube.Pos
	HasJobSite, HasPotentialJobSite, HasHome, HasMeetingPoint, HasSlept, Finalized, OffersGenerated, healthSet bool
}

func (c VillagerBehaviourConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c VillagerBehaviourConfig) New() *VillagerBehaviour {
	if c.Level < 1 || c.Level > 5 {
		c.Level = 1
	}
	if c.Profession == "" {
		c.Profession = "minecraft:none"
	}
	if c.Type == "" {
		c.Type = "minecraft:plains"
	}
	health := c.Health
	if !c.healthSet || health <= 0 {
		health = 20
	}
	gossips := make(map[string]map[string]int)
	for _, g := range c.Gossips {
		if gossips[g.target] == nil {
			gossips[g.target] = make(map[string]int)
		}
		gossips[g.target][g.kind] = g.value
	}
	inv := inventory.New(8, nil)
	for _, row := range c.Inventory {
		if row.Slot >= 0 && row.Slot < 8 {
			_ = inv.SetItem(row.Slot, row.Stack)
		}
	}
	age, finalized := c.Age, c.Finalized
	return &VillagerBehaviour{health: NewHealthManager(health, 20), effects: NewEffectManager(), movement: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true},
		speed:      .5,
		profession: c.Profession, villagerType: c.Type, level: c.Level, xp: max(0, c.XP), foodLevel: max(0, c.FoodLevel), age: age, finalized: finalized, inventory: inv,
		offers: append([]MerchantOffer(nil), c.Offers...), offersGenerated: c.OffersGenerated, gossips: gossips, lastGossipDecay: c.LastGossipDecay,
		lastRestock: c.LastRestock, restocksToday: max(0, c.RestocksToday), lastSlept: c.LastSlept, hasSlept: c.HasSlept, golemDetectedUntil: c.GolemDetectedUntil,
		jobSite: c.JobSite, home: c.Home, meetingPoint: c.MeetingPoint, hasJobSite: c.HasJobSite, hasHome: c.HasHome, hasMeetingPoint: c.HasMeetingPoint,
		potentialJobSite: c.PotentialJobSite, hasPotentialJobSite: c.HasPotentialJobSite,
	}
}

type VillagerBehaviour struct {
	health                                                                                                *HealthManager
	effects                                                                                               *EffectManager
	movement                                                                                              *MovementComputer
	speed                                                                                                 float64
	profession, villagerType                                                                              string
	level, xp                                                                                             int
	offers                                                                                                []MerchantOffer
	inventory                                                                                             *inventory.Inventory
	gossips                                                                                               map[string]map[string]int
	lastGossipDecay, lastRestock, lastSlept, golemDetectedUntil, nextPOIScan                              int64
	age, foodLevel, restocksToday                                                                         int
	jobSite, potentialJobSite, home, meetingPoint                                                         cube.Pos
	finalized, offersGenerated, hasSlept, hasJobSite, hasPotentialJobSite, hasHome, hasMeetingPoint, dead bool
}

func (b *VillagerBehaviour) Baby() bool { return b.age < 0 }

type Villager struct{ *Ent }

func (v *Villager) behaviour() *VillagerBehaviour { return v.Behaviour().(*VillagerBehaviour) }
func (v *Villager) Tick(tx *world.Tx, current int64) {
	b := v.behaviour()
	b.tickGossip(current)
	b.tickPOI(v, tx, current)
	v.tickRestock(tx, current)
	b.effects.Tick(v, tx)
	v.Ent.Tick(tx, current)
	if b.age < 0 {
		b.age = min(0, b.age+1)
	} else if b.age > 0 {
		b.age = max(0, b.age-1)
	}
}
func (b *VillagerBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	m := b.movement.TickMovement(e, e.data.Pos, e.data.Vel, e.data.Rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}
func (v *Villager) Health() float64        { return v.behaviour().health.Health() }
func (v *Villager) MaxHealth() float64     { return v.behaviour().health.MaxHealth() }
func (v *Villager) SetMaxHealth(h float64) { v.behaviour().health.SetMaxHealth(h) }
func (v *Villager) Dead() bool             { return v.behaviour().dead || v.Health() <= 0 }
func (v *Villager) releasePOIs(tx *world.Tx) {
	b := v.behaviour()
	if tx == nil {
		return
	}
	owner := v.H().UUID()
	if b.hasJobSite {
		tx.ReleasePOI(b.jobSite, owner)
		b.hasJobSite = false
	}
	if b.hasPotentialJobSite {
		tx.ReleasePOI(b.potentialJobSite, owner)
		b.hasPotentialJobSite = false
	}
	if b.hasMeetingPoint {
		tx.ReleasePOI(b.meetingPoint, owner)
		b.hasMeetingPoint = false
	}
	if b.hasHome {
		tx.ReleasePOI(b.home, owner)
		b.hasHome = false
	}
}
func (v *Villager) Hurt(d float64, src world.DamageSource) (float64, bool) {
	b := v.behaviour()
	if v.Dead() || d <= 0 || (src.Fire() && v.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, r.Level())
	}
	b.health.AddHealth(-d)
	v.viewAction(HurtAction{})
	onMobHurt(v.tx, v)
	if v.Dead() {
		b.dead = true
		v.releasePOIs(v.tx)
		v.viewAction(DeathAction{})
		dropAllDeathLoot(v.tx, v, src)
		_ = v.CloseIn(v.tx)
	}
	return d, true
}
func (v *Villager) Heal(h float64, _ world.HealingSource) float64 {
	if v.Dead() || h <= 0 {
		return 0
	}
	old := v.Health()
	v.behaviour().health.AddHealth(h)
	return v.Health() - old
}
func (v *Villager) Baby() bool   { return v.behaviour().age < 0 }
func (v *Villager) Age() int       { return v.behaviour().age }
func (v *Villager) SetAge(age int) { v.behaviour().age = age }
func (v *Villager) CanBreed() bool {
	b := v.behaviour()
	return b.age == 0 && b.foodLevel+b.foodPoints() >= 12
}
func (v *Villager) FoodLevel() int { return v.behaviour().foodLevel }
func (v *Villager) AddFood(points int) {
	if points > 0 {
		v.behaviour().foodLevel = min(24, v.behaviour().foodLevel+points)
	}
}

// MarkSlept records Java's last-slept memory for golem spawning.
func (v *Villager) MarkSlept(tick int64) { b := v.behaviour(); b.lastSlept = tick; b.hasSlept = true }

// MarkGolemDetected records the short-lived Java golem-detected brain memory.
func (v *Villager) MarkGolemDetected(tick int64) { v.behaviour().golemDetectedUntil = tick + 599 }

// WantsToSpawnGolem matches Java's recent-sleep and recent-golem sensor gates.
func (v *Villager) WantsToSpawnGolem(tick int64) bool {
	b := v.behaviour()
	return b.hasSlept && tick-b.lastSlept < 24000 && tick >= b.golemDetectedUntil
}

// BreedWith creates a child with Java's 50/25/25 biome/parent variant selection.
func (v *Villager) BreedWith(tx *world.Tx, partner *Villager, random *java26.Random) *world.EntityHandle {
	if partner == nil || partner == v || random == nil || !v.CanBreed() || !partner.CanBreed() {
		return nil
	}
	v.behaviour().age = 6000
	partner.behaviour().age = 6000
	variant := villagerTypeForBiome(tx.Biome(cube.PosFromVec3(v.Position())).String())
	roll := random.NextDouble()
	if roll >= .5 {
		variant = v.Variant()
	}
	if roll >= .75 {
		variant = partner.Variant()
	}
	conf := VillagerBehaviourConfig{Profession: "minecraft:none", Type: variant, Level: 1, Age: -24000, Finalized: true}
	return world.EntitySpawnOpts{Position: v.Position()}.New(VillagerType, conf)
}

// FinalizeSpawn applies Java's one-time villager variant/profession finalization.
func (v *Villager) FinalizeSpawn(reason string, _ *java26.Random) {
	b := v.behaviour()
	if b.finalized {
		return
	}
	if reason == "breeding" {
		b.profession = "minecraft:none"
		b.level = 1
		b.xp = 0
		b.offers = nil
	}
	b.villagerType = villagerTypeForBiome(v.tx.Biome(cube.PosFromVec3(v.Position())).String())
	b.finalized = true
}
func villagerTypeForBiome(name string) string {
	name = strings.TrimPrefix(name, "minecraft:")
	switch name {
	case "badlands", "desert", "eroded_badlands", "wooded_badlands":
		return "minecraft:desert"
	case "bamboo_jungle", "jungle", "sparse_jungle":
		return "minecraft:jungle"
	case "savanna_plateau", "savanna", "windswept_savanna":
		return "minecraft:savanna"
	case "deep_frozen_ocean", "frozen_ocean", "frozen_river", "ice_spikes", "snowy_beach", "snowy_taiga", "snowy_plains", "grove", "snowy_slopes", "frozen_peaks", "jagged_peaks":
		return "minecraft:snow"
	case "swamp", "mangrove_swamp":
		return "minecraft:swamp"
	case "old_growth_spruce_taiga", "old_growth_pine_taiga", "windswept_gravelly_hills", "windswept_hills", "taiga", "windswept_forest":
		return "minecraft:taiga"
	default:
		return "minecraft:plains"
	}
}
func (v *Villager) Inventory() *inventory.Inventory { return v.behaviour().inventory }
func (v *Villager) Collect(stack item.Stack) (int, bool) {
	if stack.Empty() || !villagerWantsItem(stack, v.Profession()) {
		return 0, false
	}
	count, _ := v.behaviour().inventory.AddItem(stack)
	return count, true
}
func villagerWantsItem(stack item.Stack, profession string) bool {
	if villagerFoodPoints(stack) > 0 {
		return true
	}
	id, _ := stack.Item().EncodeItem()
	return profession == "minecraft:farmer" && (id == "minecraft:wheat_seeds" || id == "minecraft:beetroot_seeds" || id == "minecraft:bone_meal")
}
func villagerFoodPoints(stack item.Stack) int {
	id, _ := stack.Item().EncodeItem()
	switch id {
	case "minecraft:wheat", "minecraft:carrot", "minecraft:potato", "minecraft:beetroot":
		return 4 * stack.Count()
	case "minecraft:bread":
		return 12 * stack.Count()
	}
	return 0
}
func (b *VillagerBehaviour) foodPoints() int {
	points := 0
	for _, stack := range b.inventory.Items() {
		points += villagerFoodPoints(stack)
	}
	return points
}
func (v *Villager) HasExcessFood() bool { return v.behaviour().foodPoints() >= 24 }
func (v *Villager) WantsMoreFood() bool { return v.behaviour().foodPoints() < 12 }
func (v *Villager) EatAndDigestFood() {
	b := v.behaviour()
	if b.foodLevel < 12 {
		for slot, stack := range b.inventory.Slots() {
			if stack.Empty() {
				continue
			}
			pointsPerItem := villagerFoodPoints(item.NewStack(stack.Item(), 1))
			if pointsPerItem <= 0 {
				continue
			}
			for stack.Count() > 0 && b.foodLevel < 12 {
				stack = stack.Grow(-1)
				b.foodLevel += pointsPerItem
			}
			_ = b.inventory.SetItem(slot, stack)
			if b.foodLevel >= 12 {
				break
			}
		}
	}
	b.foodLevel = max(0, b.foodLevel-12)
}
func (v *Villager) SetProfession(profession string) {
	b := v.behaviour()
	if b.profession == profession {
		return
	}
	b.profession = profession
	b.offers = nil
	b.offersGenerated = false
	if v.tx != nil {
		v.updateState()
	}
}
func (v *Villager) SetOffers(offers []MerchantOffer) {
	b := v.behaviour()
	b.offers = append([]MerchantOffer(nil), offers...)
	b.offersGenerated = true
}
func (v *Villager) Profession() string { return v.behaviour().profession }
func (v *Villager) Variant() string    { return v.behaviour().villagerType }
func (v *Villager) KnockBack(src mgl64.Vec3, force, height float64) {
	d := v.Position().Sub(src)
	d[1] = 0
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	v.SetVelocity(d)
}
func (v *Villager) AddEffect(e effect.Effect)    { v.behaviour().effects.Add(e, v) }
func (v *Villager) RemoveEffect(t effect.Type)   { v.behaviour().effects.Remove(t, v) }
func (v *Villager) Effects() []effect.Effect     { return v.behaviour().effects.Effects() }
func (v *Villager) Speed() float64               { return v.behaviour().speed }
func (v *Villager) SetSpeed(speed float64)       { v.behaviour().speed = speed }
func (v *Villager) hasEffect(t effect.Type) bool { _, ok := v.behaviour().effects.Effect(t); return ok }
func (v *Villager) MerchantName() string         { return v.behaviour().profession }
func (v *Villager) MerchantProfession() string   { return v.behaviour().profession }
func (v *Villager) MerchantLevel() int           { return v.behaviour().level }
func (v *Villager) MerchantOffers() ([]MerchantOffer, error) {
	if err := v.ensureVillagerOffers(); err != nil {
		return nil, err
	}
	return append([]MerchantOffer(nil), v.behaviour().offers...), nil
}
func (v *Villager) MerchantOffersFor(user world.Entity) ([]MerchantOffer, error) {
	if err := v.ensureVillagerOffers(); err != nil {
		return nil, err
	}
	b := v.behaviour()
	offers := append([]MerchantOffer(nil), b.offers...)
	if user == nil {
		return offers, nil
	}
	reputation := b.reputation(user.H().UUID().String())
	for i := range offers {
		offers[i].SpecialPrice -= int(math.Floor(float64(reputation) * float64(offers[i].PriceMultiplier)))
	}
	return offers, nil
}
func (v *Villager) CompleteMerchantTrade(tx *world.Tx, user world.Entity, index int) (bool, error) {
	if tx != nil {
		v.bindTx(tx)
	}
	if err := v.ensureVillagerOffers(); err != nil {
		return false, err
	}
	b := v.behaviour()
	if index < 0 || index >= len(b.offers) {
		return false, nil
	}
	o := b.offers[index]
	if o.Input.Empty() || o.Output.Empty() || o.MaxUses <= 0 || o.Uses >= o.MaxUses {
		return false, nil
	}
	oldLevel, oldXP, oldOffers := b.level, b.xp, len(b.offers)
	newXP := b.xp + max(0, o.XP)
	thresholds := [5]int{0, 10, 70, 150, 250}
	newLevel := b.level
	for newLevel < 5 && newXP >= thresholds[newLevel] {
		newLevel++
	}
	if tx == nil && b.offersGenerated && newLevel > oldLevel {
		return false, fmt.Errorf("villager promotion requires an active world transaction")
	}
	if b.offersGenerated {
		b.level = newLevel
		for tier := oldLevel + 1; tier <= newLevel; tier++ {
			if err := v.addVillagerTradeLevel(tier); err != nil {
				b.level = oldLevel
				b.offers = b.offers[:oldOffers]
				b.xp = oldXP
				return false, err
			}
		}
	}
	b.level, b.xp = newLevel, newXP
	b.offers[index].Uses++
	if newLevel != oldLevel {
		v.updateState()
	}
	if user != nil {
		b.addGossip(user.H().UUID().String(), "trading", 2)
	}
	return true, nil
}
func (b *VillagerBehaviour) reputation(target string) int {
	values := b.gossips[target]
	return -5*values["major_negative"] - values["minor_negative"] + values["minor_positive"] + 5*values["major_positive"] + values["trading"]
}
func (v *Villager) Reputation(user world.Entity) int {
	if user == nil {
		return 0
	}
	return v.behaviour().reputation(user.H().UUID().String())
}
func (v *Villager) OnReputationEventFrom(kind string, source world.Entity) {
	if source == nil {
		return
	}
	b := v.behaviour()
	switch kind {
	case "zombie_villager_cured":
		b.addGossip(source.H().UUID().String(), "major_positive", 20)
		b.addGossip(source.H().UUID().String(), "minor_positive", 25)
	case "trade":
		b.addGossip(source.H().UUID().String(), "trading", 2)
	case "villager_hurt":
		b.addGossip(source.H().UUID().String(), "minor_negative", 25)
	case "villager_killed":
		b.addGossip(source.H().UUID().String(), "major_negative", 25)
	}
}
func (b *VillagerBehaviour) addGossip(target, kind string, amount int) {
	if target == "" || !villagerGossipType(kind) || amount == 0 {
		return
	}
	if b.gossips[target] == nil {
		b.gossips[target] = make(map[string]int)
	}
	maximum := map[string]int{"major_negative": 100, "minor_negative": 200, "minor_positive": 25, "major_positive": 20, "trading": 25}[kind]
	value := b.gossips[target][kind] + amount
	if value < 2 {
		delete(b.gossips[target], kind)
	} else {
		b.gossips[target][kind] = min(value, maximum)
	}
	if len(b.gossips[target]) == 0 {
		delete(b.gossips, target)
	}
}
func (b *VillagerBehaviour) tickGossip(current int64) {
	if b.lastGossipDecay == 0 {
		b.lastGossipDecay = current
		return
	}
	if current-b.lastGossipDecay < 24000 {
		return
	}
	for target, entries := range b.gossips {
		for kind, value := range entries {
			decay := map[string]int{"major_negative": 10, "minor_negative": 20, "minor_positive": 1, "major_positive": 0, "trading": 2}[kind]
			value -= decay
			if value < 2 {
				delete(entries, kind)
			} else {
				entries[kind] = value
			}
		}
		if len(entries) == 0 {
			delete(b.gossips, target)
		}
	}
	b.lastGossipDecay = current
}
func (b *VillagerBehaviour) tickPOI(v *Villager, tx *world.Tx, current int64) {
	owner := v.H().UUID()
	if b.hasJobSite {
		poi, ok := tx.POI(b.jobSite)
		if !ok || poi.Kind != world.Workstation || !villagerPOIMatchesProfession(poi.Type, b.profession) || poi.HasOwner && !villagerOwnsPOI(poi, owner) {
			if ok && villagerOwnsPOI(poi, owner) {
				tx.ReleasePOI(b.jobSite, owner)
			}
			b.hasJobSite = false
			if b.xp == 0 && b.level <= 1 {
				v.SetProfession("minecraft:none")
			}
		} else if poi.Vacant() && !tx.ClaimPOI(b.jobSite, owner) {
			b.hasJobSite = false
		}
	}
	if b.hasPotentialJobSite {
		poi, ok := tx.POI(b.potentialJobSite)
		if !ok || poi.Kind != world.Workstation || poi.HasOwner && !villagerOwnsPOI(poi, owner) {
			if ok && villagerOwnsPOI(poi, owner) {
				tx.ReleasePOI(b.potentialJobSite, owner)
			}
			b.hasPotentialJobSite = false
		}
	}
	if b.hasHome {
		poi, ok := tx.POI(b.home)
		if !ok || poi.Kind != world.Home || poi.HasOwner && !villagerOwnsPOI(poi, owner) {
			if ok && villagerOwnsPOI(poi, owner) {
				tx.ReleasePOI(b.home, owner)
			}
			b.hasHome = false
		} else if poi.Vacant() && !tx.ClaimPOI(b.home, owner) {
			b.hasHome = false
		}
	}
	if b.hasMeetingPoint {
		poi, ok := tx.POI(b.meetingPoint)
		if !ok || poi.Kind != world.Meeting {
			if ok && villagerOwnsPOI(poi, owner) {
				tx.ReleasePOI(b.meetingPoint, owner)
			}
			b.hasMeetingPoint = false
		} else if !villagerOwnsPOI(poi, owner) && !tx.ClaimPOI(b.meetingPoint, owner) {
			b.hasMeetingPoint = false
		}
	}
	if current >= b.nextPOIScan {
		b.nextPOIScan = current + 20
		if !b.hasHome {
			if pos, _, found := tx.NearestPOI(cube.PosFromVec3(v.Position()), world.Home, 48, func(p world.POI) bool { return p.Vacant() }); found && tx.ClaimPOI(pos, owner) {
				b.home, b.hasHome = pos, true
			}
		}
		if !b.hasMeetingPoint {
			if pos, _, found := tx.NearestPOI(cube.PosFromVec3(v.Position()), world.Meeting, 48, func(p world.POI) bool { return p.Vacant() }); found && tx.ClaimPOI(pos, owner) {
				b.meetingPoint, b.hasMeetingPoint = pos, true
			}
		}
		if !v.Baby() && !b.hasJobSite && !b.hasPotentialJobSite && b.profession != "minecraft:nitwit" {
			pos, _, found := tx.NearestPOI(cube.PosFromVec3(v.Position()), world.Workstation, 48, func(p world.POI) bool {
				profession := villagerProfessionForWorkstation(p.Type)
				return p.Vacant() && profession != "" && (b.profession == "minecraft:none" || b.profession == profession)
			})
			if found && tx.ClaimPOI(pos, owner) {
				b.potentialJobSite, b.hasPotentialJobSite = pos, true
			}
		}
	}
	if b.hasPotentialJobSite {
		if villagerNearPOI(v.Position(), b.potentialJobSite, 2) {
			poi, ok := tx.POI(b.potentialJobSite)
			if ok && villagerOwnsPOI(poi, owner) {
				b.jobSite, b.hasJobSite = b.potentialJobSite, true
				b.hasPotentialJobSite = false
				if b.profession == "minecraft:none" {
					if profession := villagerProfessionForWorkstation(poi.Type); profession != "" {
						v.SetProfession(profession)
					}
				}
			}
		} else {
			v.walkTowardsPOI(b.potentialJobSite)
		}
	}
	workTime := int(tx.World().Time()) % 24000
	if workTime >= 2000 && workTime < 9000 && b.hasJobSite && !villagerNearPOI(v.Position(), b.jobSite, 2) {
		v.walkTowardsPOI(b.jobSite)
	}
	if workTime >= 9000 && workTime < 11000 && b.hasMeetingPoint && !villagerNearPOI(v.Position(), b.meetingPoint, 2) {
		v.walkTowardsPOI(b.meetingPoint)
	}
	night := workTime >= 12000 || workTime < 1000
	if b.hasHome {
		if night && villagerNearPOI(v.Position(), b.home, 2) {
			if !b.hasSlept || current-b.lastSlept >= 24000 {
				v.MarkSlept(current)
			}
		}
		if night && !villagerNearPOI(v.Position(), b.home, 2) {
			v.walkTowardsPOI(b.home)
		}
	}
}

func (v *Villager) walkTowardsPOI(pos cube.Pos) bool {
	p := v.Position()
	dx, dz := float64(pos[0])+.5-p[0], float64(pos[2])+.5-p[2]
	distance := math.Sqrt(dx*dx + dz*dz)
	velocity := v.Velocity()
	if villagerNearPOI(p, pos, 2) {
		velocity[0], velocity[2] = 0, 0
		v.SetVelocity(velocity)
		return true
	}
	if distance == 0 {
		return false
	}
	velocity[0], velocity[2] = dx/distance*v.Speed()*.4, dz/distance*v.Speed()*.4
	v.SetVelocity(velocity)
	return false
}

func villagerNearPOI(position mgl64.Vec3, pos cube.Pos, radius float64) bool {
	dx, dy, dz := float64(pos[0])+.5-position[0], float64(pos[1])-position[1], float64(pos[2])+.5-position[2]
	return dx*dx+dy*dy+dz*dz < radius*radius
}

func villagerWorkstationForProfession(profession string) string {
	switch profession {
	case "minecraft:armorer":
		return "minecraft:blast_furnace"
	case "minecraft:butcher":
		return "minecraft:smoker"
	case "minecraft:cartographer":
		return "minecraft:cartography_table"
	case "minecraft:cleric":
		return "minecraft:brewing_stand"
	case "minecraft:farmer":
		return "minecraft:composter"
	case "minecraft:fisherman":
		return "minecraft:barrel"
	case "minecraft:fletcher":
		return "minecraft:fletching_table"
	case "minecraft:leatherworker":
		return "minecraft:cauldron"
	case "minecraft:librarian":
		return "minecraft:lectern"
	case "minecraft:mason":
		return "minecraft:stonecutter"
	case "minecraft:shepherd":
		return "minecraft:loom"
	case "minecraft:toolsmith":
		return "minecraft:smithing_table"
	case "minecraft:weaponsmith":
		return "minecraft:grindstone"
	default:
		return ""
	}
}

func villagerProfessionForWorkstation(poi string) string {
	switch poi {
	case "minecraft:blast_furnace":
		return "minecraft:armorer"
	case "minecraft:smoker":
		return "minecraft:butcher"
	case "minecraft:cartography_table":
		return "minecraft:cartographer"
	case "minecraft:brewing_stand":
		return "minecraft:cleric"
	case "minecraft:composter":
		return "minecraft:farmer"
	case "minecraft:barrel":
		return "minecraft:fisherman"
	case "minecraft:fletching_table":
		return "minecraft:fletcher"
	case "minecraft:cauldron":
		return "minecraft:leatherworker"
	case "minecraft:lectern":
		return "minecraft:librarian"
	case "minecraft:stonecutter":
		return "minecraft:mason"
	case "minecraft:loom":
		return "minecraft:shepherd"
	case "minecraft:smithing_table":
		return "minecraft:toolsmith"
	case "minecraft:grindstone":
		return "minecraft:weaponsmith"
	default:
		return ""
	}
}

func villagerPOIMatchesProfession(poi, profession string) bool {
	return villagerWorkstationForProfession(profession) == poi
}

func (v *Villager) tickRestock(tx *world.Tx, current int64) {
	b := v.behaviour()
	if current > b.lastRestock+12000 {
		missedUpdates := 2 - b.restocksToday
		if missedUpdates > 0 {
			for i := range b.offers {
				b.offers[i].Uses = 0
			}
			for range missedUpdates {
				for i := range b.offers {
					o := &b.offers[i]
					o.Demand += o.Uses - (o.MaxUses - o.Uses)
				}
			}
		}
		b.restocksToday = 0
		b.lastRestock = current
	}
	if b.restocksToday >= 2 || b.restocksToday > 0 && current <= b.lastRestock+2400 || len(b.offers) == 0 || current%20 != 0 {
		return
	}
	workTime := int(tx.World().Time()) % 24000
	if workTime < 2000 || workTime >= 9000 || !b.hasJobSite || !villagerHasWorkstation(tx, b.jobSite, b.profession, v.H().UUID()) || !villagerNearPOI(v.Position(), b.jobSite, 1.73) {
		return
	}
	needsRestock := false
	for i := range b.offers {
		if b.offers[i].Uses > 0 {
			needsRestock = true
			break
		}
	}
	if !needsRestock {
		return
	}
	for i := range b.offers {
		o := &b.offers[i]
		o.Demand += o.Uses - (o.MaxUses - o.Uses)
		o.Uses = 0
	}
	b.lastRestock = current
	b.restocksToday++
}
func villagerOwnsPOI(poi world.POI, owner uuid.UUID) bool {
	for _, existing := range poi.Owners {
		if existing == owner {
			return true
		}
	}
	return len(poi.Owners) == 0 && poi.HasOwner && poi.Owner == owner
}
func villagerHasWorkstation(tx *world.Tx, pos cube.Pos, profession string, owner uuid.UUID) bool {
	poi, ok := tx.POI(pos)
	return ok && poi.Kind == world.Workstation && villagerOwnsPOI(poi, owner) && villagerPOIMatchesProfession(poi.Type, profession)
}
func (v *Villager) Interact(tx *world.Tx, user item.User, _ *item.UseContext) bool {
	if v.Baby() {
		return true
	}
	v.bindTx(tx)
	opener, ok := user.(interface {
		OpenMerchant(world.Entity, Merchant, *world.Tx)
	})
	if !ok {
		return false
	}
	opener.OpenMerchant(v, v, tx)
	return true
}
func (v *Villager) viewAction(a world.EntityAction) {
	vs := v.tx.Viewers(v.Position())
	for _, viewer := range vs {
		viewer.ViewEntityAction(v, a)
	}
	v.tx.ReleaseViewers(vs)
}
func NewVillager(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(VillagerType, VillagerBehaviourConfig{})
}

var _ world.EntityType = villagerType{}
var _ Living = (*Villager)(nil)
var _ Merchant = (*Villager)(nil)
var _ Interactable = (*Villager)(nil)
var _ Collector = (*Villager)(nil)
