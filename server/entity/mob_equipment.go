package entity

import (
	"encoding/json"
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports what Java's Mob does with its equipment: the armour it
// may spawn in, the enchantments its spawn gear may get, the items it picks
// up and equips, how its armour absorbs damage and what of it drops on
// death.

// The equipment slots, in the order of Java's EquipmentSlot.VALUES.
const (
	slotMainhand = iota
	slotOffhand
	slotFeet
	slotLegs
	slotChest
	slotHead
	equipmentSlots
)

// defaultDropChance is the chance an item a mob spawned with drops when it
// dies.
const defaultDropChance = .085

// equipmentHolder is implemented by the states of mobs that keep Java's six
// equipment slots and their drop chances.
type equipmentHolder interface {
	// equipment returns the item in a slot.
	equipment(slot int) *item.Stack
	// dropChance returns the chance the item in a slot drops on death,
	// preserved undamaged if above 1.
	dropChance(slot int) float32
	// setDropChance changes the chance the item in a slot drops.
	setDropChance(slot int, chance float32)
}

// equipmentSlotFor ports LivingEntity.getEquipmentSlotForItem for the
// humanoid slots: armour, carved pumpkins, heads and elytra are worn, the
// rest held in the main hand.
func equipmentSlotFor(stack item.Stack) int {
	switch it := stack.Item().(type) {
	case item.HelmetType:
		return slotHead
	case item.ChestplateType, item.Elytra:
		return slotChest
	case item.LeggingsType:
		return slotLegs
	case item.BootsType:
		return slotFeet
	case block.Skull:
		return slotHead
	case block.Pumpkin:
		if it.Carved {
			return slotHead
		}
	}
	return slotMainhand
}

// populateDefaultArmour ports Mob.populateDefaultEquipmentSlots: with a
// chance growing with the local difficulty a mob spawns in a set of armour
// of one material, the better ones rarer, which on hard is less often cut
// short after the helmet.
func populateDefaultArmour(h equipmentHolder, r *vanilla.Random, special float32, hard bool) {
	if r.NextFloat() >= .15*special {
		return
	}
	armourType := int(r.NextInt(3))
	for range 3 {
		if r.NextFloat() < .1087 {
			armourType++
		}
	}
	partial := float32(.25)
	if hard {
		partial = .1
	}
	for i, slot := range [...]int{slotHead, slotChest, slotLegs, slotFeet} {
		if i != 0 && r.NextFloat() < partial {
			break
		}
		if h.equipment(slot).Empty() {
			*h.equipment(slot) = item.NewStack(defaultArmour(slot, armourType), 1)
		}
	}
}

// defaultArmour ports Mob.getEquipmentForSlot.
func defaultArmour(slot, armourType int) world.Item {
	tier := [...]item.ArmourTier{item.ArmourTierLeather{}, item.ArmourTierCopper{}, item.ArmourTierGold{}, item.ArmourTierChain{}, item.ArmourTierIron{}, item.ArmourTierDiamond{}}[min(armourType, 5)]
	switch slot {
	case slotHead:
		return item.Helmet{Tier: tier}
	case slotChest:
		return item.Chestplate{Tier: tier}
	case slotLegs:
		return item.Leggings{Tier: tier}
	}
	return item.Boots{Tier: tier}
}

// populateDefaultEnchantments ports Mob.populateDefaultEquipmentEnchantments:
// the weapon of a new mob is enchanted with a chance of a quarter, each
// piece of its armour with one of a half, scaled by the local difficulty.
func populateDefaultEnchantments(h equipmentHolder, r *vanilla.Random, special float32) {
	enchant := func(slot int, chance float32) {
		stack := h.equipment(slot)
		if stack.Empty() || r.NextFloat() >= chance*special {
			return
		}
		if enchanted, err := loot.EnchantMobSpawnEquipment(*stack, special, r); err == nil {
			*stack = enchanted
		}
	}
	enchant(slotMainhand, .25)
	for _, slot := range [...]int{slotFeet, slotLegs, slotChest, slotHead} {
		enchant(slot, .5)
	}
}

// halloweenHead ports the pumpkin Zombie and AbstractSkeleton.finalizeSpawn
// put on a quarter of the mobs without headgear on Halloween, which never
// drops.
func halloweenHead(h equipmentHolder, r *vanilla.Random) {
	if !h.equipment(slotHead).Empty() || !halloween() || r.NextFloat() >= .25 {
		return
	}
	*h.equipment(slotHead) = item.NewStack(block.Pumpkin{Carved: true}, 1)
	if r.NextFloat() < .1 {
		*h.equipment(slotHead) = item.NewStack(block.LitPumpkin{}, 1)
	}
	h.setDropChance(slotHead, 0)
}

// halloween ports SpecialDates.isHalloween.
func halloween() bool {
	now := time.Now()
	return now.Month() == time.October && now.Day() == 31
}

// equipItemIfPossible ports Mob.equipItemIfPossible: a mob equips a stack it
// picks up if it is better than what it has there, or holds armour it cannot
// wear better in an empty hand, maybe dropping what it replaces. The item it
// equips is then sure to drop when it dies.
func equipItemIfPossible(m *Mob, h equipmentHolder, stack item.Stack, preferred func(item.Stack) bool) item.Stack {
	slot := equipmentSlotFor(stack)
	current := *h.equipment(slot)
	replace := mobCanReplaceItem(stack, current, preferred)
	if slot != slotMainhand && slot != slotOffhand && !replace {
		slot = slotMainhand
		current = *h.equipment(slot)
		replace = current.Empty()
	}
	if !replace {
		return item.Stack{}
	}
	if !current.Empty() && float32(math.Max(float64(m.Random().NextFloat())-.1, 0)) < h.dropChance(slot) {
		m.tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: m.data.Pos}, current))
	}
	toEquip := stack.Grow(1 - stack.Count())
	*h.equipment(slot) = toEquip
	h.setDropChance(slot, 2)
	m.AI.persistent = true
	viewEquipment(m.tx, m.Self)
	return toEquip
}

