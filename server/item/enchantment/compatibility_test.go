package enchantment_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

// Java's enchantment exclusive sets (data/minecraft/tags/enchantment/
// exclusive_set/*.json): Enchantment.areCompatible is false for two different
// enchantments when either lists the other's set.
var exclusivePairs = []struct {
	set  string
	a, b item.EnchantmentType
}{
	{"damage", enchantment.Sharpness, enchantment.Smite},
	{"damage", enchantment.Sharpness, enchantment.BaneOfArthropods},
	{"damage", enchantment.Smite, enchantment.BaneOfArthropods},
	{"damage", enchantment.Density, enchantment.Breach},
	{"damage", enchantment.Density, enchantment.Smite},
	{"damage", enchantment.Density, enchantment.BaneOfArthropods},
	{"damage", enchantment.Density, enchantment.Sharpness},
	{"damage", enchantment.Breach, enchantment.Smite},
	{"damage", enchantment.Breach, enchantment.BaneOfArthropods},
	{"damage", enchantment.Breach, enchantment.Sharpness},
	{"damage", enchantment.Impaling, enchantment.Sharpness},
	{"damage", enchantment.Impaling, enchantment.Smite},
	{"damage", enchantment.Impaling, enchantment.BaneOfArthropods},
	{"damage", enchantment.Impaling, enchantment.Density},
	{"damage", enchantment.Impaling, enchantment.Breach},
	{"armor", enchantment.Protection, enchantment.FireProtection},
	{"armor", enchantment.Protection, enchantment.BlastProtection},
	{"armor", enchantment.Protection, enchantment.ProjectileProtection},
	{"armor", enchantment.FireProtection, enchantment.BlastProtection},
	{"armor", enchantment.BlastProtection, enchantment.ProjectileProtection},
	{"boots", enchantment.FrostWalker, enchantment.DepthStrider},
	{"bow", enchantment.Infinity, enchantment.Mending},
	{"crossbow", enchantment.Multishot, enchantment.Piercing},
	{"mining", enchantment.SilkTouch, enchantment.Fortune},
	{"riptide", enchantment.Riptide, enchantment.Loyalty},
	{"riptide", enchantment.Riptide, enchantment.Channeling},
}

// Pairs Java allows together on one item.
var compatiblePairs = []struct {
	a, b item.EnchantmentType
	on   world.Item
}{
	{enchantment.Sharpness, enchantment.FireAspect, item.Sword{Tier: item.ToolTierIron}},
	{enchantment.Sharpness, enchantment.Looting, item.Sword{Tier: item.ToolTierIron}},
	{enchantment.Density, enchantment.WindBurst, item.Mace{}},
	{enchantment.Protection, enchantment.Thorns, item.Chestplate{Tier: item.ArmourTierIron{}}},
	{enchantment.FeatherFalling, enchantment.DepthStrider, item.Boots{Tier: item.ArmourTierIron{}}},
	{enchantment.Loyalty, enchantment.Channeling, item.Trident{}},
	{enchantment.Impaling, enchantment.Riptide, item.Trident{}},
	{enchantment.Power, enchantment.Infinity, item.Bow{}},
	{enchantment.Piercing, enchantment.QuickCharge, item.Crossbow{}},
	{enchantment.Unbreaking, enchantment.Mending, item.Pickaxe{Tier: item.ToolTierIron}},
	{enchantment.Efficiency, enchantment.Fortune, item.Pickaxe{Tier: item.ToolTierIron}},
}

