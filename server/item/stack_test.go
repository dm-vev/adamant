package item

import (
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/nbt"
)

// ItemStack.hurtAndBreak and setDamageValue: durability is the maximum minus
// the damage, an item breaks when its damage reaches the maximum, repairs
// stop at full durability, and unbreakable or undamageable items take none.
// The elytra stops at 1 durability instead of breaking
// (LivingEntity.canGlideUsing checks ItemStack.nextDamageWillBreak).
func TestStackDamage(t *testing.T) {
	sword := NewStack(Sword{Tier: ToolTierDiamond}, 1)
	if sword.MaxDurability() != 1561 || sword.Durability() != 1561 {
		t.Fatalf("diamond sword durability %d/%d, want 1561/1561", sword.Durability(), sword.MaxDurability())
	}
	for _, c := range []struct {
		name   string
		stack  Stack
		damage []int
		want   int
		broken bool
	}{
		{"one hit", sword, []int{1}, 1560, false},
		{"several hits add up", sword, []int{10, 5}, 1546, false},
		{"one left", sword, []int{1560}, 1, false},
		{"exactly broken", sword, []int{1561}, 0, true},
		{"more than left", sword, []int{1000, 600}, 0, true},
		{"repair", sword, []int{100, -40}, 1501, false},
		{"repair stops at full", sword, []int{10, -50}, 1561, false},
		{"zero damage", sword, []int{0}, 1561, false},
		{"unbreakable", sword.AsUnbreakable(), []int{5000}, 1561, false},
		{"elytra keeps its last point", NewStack(Elytra{}, 1), []int{431, 1, 50}, 1, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := c.stack
			for _, d := range c.damage {
				s = s.Damage(d)
			}
			if s.Empty() != c.broken {
				t.Fatalf("broken = %v, want %v", s.Empty(), c.broken)
			}
			if !c.broken && s.Durability() != c.want {
				t.Fatalf("durability %d, want %d", s.Durability(), c.want)
			}
		})
	}
	apple := NewStack(Apple{}, 3)
	if got := apple.Damage(5); !got.Equal(apple) || got.Durability() != -1 || got.MaxDurability() != -1 {
		t.Fatalf("an apple took damage: %v", got)
	}
	if got := sword.AsUnbreakable().AsBreakable(); got.Unbreakable() || got.Damage(1).Durability() != 1560 {
		t.Fatal("AsBreakable did not make the sword breakable again")
	}
	if apple.AsUnbreakable().Unbreakable() {
		t.Fatal("an apple became unbreakable")
	}
}

func TestStackWithDurability(t *testing.T) {
	sword := NewStack(Sword{Tier: ToolTierIron}, 1)
	for in, want := range map[int]int{250: 250, 1000: 250, 100: 100, 1: 1} {
		if got := sword.Damage(10).WithDurability(in).Durability(); got != want {
			t.Errorf("WithDurability(%d) = %d, want %d", in, got, want)
		}
	}
	if !sword.WithDurability(0).Empty() {
		t.Error("WithDurability(0) did not break the sword")
	}
	if got := NewStack(Apple{}, 1).WithDurability(5); got.Durability() != -1 {
		t.Error("an apple got a durability")
	}
}

// The durability items lose per use follow their Java weapon and tool
// components: Weapon.itemDamagePerAttack and Tool.damagePerBlock. Swords and
// maces take 1 per hit and 2 per block, digging tools 2 per hit and 1 per
// block, shears 1 per block only.
func TestDurabilityPerUse(t *testing.T) {
	for _, c := range []struct {
		item          Durable
		max           int
		attack, block int
	}{
		{Sword{Tier: ToolTierWood}, 59, 1, 2},
		{Sword{Tier: ToolTierNetherite}, 2031, 1, 2},
		{Pickaxe{Tier: ToolTierStone}, 131, 2, 1},
		{Axe{Tier: ToolTierGold}, 32, 2, 1},
		{Shovel{Tier: ToolTierIron}, 250, 2, 1},
		{Hoe{Tier: ToolTierDiamond}, 1561, 2, 1},
		{Mace{}, 500, 1, 2},
		{Shears{}, 238, 0, 1},
		{Bow{}, 384, 0, 0},
		{FishingRod{}, 64, 0, 0},
		{Elytra{}, 432, 0, 0},
	} {
		info := c.item.DurabilityInfo()
		if info.MaxDurability != c.max || info.AttackDurability != c.attack || info.BreakDurability != c.block {
			t.Errorf("%T: max %d, per hit %d, per block %d; want %d, %d, %d", c.item, info.MaxDurability, info.AttackDurability, info.BreakDurability, c.max, c.attack, c.block)
		}
		if !info.BrokenItem().Empty() {
			t.Errorf("%T breaks into %v", c.item, info.BrokenItem())
		}
	}
}