// lootItem is the behaviour of an item entity, which mobs pick up loot from.
type lootItem interface {
	// lootStack returns the stack a mob may pick up: none while the pickup
	// delay lasts.
	lootStack() (item.Stack, bool)
	// takeLoot takes count items off the stack and reports if none are left.
	takeLoot(count int) (empty bool)
}

// pickableItem returns the stack of an item entity a mob may pick up: one
// without a pickup delay.
func pickableItem(e world.Entity) (item.Stack, bool) {
	ent, ok := e.(*Ent)
	if !ok {
		return item.Stack{}, false
	}
	i, ok := ent.Behaviour().(lootItem)
	if !ok {
		return item.Stack{}, false
	}
	return i.lootStack()
}

// takeItemEntity takes count items off an item entity, removing it once
// empty, as Mob.take.
func takeItemEntity(m *Mob, e world.Entity, count int) {
	ent := e.(*Ent)
	if ent.Behaviour().(lootItem).takeLoot(count) {
		_ = ent.CloseIn(m.tx)
	}
}

// pickUpLoot ports the looting half of Mob.aiStep: a mob that may pick up
// loot equips the items lying within a block of it that it wants, while mob
// griefing is on.
func pickUpLoot(m *Mob, h equipmentHolder, wants func(item.Stack) bool, preferred func(item.Stack) bool) {
	if !m.Alive() || mobGriefingDisabled(m.tx) {
		return
	}
	box := m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})
	for e := range m.tx.EntitiesOfTypesWithin(box.Grow(1), itemEntityTypes) {
		stack, ok := pickableItem(e)
		if !ok || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) || wants != nil && !wants(stack) {
			continue
		}
		equipped := equipItemIfPossible(m, h, stack, preferred)
		if equipped.Empty() {
			continue
		}
		if c, ok := m.Self.(Collector); ok {
			viewers := m.tx.Viewers(e.Position())
			for _, v := range viewers {
				v.ViewEntityAction(e, PickedUpAction{Collector: c})
			}
			m.tx.ReleaseViewers(viewers)
		}
		takeItemEntity(m, e, equipped.Count())
	}
}

// viewEquipment shows the viewers of a mob what it holds and wears.
func viewEquipment(tx *world.Tx, e world.Entity) {
	viewers := tx.Viewers(e.Position())
	for _, v := range viewers {
		v.ViewEntityItems(e)
		v.ViewEntityArmour(e)
	}
	tx.ReleaseViewers(viewers)
}