func TestEnchantmentExclusiveSets(t *testing.T) {
	for _, c := range exclusivePairs {
		name := c.set + ": " + c.a.Name() + " + " + c.b.Name()
		if c.a.CompatibleWithEnchantment(c.b) || c.b.CompatibleWithEnchantment(c.a) {
			t.Errorf("%s: compatible, want exclusive", name)
		}
		// The second is refused, whichever comes first, even on a book, which
		// takes any enchantment.
		for _, order := range [][2]item.EnchantmentType{{c.a, c.b}, {c.b, c.a}} {
			s := item.NewStack(item.Book{}, 1).WithEnchantments(item.NewEnchantment(order[0], 1), item.NewEnchantment(order[1], 1))
			if _, ok := s.Enchantment(order[0]); !ok || len(s.Enchantments()) != 1 {
				t.Errorf("%s: applying %s then %s left %v", name, order[0].Name(), order[1].Name(), s.Enchantments())
			}
		}
	}
	for _, c := range compatiblePairs {
		s := item.NewStack(c.on, 1).WithEnchantments(item.NewEnchantment(c.a, 1), item.NewEnchantment(c.b, 1))
		if len(s.Enchantments()) != 2 {
			t.Errorf("%s + %s on %T: kept %v, want both", c.a.Name(), c.b.Name(), c.on, s.Enchantments())
		}
	}
}

// Supported items follow the enchantable item tags: sharp weapons are
// swords and axes, mining enchantments go on mining tools, and so on.
func TestEnchantmentSupportedItems(t *testing.T) {
	sword, axe, pickaxe := item.Sword{Tier: item.ToolTierDiamond}, item.Axe{Tier: item.ToolTierDiamond}, item.Pickaxe{Tier: item.ToolTierDiamond}
	shovel, hoe := item.Shovel{Tier: item.ToolTierDiamond}, item.Hoe{Tier: item.ToolTierDiamond}
	helmet, chest := item.Helmet{Tier: item.ArmourTierDiamond{}}, item.Chestplate{Tier: item.ArmourTierDiamond{}}
	legs, boots := item.Leggings{Tier: item.ArmourTierDiamond{}}, item.Boots{Tier: item.ArmourTierDiamond{}}
	for _, c := range []struct {
		e        item.EnchantmentType
		yes, not []world.Item
	}{
		{enchantment.Sharpness, []world.Item{sword, axe}, []world.Item{pickaxe, item.Bow{}, item.Trident{}, helmet}},
		{enchantment.Smite, []world.Item{sword, axe}, []world.Item{pickaxe, item.Bow{}}},
		{enchantment.Knockback, []world.Item{sword}, []world.Item{pickaxe, item.Bow{}}},
		{enchantment.Looting, []world.Item{sword}, []world.Item{pickaxe, item.Bow{}}},
		{enchantment.Efficiency, []world.Item{pickaxe, axe, shovel, hoe, item.Shears{}}, []world.Item{sword, helmet}},
		{enchantment.SilkTouch, []world.Item{pickaxe, axe, shovel, hoe}, []world.Item{sword, item.Bow{}}},
		{enchantment.Fortune, []world.Item{pickaxe, axe, shovel, hoe}, []world.Item{sword, item.Bow{}}},
		{enchantment.Power, []world.Item{item.Bow{}}, []world.Item{item.Crossbow{}, sword}},
		{enchantment.Infinity, []world.Item{item.Bow{}}, []world.Item{item.Crossbow{}}},
		{enchantment.Multishot, []world.Item{item.Crossbow{}}, []world.Item{item.Bow{}}},
		{enchantment.Loyalty, []world.Item{item.Trident{}}, []world.Item{sword, item.Bow{}}},
		{enchantment.Breach, []world.Item{item.Mace{}}, []world.Item{sword}},
		{enchantment.Protection, []world.Item{helmet, chest, legs, boots}, []world.Item{sword, item.Shears{}}},
		{enchantment.Respiration, []world.Item{helmet}, []world.Item{chest, boots}},
		{enchantment.AquaAffinity, []world.Item{helmet}, []world.Item{chest, boots}},
		{enchantment.SwiftSneak, []world.Item{legs}, []world.Item{helmet, boots}},
		{enchantment.FeatherFalling, []world.Item{boots}, []world.Item{helmet, legs}},
		{enchantment.DepthStrider, []world.Item{boots}, []world.Item{helmet}},
		{enchantment.Mending, []world.Item{sword, pickaxe, helmet, item.Bow{}, item.Elytra{}}, []world.Item{item.Stick{}}},
		{enchantment.Unbreaking, []world.Item{sword, helmet, item.Shears{}, item.Elytra{}}, []world.Item{item.Stick{}}},
	} {
		for _, i := range c.yes {
			if !c.e.CompatibleWithItem(i) {
				t.Errorf("%s does not go on %T", c.e.Name(), i)
			}
		}
		for _, i := range c.not {
			if c.e.CompatibleWithItem(i) {
				t.Errorf("%s goes on %T", c.e.Name(), i)
			}
		}
	}
}