// Merging follows ItemStack.isSameItemSameComponents and the item's max stack
// size: the receiver fills up to its limit and the rest stays behind.
func TestStackMerging(t *testing.T) {
	RegisterEnchantment(255, testEnchantment{})
	diamond := func(n int) Stack { return NewStack(Diamond{}, n) }
	pearl := func(n int) Stack { return NewStack(EnderPearl{}, n) }
	for _, c := range []struct {
		name       string
		a, b       Stack
		got, rest  int
		comparable bool
	}{
		{"fits", diamond(10), diamond(20), 30, 0, true},
		{"fills to 64", diamond(60), diamond(10), 64, 6, true},
		{"full receiver", diamond(64), diamond(1), 64, 1, true},
		{"pearls stack to 16", pearl(10), pearl(10), 16, 4, true},
		{"swords stack to 1", NewStack(Sword{Tier: ToolTierIron}, 1), NewStack(Sword{Tier: ToolTierIron}, 1), 1, 1, true},
		{"different items", diamond(1), NewStack(Emerald{}, 1), 1, 1, false},
		{"different names", diamond(1).WithCustomName("a"), diamond(1).WithCustomName("b"), 1, 1, false},
		{"name against none", diamond(1).WithCustomName("a"), diamond(1), 1, 1, false},
		{"different lore", diamond(1).WithLore("a"), diamond(1).WithLore("a", "b"), 1, 1, false},
		{"same lore", diamond(1).WithLore("a"), diamond(1).WithLore("a"), 2, 0, true},
		{"different enchantments", diamond(1).WithForcedEnchantments(NewEnchantment(testEnchantment{}, 1)), diamond(1), 1, 1, false},
		{"different enchantment levels", diamond(1).WithForcedEnchantments(NewEnchantment(testEnchantment{}, 1)), diamond(1).WithForcedEnchantments(NewEnchantment(testEnchantment{}, 2)), 1, 1, false},
		{"different values", diamond(1).WithValue("k", 1), diamond(1).WithValue("k", 2), 1, 1, false},
		{"different locks", diamond(1).WithLockMode(LockInSlot), diamond(1), 1, 1, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.a.Comparable(c.b) != c.comparable || c.b.Comparable(c.a) != c.comparable {
				t.Fatalf("comparable = %v, want %v", c.a.Comparable(c.b), c.comparable)
			}
			a, b := c.a.AddStack(c.b)
			if a.Count() != c.got || b.Count() != c.rest {
				t.Fatalf("merged %d and %d, want %d and %d", a.Count(), b.Count(), c.got, c.rest)
			}
			if a.Count()+b.Count() != c.a.Count()+c.b.Count() {
				t.Fatal("merging changed the item total")
			}
		})
	}
	// Equal also compares the count and damage; Comparable does not.
	sword := NewStack(Sword{Tier: ToolTierIron}, 1)
	if !sword.Comparable(sword.Damage(3)) || sword.Equal(sword.Damage(3)) || sword.Equal(sword.Grow(1)) || !sword.Equal(sword.Damage(0)) {
		t.Fatal("Equal or Comparable compared the wrong fields")
	}
}

// Merging into an empty slot moves the stack there, as Slot.safeInsert does
// for an empty slot: the items keep their type and only the slot's limit
// applies.
func TestStackMergingIntoEmpty(t *testing.T) {
	for name, empty := range map[string]Stack{
		"zero stack":      {},
		"empty of a type": NewStack(Sword{Tier: ToolTierIron}, 0),
		"emptied by grow": NewStack(Diamond{}, 5).Grow(-10),
	} {
		t.Run(name, func(t *testing.T) {
			a, b := empty.AddStack(NewStack(Diamond{}, 70).Grow(-6))
			if _, ok := a.Item().(Diamond); !ok || a.Count() != 64 || !b.Empty() {
				t.Fatalf("merged into %v, left %v; want 64 diamonds and nothing", a, b)
			}
			a, b = empty.AddStack(NewStack(EnderPearl{}, 16).Grow(4))
			if _, ok := a.Item().(EnderPearl); !ok || a.Count() != 16 || b.Count() != 4 {
				t.Fatalf("merged into %v, left %v; want 16 pearls and 4", a, b)
			}
		})
	}
}