// dropEquipmentOnDeath ports the equipment half of Mob.dropCustomDeathLoot:
// each item drops with its slot's chance, raised by Looting, if a player
// killed the mob or the item is preserved; an item that is not preserved
// drops badly damaged.
func dropEquipmentOnDeath(tx *world.Tx, e world.Entity, h equipmentHolder, killedByPlayer bool, looting int) {
	for slot := range equipmentSlots {
		stack := *h.equipment(slot)
		chance := h.dropChance(slot)
		if chance == 0 || stack.Empty() {
			continue
		}
		dropMobEquipment(tx, e, stack, chance, killedByPlayer, looting)
	}
}

// equipmentArmour returns the armour and armour toughness the equipment of a
// mob gives it.
func equipmentArmour(h equipmentHolder) (armour, toughness float64) {
	for _, slot := range [...]int{slotFeet, slotLegs, slotChest, slotHead} {
		if a, ok := h.equipment(slot).Item().(item.Armour); ok {
			armour += a.DefencePoints()
			toughness += a.Toughness()
		}
	}
	return armour, toughness
}

// equipmentEnchantments returns the enchantments on the armour of a mob.
func equipmentEnchantments(h equipmentHolder) []item.Enchantment {
	var out []item.Enchantment
	for _, slot := range [...]int{slotFeet, slotLegs, slotChest, slotHead} {
		out = append(out, h.equipment(slot).Enchantments()...)
	}
	return out
}

// mobArmourAbsorb ports LivingEntity.getDamageAfterArmorAbsorb for a mob
// with base armour of its own and what its equipment gives it: armour
// absorbs damage that is not past it, as CombatRules.getDamageAfterAbsorb
// does.
func mobArmourAbsorb(h equipmentHolder, baseArmour, damage float64, src world.DamageSource) float64 {
	if !src.ReducedByArmour() {
		return damage
	}
	armour, toughness := equipmentArmour(h)
	armour += baseArmour
	t := 2 + toughness/4
	real := math.Min(math.Max(armour-damage/t, armour*.2), 20)
	return damage * (1 - real/25)
}

// mobProtectionAbsorb ports the enchantment half of
// LivingEntity.getDamageAfterMagicAbsorb.
func mobProtectionAbsorb(h equipmentHolder, damage float64, src world.DamageSource) float64 {
	if damage <= 0 {
		return 0
	}
	return damage * (1 - enchantment.ProtectionFactor(src, equipmentEnchantments(h)))
}

// equipmentSlotNames are the names Java NBT gives the equipment slots.
var equipmentSlotNames = [equipmentSlots]string{"mainhand", "offhand", "feet", "legs", "chest", "head"}

// readDropChances reads the drop_chances Java saves of the slots whose
// chance is not the default.
func readDropChances(m map[string]any) map[int]float32 {
	raw, ok := m["drop_chances"].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[int]float32, len(raw))
	for slot, name := range equipmentSlotNames {
		if v, ok := oceanFloat(raw[name]); ok {
			out[slot] = float32(v)
		}
	}
	return out
}

// encodeDropChances writes drop chances as Java's drop_chances.
func encodeDropChances(chances map[int]float32) map[string]any {
	out := make(map[string]any, len(chances))
	for slot, chance := range chances {
		out[equipmentSlotNames[slot]] = chance
	}
	return out
}

// notSpear ports the wantsToPickUp of AbstractSkeleton and Drowned, which
// leave spears.
func notSpear(s item.Stack) bool { return !inItemTag(s, "spears") }

// mobGear holds Java's six equipment slots of a mob, their drop chances and
// whether the mob may pick up loot, for mobs whose state embeds it.
type mobGear struct {
	items       [equipmentSlots]item.Stack
	dropChances map[int]float32
	pickUpLoot  bool
}

func (g *mobGear) equipment(slot int) *item.Stack { return &g.items[slot] }

func (g *mobGear) dropChance(slot int) float32 {
	if chance, ok := g.dropChances[slot]; ok {
		return chance
	}
	return defaultDropChance
}

func (g *mobGear) setDropChance(slot int, chance float32) {
	if g.dropChances == nil {
		g.dropChances = make(map[int]float32)
	}
	g.dropChances[slot] = chance
}