// Java's #enchantable/weapon and #enchantable/fire_aspect item tags hold the
// mace, so a mace takes Smite, Bane of Arthropods and Fire Aspect from books
// in an anvil and from /enchant (the wiki's Mace page lists them). Their
// primary_items, #enchantable/melee_weapon, do not hold it, so an
// enchanting table does not offer them for a mace.
func TestMaceTakesWeaponEnchantments(t *testing.T) {
	for _, e := range []item.EnchantmentType{enchantment.Smite, enchantment.BaneOfArthropods, enchantment.FireAspect} {
		if !e.CompatibleWithItem(item.Mace{}) {
			t.Errorf("%s does not go on a mace", e.Name())
		}
		if item.IsPrimaryItem(e, item.Mace{}) || !item.IsPrimaryItem(e, item.Sword{Tier: item.ToolTierIron}) {
			t.Errorf("%s: a mace is a primary item, or a sword is not", e.Name())
		}
	}
	// Sharpness, Knockback and Looting stay off a mace.
	for _, e := range []item.EnchantmentType{enchantment.Sharpness, enchantment.Knockback, enchantment.Looting} {
		if e.CompatibleWithItem(item.Mace{}) {
			t.Errorf("%s goes on a mace", e.Name())
		}
	}
}

// Java's #enchantable/melee_weapon item tag holds #swords and #spears, so a
// spear takes Sharpness, Smite, Bane of Arthropods, Knockback, Fire Aspect
// and Looting (data/minecraft/enchantment/*.json), which name it in their
// supported_items, and an enchanting table offers them for it, as their
// primary_items name it too or they have none.
func TestSpearsTakeMeleeWeaponEnchantments(t *testing.T) {
	for _, tier := range item.ToolTiers() {
		spear := item.Spear{Tier: tier}
		for _, e := range []item.EnchantmentType{
			enchantment.Sharpness, enchantment.Smite, enchantment.BaneOfArthropods,
			enchantment.Knockback, enchantment.FireAspect, enchantment.Looting,
		} {
			if !e.CompatibleWithItem(spear) || !item.IsPrimaryItem(e, spear) {
				t.Errorf("%s does not go on a %s spear, or a table does not offer it", e.Name(), tier.Name)
			}
		}
	}
}