func TestStackCountAndValues(t *testing.T) {
	s := NewStack(Diamond{}, 5)
	if s.Grow(-10).Count() != 0 || !s.Grow(-5).Empty() || s.Grow(3).Count() != 8 {
		t.Fatal("Grow did not clamp at zero")
	}
	if s.MaxCount() != 64 || NewStack(EnderPearl{}, 1).MaxCount() != 16 {
		t.Fatal("max counts differ from Java's max_stack_size")
	}
	lore := []string{"a", "b"}
	s = s.WithLore(lore...)
	lore[0] = "changed"
	if s.Lore()[0] != "a" {
		t.Fatal("WithLore shares the caller's slice")
	}
	s.Lore()[0] = "changed"
	if s.Lore()[0] != "a" {
		t.Fatal("Lore returns the stack's own slice")
	}
	v := s.WithValue("k", 1)
	if _, ok := s.Value("k"); ok {
		t.Fatal("WithValue changed the original stack")
	}
	if got, _ := v.Value("k"); got != 1 {
		t.Fatalf("value %v", got)
	}
	if v = v.WithValue("k", nil); v.Values() != nil {
		t.Fatalf("clearing the last value left %v", v.Values())
	}
	if (Stack{}).Lore() != nil || (Stack{}).Item() != nil || (Stack{}).Enchantments() != nil {
		t.Fatal("an empty stack has contents")
	}
	if func() (panicked bool) {
		defer func() { panicked = recover() != nil }()
		NewStack(Diamond{}, -1)
		return
	}() == false {
		t.Fatal("NewStack accepted a negative count")
	}
}

// WithItem keeps what still applies to the new item: names, lore and values
// always, damage and unbreakable only on a durable item.
func TestStackWithItem(t *testing.T) {
	sword := NewStack(Sword{Tier: ToolTierIron}, 1).Damage(50).WithCustomName("Blade").WithLore("x").WithValue("k", "v").AsUnbreakable()
	axe := sword.WithItem(Axe{Tier: ToolTierIron})
	if axe.Durability() != 200 || !axe.Unbreakable() || axe.CustomName() != "Blade" || axe.Lore()[0] != "x" {
		t.Fatalf("axe = %v", axe)
	}
	stick := sword.WithItem(Stick{})
	if stick.Unbreakable() || stick.Durability() != -1 || stick.CustomName() != "Blade" {
		t.Fatalf("stick = %v", stick)
	}
	if v, _ := stick.Value("k"); v != "v" {
		t.Fatal("WithItem dropped a value")
	}
}

// A stack survives the world's NBT format, through binary encoding, with its
// count, damage, name, lore, enchantments, anvil cost, lock, unbreakable tag
// and custom values.
func TestStackNBTRoundTrip(t *testing.T) {
	RegisterEnchantment(255, testEnchantment{})
	for name, s := range map[string]Stack{
		"plain":     NewStack(Diamond{}, 64),
		"named":     NewStack(Diamond{}, 3).WithCustomName("Shiny").WithLore("one", "two"),
		"damaged":   NewStack(Sword{Tier: ToolTierDiamond}, 1).Damage(77).WithAnvilCost(3),
		"enchanted": NewStack(Sword{Tier: ToolTierIron}, 1).WithForcedEnchantments(NewEnchantment(testEnchantment{}, 2)),
		"locked":    NewStack(Apple{}, 5).WithLockMode(LockInInventory),
		"unbreak":   NewStack(Pickaxe{Tier: ToolTierIron}, 1).AsUnbreakable(),
		"values":    NewStack(Stick{}, 2).WithValue("owner", "steve").WithValue("n", int64(4)),
	} {
		t.Run(name, func(t *testing.T) {
			data, err := nbt.MarshalEncoding(WriteNBT(s), nbt.LittleEndian)
			if err != nil {
				t.Fatal(err)
			}
			var decoded map[string]any
			if err = nbt.UnmarshalEncoding(data, &decoded, nbt.LittleEndian); err != nil {
				t.Fatal(err)
			}
			got := ReadNBT(decoded)
			if !got.Equal(s) || got.Unbreakable() != s.Unbreakable() || got.Durability() != s.Durability() {
				t.Fatalf("read back %v, want %v", got, s)
			}
			if !reflect.DeepEqual(got.Values(), s.Values()) {
				t.Fatalf("values %v, want %v", got.Values(), s.Values())
			}
		})
	}
	if len(WriteNBT(Stack{})) != 0 || !ReadNBT(map[string]any{"Name": "minecraft:not_an_item", "Count": byte(1)}).Empty() {
		t.Fatal("an empty or unknown stack has NBT")
	}
}