func (g *mobGear) canPickUpLoot() bool     { return g.pickUpLoot }
func (g *mobGear) setCanPickUpLoot(v bool) { g.pickUpLoot = v }

// heldItems returns what the mob holds in its main and off hand.
func (g *mobGear) heldItems() (item.Stack, item.Stack) {
	return g.items[slotMainhand], g.items[slotOffhand]
}

// armour returns what the mob wears, head first.
func (g *mobGear) armour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	return g.items[slotHead], g.items[slotChest], g.items[slotLegs], g.items[slotFeet]
}

// decodeGear reads the equipment of a mob from Java's equipment and
// drop_chances, Bedrock's Mainhand, Offhand and Armor lists and the
// CanPickUpLoot flag.
func decodeGear(n map[string]any) mobGear {
	var g mobGear
	if source, ok := n["equipment"].(map[string]any); ok {
		for slot, name := range equipmentSlotNames {
			if raw, ok := source[name].(map[string]any); ok {
				g.items[slot], _ = item.StackFromStructureNBT(raw)
			}
		}
	}
	equipment := ReadNativeMobEquipment(n)
	if _, ok := n["Mainhand"]; ok && len(equipment.Mainhand) > 0 {
		g.items[slotMainhand] = hostileStackFromNative(equipment.Mainhand[0])
	}
	if _, ok := n["Offhand"]; ok && len(equipment.Offhand) > 0 {
		g.items[slotOffhand] = hostileStackFromNative(equipment.Offhand[0])
	}
	for i, native := range equipment.Armor {
		if i < 4 {
			g.items[slotHead-i] = hostileStackFromNative(native)
		}
	}
	g.dropChances = readDropChances(n)
	if java := ReadNativeMobJavaState(n); java != nil {
		if g.dropChances == nil {
			g.dropChances = readDropChances(java)
		}
		g.pickUpLoot = nbtconv.Bool(java, "CanPickUpLoot")
	}
	g.pickUpLoot = g.pickUpLoot || nbtconv.Bool(n, "CanPickUpLoot")
	return g
}

// encodeGear writes the equipment of a mob into the native NBT out, keeping
// what the previous native NBT knew of its items, and returns the Java state
// to keep with it.
func (g *mobGear) encodeGear(previous, out map[string]any) map[string]any {
	equipment := ReadNativeMobEquipment(previous)
	var prevMain, prevOff NativeMobItem
	if len(equipment.Mainhand) > 0 {
		prevMain = equipment.Mainhand[0]
	}
	if len(equipment.Offhand) > 0 {
		prevOff = equipment.Offhand[0]
	}
	native := func(stack item.Stack, prev NativeMobItem) NativeMobItem {
		if stack.Empty() {
			return nativeMobItemFromStack(stack)
		}
		return hostileNativeStack(stack, prev)
	}
	equipment.Mainhand = []NativeMobItem{native(g.items[slotMainhand], prevMain)}
	equipment.Offhand = []NativeMobItem{native(g.items[slotOffhand], prevOff)}
	armor := make([]NativeMobItem, 4)
	copy(armor, equipment.Armor)
	for i := range armor {
		armor[i] = native(g.items[slotHead-i], armor[i])
	}
	equipment.Armor = armor
	for k, v := range EncodeNativeMobEquipment(equipment) {
		out[k] = v
	}
	out["CanPickUpLoot"] = byte(boolByte(g.pickUpLoot))
	java := map[string]any{}
	if len(g.dropChances) != 0 {
		java["drop_chances"] = encodeDropChances(g.dropChances)
	}
	return java
}

// wornArmour returns the armour pieces e wears.
func wornArmour(e world.Entity) []item.Stack {
	switch a := e.(type) {
	case interface{ Armour() *inventory.Armour }:
		return a.Armour().Items()
	case interface {
		EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack)
	}:
		head, chest, legs, feet := a.EquipmentArmour()
		return []item.Stack{head, chest, legs, feet}
	}
	return nil
}