// Java's #enchantable/armor item tag holds #head_armor, #chest_armor,
// #leg_armor and #foot_armor: the helmets, the turtle shell, chestplates,
// leggings and boots. The elytra, skulls, carved pumpkins and the armour of
// animals are worn but are not in it, so the protections and Thorns, whose
// supported_items it is, do not go on them; nor do Respiration and Aqua
// Affinity (#enchantable/head_armor) on a skull or a carved pumpkin. Curse
// of Binding (#enchantable/equippable) and Curse of Vanishing
// (#enchantable/vanishing) go on the elytra, skulls and carved pumpkins, and
// not on the armour of animals or a pumpkin.
func TestArmourTagsLeaveOutTheElytra(t *testing.T) {
	diamond := item.ArmourTierDiamond{}
	pieces := []world.Item{
		item.Helmet{Tier: diamond}, item.TurtleShell{}, item.Chestplate{Tier: diamond},
		item.Leggings{Tier: diamond}, item.Boots{Tier: diamond},
	}
	skull, carved, pumpkin := block.Skull{Type: block.ZombieHead()}, block.Pumpkin{Carved: true}, block.Pumpkin{}
	worn := []world.Item{item.Elytra{}, skull, carved, pumpkin, item.IronHorseArmour{}, item.WolfArmor{}, item.NautilusArmor{}}
	for _, e := range []item.EnchantmentType{
		enchantment.Protection, enchantment.FireProtection, enchantment.BlastProtection,
		enchantment.ProjectileProtection, enchantment.Thorns,
	} {
		for _, i := range pieces {
			if !e.CompatibleWithItem(i) {
				t.Errorf("%s does not go on %T", e.Name(), i)
			}
		}
		for _, i := range worn {
			if e.CompatibleWithItem(i) {
				t.Errorf("%s goes on %T %v", e.Name(), i, i)
			}
		}
	}
	for _, e := range []item.EnchantmentType{enchantment.Respiration, enchantment.AquaAffinity} {
		for _, i := range []world.Item{item.Helmet{Tier: diamond}, item.TurtleShell{}} {
			if !e.CompatibleWithItem(i) {
				t.Errorf("%s does not go on %T", e.Name(), i)
			}
		}
		for _, i := range []world.Item{skull, carved} {
			if e.CompatibleWithItem(i) {
				t.Errorf("%s goes on %T %v", e.Name(), i, i)
			}
		}
	}
	for _, i := range append(pieces, item.Elytra{}, skull, carved) {
		if !enchantment.CurseOfBinding.CompatibleWithItem(i) || !enchantment.CurseOfVanishing.CompatibleWithItem(i) {
			t.Errorf("a curse does not go on %T %v", i, i)
		}
	}
	for _, i := range []world.Item{pumpkin, item.IronHorseArmour{}, item.WolfArmor{}, item.NautilusArmor{}} {
		if enchantment.CurseOfBinding.CompatibleWithItem(i) {
			t.Errorf("Curse of Binding goes on %T %v", i, i)
		}
	}
	// Wolf armour takes Curse of Vanishing as an item.Durable.
	for _, i := range []world.Item{pumpkin, item.IronHorseArmour{}, item.NautilusArmor{}} {
		if enchantment.CurseOfVanishing.CompatibleWithItem(i) {
			t.Errorf("Curse of Vanishing goes on %T %v", i, i)
		}
	}
}

// A book becomes an enchanted book holding any enchantment, as enchanting a
// book in an enchanting table or with /enchant does, but its enchantments
// still respect the exclusive sets. Removing the last turns it back into a
// book.
func TestEnchantingBooks(t *testing.T) {
	s := item.NewStack(item.Book{}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 5), item.NewEnchantment(enchantment.Protection, 4), item.NewEnchantment(enchantment.Smite, 5))
	if _, ok := s.Item().(item.EnchantedBook); !ok {
		t.Fatalf("enchanted book is %T", s.Item())
	}
	if got := len(s.Enchantments()); got != 2 {
		t.Fatalf("book kept %v, want sharpness and protection", s.Enchantments())
	}
	s = s.WithoutEnchantments(enchantment.Sharpness, enchantment.Protection)
	if _, ok := s.Item().(item.Book); !ok {
		t.Fatalf("a book without enchantments is %T", s.Item())
	}
	// Re-applying an enchantment replaces its level.
	sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 1))
	sword = sword.WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 4))
	if e, _ := sword.Enchantment(enchantment.Sharpness); e.Level() != 4 || len(sword.Enchantments()) != 1 {
		t.Fatalf("re-applied sharpness = %v", sword.Enchantments())
	}
	// An unsupported enchantment is dropped; forcing it is not.
	sword = sword.WithEnchantments(item.NewEnchantment(enchantment.Power, 1))
	if _, ok := sword.Enchantment(enchantment.Power); ok {
		t.Fatal("power applied to a sword")
	}
	sword = sword.WithForcedEnchantments(item.NewEnchantment(enchantment.Smite, 2))
	if _, ok := sword.Enchantment(enchantment.Smite); !ok {
		t.Fatal("a forced enchantment was dropped")
	}
}
