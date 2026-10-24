package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
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

// armourSlotIndex maps an equipment slot to the head-first index of the
// armour lists Bedrock saves and sends.
func armourSlotIndex(slot int) int { return slotHead - slot }

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
	viewEquipment(m.tx, m.Self)
	return toEquip
}

// pickUpLoot ports the looting half of Mob.aiStep: a mob that may pick up
// loot equips the items lying within a block of it that it wants, while mob
// griefing is on.
func pickUpLoot(m *Mob, h equipmentHolder, wants func(item.Stack) bool, preferred func(item.Stack) bool) {
	if !m.Alive() || mobGriefingDisabled(m.tx) {
		return
	}
	box := m.BBox().GrowVec3(mgl64.Vec3{1, 0, 1})
	for e := range m.tx.EntitiesWithin(box.Grow(1)) {
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

func (c *hostileConfig) equipment(slot int) *item.Stack {
	return [...]*item.Stack{&c.main, &c.off, &c.feet, &c.legs, &c.chest, &c.head}[slot]
}

func (b *hostileState) equipment(slot int) *item.Stack {
	return [...]*item.Stack{&b.main, &b.off, &b.feet, &b.legs, &b.chest, &b.head}[slot]
}

func (b *hostileState) dropChance(slot int) float32 {
	if slot == slotHead && b.headDropGuaranteed {
		return 2
	}
	if chance, ok := b.dropChances[slot]; ok {
		return chance
	}
	return defaultDropChance
}

func (b *hostileState) setDropChance(slot int, chance float32) {
	if b.dropChances == nil {
		b.dropChances = make(map[int]float32)
	}
	b.dropChances[slot] = chance
}

// afterAiStep picks up loot as Mob.aiStep does for the zombies and
// skeletons that may. Raiders pick up loot of their own accord.
func (b *hostileState) afterAiStep(m *Mob) {
	if !b.canPickupLoot || b.dead || hostileRaidMob(b.kind) || b.kind == "minecraft:vex" || b.kind == "minecraft:witch" {
		return
	}
	var wants, preferred func(item.Stack) bool
	switch {
	case b.kind == "minecraft:wither_skeleton":
		// WitherSkeleton.getPreferredWeaponType is null.
		wants = notSpear
	case hostileSkeleton(b.kind):
		wants = notSpear
		preferred = func(s item.Stack) bool { return inItemTag(s, "skeleton_preferred_weapons") }
	}
	pickUpLoot(m, b, wants, preferred)
}

// afterAiStep picks up loot as Mob.aiStep does for drowned that may, which
// prefer tridents and leave spears.
func (b *oceanBehaviour) afterAiStep(m *Mob) {
	if b.kind != oceanDrowned || !b.canPickupLoot || b.dead {
		return
	}
	pickUpLoot(m, b, notSpear, func(s item.Stack) bool { return inItemTag(s, "drowned_preferred_weapons") })
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

// afterAiStep picks up loot as Mob.aiStep does for zombified piglins that
// may.
func (s *zombifiedPiglinState) afterAiStep(m *Mob) {
	if s.pickUpLoot && !s.dead {
		pickUpLoot(m, s, nil, nil)
	}
}

func (s *piglinState) equipment(slot int) *item.Stack {
	return [...]*item.Stack{&s.main, &s.off, &s.armor[3], &s.armor[2], &s.armor[1], &s.armor[0]}[slot]
}

func (s *piglinState) dropChance(slot int) float32 {
	switch {
	case slot == slotMainhand && s.guaranteedMain, slot == slotOffhand && s.guaranteedOff:
		return 2
	}
	return defaultDropChance
}

func (s *piglinState) setDropChance(slot int, chance float32) {
	switch slot {
	case slotMainhand:
		s.guaranteedMain = chance > 1
	case slotOffhand:
		s.guaranteedOff = chance > 1
	}
}
