package item_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

func TestSpearMaterialStats(t *testing.T) {
	tests := []struct {
		tier             item.ToolTier
		name             string
		damage, multiplier float64
		delay, durability int
		attackDuration   float64
	}{
		{item.ToolTierWood, "wooden_spear", 1, 0.7, 15, 59, 0.65},
		{item.ToolTierStone, "stone_spear", 2, 0.82, 14, 131, 0.75},
		{item.ToolTierCopper, "copper_spear", 2, 0.82, 13, 190, 0.85},
		{item.ToolTierIron, "iron_spear", 3, 0.95, 12, 250, 0.95},
		{item.ToolTierGold, "golden_spear", 1, 0.7, 14, 32, 0.95},
		{item.ToolTierDiamond, "diamond_spear", 4, 1.075, 10, 1561, 1.05},
		{item.ToolTierNetherite, "netherite_spear", 5, 1.2, 8, 2031, 1.15},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spear := item.Spear{Tier: tc.tier}
			name, meta := spear.EncodeItem()
			if name != "minecraft:"+tc.name || meta != 0 {
				t.Fatalf("EncodeItem() = %q, %d", name, meta)
			}
			if got := spear.AttackDamage(); got != tc.damage {
				t.Fatalf("AttackDamage() = %v, want %v", got, tc.damage)
			}
			if got := spear.DurabilityInfo().MaxDurability; got != tc.durability {
				t.Fatalf("MaxDurability = %d, want %d", got, tc.durability)
			}
			info := spear.KineticWeaponInfo()
			if info.DelayTicks != tc.delay || info.DamageMultiplier != tc.multiplier || info.AttackDuration != tc.attackDuration {
				t.Fatalf("kinetic info = %+v; want delay %d, multiplier %v, attack duration %v", info, tc.delay, tc.multiplier, tc.attackDuration)
			}
			if info.ContactCooldownTicks != 10 || info.ForwardMovement != 0.38 || info.Dismount.MaxDurationTicks == 0 || info.Knockback.MaxDurationTicks == 0 || info.Damage.MaxDurationTicks == 0 {
				t.Fatalf("incomplete kinetic conditions: %+v", info)
			}
		})
	}
}

func TestLungeEnchantBehavior(t *testing.T) {
	for level, wantMin := range []int{5, 13, 21} {
		level++
		minCost, maxCost := enchantment.Lunge.Cost(level)
		if minCost != wantMin || maxCost != wantMin+20 {
			t.Fatalf("Lunge.Cost(%d) = %d..%d, want %d..%d", level, minCost, maxCost, wantMin, wantMin+20)
		}
		if got := enchantment.Lunge.Impulse(level); got != float64(level)*0.458 {
			t.Fatalf("Lunge.Impulse(%d) = %v", level, got)
		}
	}
	if !enchantment.Lunge.CompatibleWithItem(item.Spear{Tier: item.ToolTierWood}) {
		t.Fatal("Lunge must be compatible with spears")
	}
	if enchantment.Lunge.CompatibleWithItem(item.Sword{Tier: item.ToolTierIron}) {
		t.Fatal("Lunge must not be compatible with swords")
	}
}