// armourSlot returns the armour slot, head first, a stack is worn in.
func armourSlot(stack item.Stack) (int, bool) {
	switch stack.Item().(type) {
	case item.HelmetType:
		return 0, true
	case item.ChestplateType:
		return 1, true
	case item.LeggingsType:
		return 2, true
	case item.BootsType:
		return 3, true
	}
	return 0, false
}

// mobCanReplaceItem ports Mob.canReplaceCurrentItem for an item in a slot that
// holds current: better armour and weapons, the preferred weapons first.
func mobCanReplaceItem(stack, current item.Stack, preferred func(item.Stack) bool) bool {
	if current.Empty() {
		return true
	}
	if _, armour := armourSlot(stack); armour {
		na, _ := stack.Item().(item.Armour)
		ca, _ := current.Item().(item.Armour)
		if na == nil || ca == nil {
			return false
		}
		if na.DefencePoints() != ca.DefencePoints() {
			return na.DefencePoints() > ca.DefencePoints()
		}
		if na.Toughness() != ca.Toughness() {
			return na.Toughness() > ca.Toughness()
		}
		return mobCanReplaceEqualItem(stack, current)
	}
	if preferred != nil {
		if preferred(current) && !preferred(stack) {
			return false
		}
		if !preferred(current) && preferred(stack) {
			return true
		}
	}
	nd, cd := stack.AttackDamage(), current.AttackDamage()
	if nd != cd {
		return nd > cd
	}
	return mobCanReplaceEqualItem(stack, current)
}

// mobCanReplaceEqualItem ports Mob.canReplaceEqualItem.
func mobCanReplaceEqualItem(stack, current item.Stack) bool {
	if n, c := len(stack.Enchantments()), len(current.Enchantments()); n != c {
		return n > c
	}
	damage := func(st item.Stack) int { return st.MaxDurability() - st.Durability() }
	if damage(stack) != damage(current) {
		return damage(stack) < damage(current)
	}
	return stack.CustomName() != "" && current.CustomName() == ""
}

// dropMobEquipment ports one slot of Mob.dropCustomDeathLoot: a worn item
// rolls its drop chance, raised by looting, when a player killed the mob or
// the chance exceeds 1 (a preserved item, which also keeps its durability).
func dropMobEquipment(tx *world.Tx, e world.Entity, stack item.Stack, chance float32, killedByPlayer bool, looting int) {
	if stack.Empty() {
		return
	}
	preserve := chance > 1
	if chance == 0 || (!killedByPlayer && !preserve) {
		return
	}
	if oceanStackEnchantmentLevel(stack, "Curse of Vanishing", "minecraft:vanishing_curse") > 0 {
		return
	}
	random := e.H().RegionRandom()
	if random.NextFloat() >= oceanEquipmentDropChance(chance, looting) {
		return
	}
	if !preserve {
		if durable, ok := stack.Item().(item.Durable); ok {
			maxDamage := durable.DurabilityInfo().MaxDurability
			if maxDamage > 0 {
				remaining := random.NextInt(1 + random.NextInt(int32(max(maxDamage-3, 1))))
				stack = stack.WithDurability(int(remaining))
			}
		}
	}
	tx.AddEntity(NewItemDrop(world.EntitySpawnOpts{Position: e.Position()}, stack))
}

func oceanEquipmentDropChance(chance float32, looting int) float32 {
	if looting > 0 {
		chance += float32(looting) * float32(.01)
	}
	return chance
}

func oceanStackEnchantmentLevel(main item.Stack, nativeName, sourceID string) int {
	if main.Empty() {
		return 0
	}
	level := 0
	for _, enchantment := range main.Enchantments() {
		if enchantment.Type().Name() == nativeName && enchantment.Level() > level {
			level = enchantment.Level()
		}
	}
	if raw, ok := main.Value("java_components_json"); ok {
		if encoded, ok := raw.(string); ok {
			var components map[string]any
			if json.Unmarshal([]byte(encoded), &components) == nil {
				if enchantments, ok := components["minecraft:enchantments"].(map[string]any); ok {
					levels, _ := enchantments["levels"].(map[string]any)
					if levels == nil {
						levels = enchantments
					}
					if rawLevel, ok := levels[sourceID]; ok {
						if n, ok := oceanFloat(rawLevel); ok && int(n) > level {
							level = int(n)
						}
					}
				}
			}
		}
	}
	return level
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
