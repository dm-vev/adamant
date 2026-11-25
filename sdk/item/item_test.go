package item_test

import (
	"slices"
	"testing"
	"time"

	sdkitem "github.com/df-mc/dragonfly/sdk/item"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func TestCustomItemDefineCapabilitiesAndRegistry(t *testing.T) {
	var used bool
	wand := sdkitem.Define("sdk_test:blink_wand",
		sdkitem.MaxCount(1),
		sdkitem.AttackDamage(7.5),
		sdkitem.Durability(item.DurabilityInfo{MaxDurability: 128}),
		sdkitem.Cooldown(2*time.Second),
		sdkitem.OnUse(func(*world.Tx, item.User, *item.UseContext) bool {
			used = true
			return true
		}),
	)

	if name, _ := wand.EncodeItem(); name != "sdk_test:blink_wand" {
		t.Fatalf("EncodeItem = %q", name)
	}
	if _, ok := wand.(item.Weapon); !ok {
		t.Fatal("expected item.Weapon capability")
	}
	if d, ok := wand.(item.Durable); !ok || d.DurabilityInfo().MaxDurability != 128 {
		t.Fatal("expected item.Durable with MaxDurability=128")
	}
	if cd, ok := wand.(item.Cooldown); !ok || cd.Cooldown() != 2*time.Second {
		t.Fatal("expected item.Cooldown of 2s")
	}
	u, ok := wand.(item.Usable)
	if !ok {
		t.Fatal("expected item.Usable capability")
	}
	if !u.Use(nil, nil, &item.UseContext{}) || !used {
		t.Fatal("OnUse callback did not fire")
	}

	sdkitem.Register(wand)
	got, ok := sdkitem.ByName("sdk_test:blink_wand", 0)
	if !ok || got == nil {
		t.Fatal("ByName did not find registered custom item")
	}

	// Fluent StackBuilder test.
	st := sdkitem.Stack(item.Sword{Tier: item.ToolTierDiamond}).
		Name("Excalibur").
		Lore("Forged in Go").
		AddLore("Line 2").
		Enchant(enchantment.Sharpness, 5).
		Unbreakable().
		Value("tier", "legendary").
		Build()
	if st.CustomName() != "Excalibur" || !slices.Equal(st.Lore(), []string{"Forged in Go", "Line 2"}) {
		t.Fatalf("StackBuilder name/lore = (%q, %v)", st.CustomName(), st.Lore())
	}
	if _, ok := st.Enchantment(enchantment.Sharpness); !ok || !st.Unbreakable() {
		t.Fatal("StackBuilder enchant/unbreakable missing")
	}
	if v, ok := st.Value("tier"); !ok || v != "legendary" {
		t.Fatalf("StackBuilder Value = (%v, %v)", v, ok)
	}
}
