package loot

import (
	"encoding/json"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSurvivesExplosionAtRadiusOne(t *testing.T) {
	r := java26.NewXoroshiroRandom(1)
	ctx := Context{Random: &r, ExplosionRadius: 1}
	ok, err := testCondition(condition{Type: "minecraft:survives_explosion"}, &ctx)
	if err != nil || !ok {
		t.Fatalf("radius-one explosion survival = %t, %v; want true", ok, err)
	}
}

func TestEntityPredicateRequiresMatchingContextAndProperties(t *testing.T) {
	predicate := condition{Type: "minecraft:entity_properties", Entity: "this", Predicate: map[string]any{
		"minecraft:entity_type": "minecraft:creeper",
	}}
	if ok, err := testCondition(predicate, &Context{Entity: "minecraft:skeleton"}); err != nil || ok {
		t.Fatalf("mismatched entity type predicate = %t, %v; want false", ok, err)
	}
	if ok, err := testCondition(condition{Type: "minecraft:entity_properties", Entity: "this", Predicate: map[string]any{}}, &Context{}); err != nil || ok {
		t.Fatalf("missing entity predicate context = %t, %v; want false", ok, err)
	}
}

func TestMatchToolPredicateChecksMissingToolTagsAndEnchantments(t *testing.T) {
	if ok, err := matchToolPredicate(map[string]any{"items": "#minecraft:pickaxes"}, &Context{}); err != nil || ok {
		t.Fatalf("missing tool match = %t, %v; want false", ok, err)
	}
	pickaxe, ok := world.ItemByName("minecraft:diamond_pickaxe", 0)
	if !ok {
		t.Fatal("diamond pickaxe is unavailable")
	}
	stack := item.NewStack(pickaxe, 1).WithEnchantments(item.NewEnchantment(enchantment.Fortune, 2))
	ctx := Context{ToolStack: stack}
	predicate := map[string]any{
		"items": "#minecraft:pickaxes",
		"components": map[string]any{"predicates": map[string]any{"minecraft:enchantments": []any{map[string]any{"enchantments": "#minecraft:non_treasure", "levels": map[string]any{"min": float64(2)}}}}},
	}
	if ok, err := matchToolPredicate(predicate, &ctx); err != nil || !ok {
		t.Fatalf("tagged enchanted pickaxe match = %t, %v; want true", ok, err)
	}
	predicate["components"].(map[string]any)["predicates"].(map[string]any)["minecraft:enchantments"].([]any)[0].(map[string]any)["levels"] = map[string]any{"min": float64(3)}
	if ok, err := matchToolPredicate(predicate, &ctx); err != nil || ok {
		t.Fatalf("under-level pickaxe match = %t, %v; want false", ok, err)
	}
	smeltPredicate := map[string]any{"components": map[string]any{"predicates": map[string]any{"minecraft:enchantments": []any{map[string]any{"enchantments": "#minecraft:smelts_loot"}}}}}
	if ok, err := matchToolPredicate(smeltPredicate, &ctx); err != nil || ok {
		t.Fatalf("non-smelting enchantment tag match = %t, %v; want false", ok, err)
	}
	ctx.ToolStack = stack.WithForcedEnchantments(item.NewEnchantment(enchantment.FireAspect, 1))
	if ok, err := matchToolPredicate(smeltPredicate, &ctx); err != nil || !ok {
		t.Fatalf("smelts-loot enchantment tag match = %t, %v; want true", ok, err)
	}
}

func TestLootEntryDefaultWeightAndFloorLuck(t *testing.T) {
	var e entry
	if err := json.Unmarshal([]byte(`{"type":"minecraft:item","name":"minecraft:stone"}`), &e); err != nil {
		t.Fatal(err)
	}
	r := java26.NewXoroshiroRandom(3)
	ctx := Context{Random: &r}
	selected, err := choose([]entry{e}, &ctx)
	if err != nil || len(selected) != 1 || selected[0].Name != "minecraft:stone" {
		t.Fatalf("default-weight selection = %#v, %v; want stone", selected, err)
	}
	ctx.Luck = 1
	selected, err = choose([]entry{{Type: "minecraft:item", Name: "minecraft:stone", Weight: 1, Quality: -2}}, &ctx)
	if err != nil || len(selected) != 0 {
		t.Fatalf("floored non-positive weight selection = %#v, %v; want no selection", selected, err)
	}
}

func TestSetEnchantmentsModifierDecodesObjectAndTransformsBook(t *testing.T) {
	var e entry
	data := []byte(`{"type":"minecraft:item","name":"minecraft:book","modifier":{"type":"minecraft:set_enchantments","enchantments":{"minecraft:wind_burst":1}}}`)
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	stack, err := makeStack(e, &Context{})
	if err != nil {
		t.Fatal(err)
	}
	name, _ := stack.Item().EncodeItem()
	if name != "minecraft:enchanted_book" || lootEnchantmentLevel(stack, "minecraft:wind_burst") != 1 {
		t.Fatalf("set_enchantments result = %s with wind_burst %d; want enchanted book with level 1", name, lootEnchantmentLevel(stack, "minecraft:wind_burst"))
	}
}

func TestInlineLootTableValueExpandsItsPools(t *testing.T) {
	var e entry
	data := []byte(`{"type":"minecraft:loot_table","value":{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:stone"}]}]}}`)
	if err := json.Unmarshal(data, &e); err != nil {
		t.Fatal(err)
	}
	r := java26.NewXoroshiroRandom(7)
	expanded, err := expand(e, &Context{Random: &r})
	if err != nil || len(expanded) != 1 || expanded[0].Name != "minecraft:stone" {
		t.Fatalf("inline loot-table expansion = %#v, %v; want one stone entry", expanded, err)
	}
}

func TestSetEnchantmentsPreservesOthersAndAppliesAddMode(t *testing.T) {
	for _, test := range []struct {
		name string
		add bool
		level, want float64
	}{
		{name: "replace", level: 1, want: 1},
		{name: "add", add: true, level: 1, want: 3},
		{name: "remove", level: 0, want: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := entry{Type: "minecraft:item", Name: "minecraft:diamond_pickaxe", Modifier: modifierList{
				{Type: "minecraft:set_components", Components: map[string]any{
					"minecraft:enchantments": map[string]any{"minecraft:fortune": float64(2), "minecraft:mending": float64(1)},
				}},
				{Type: "minecraft:set_enchantments", Add: test.add, Enchantments: map[string]any{"minecraft:fortune": test.level}},
			}}
			stack, err := makeStack(e, &Context{})
			if err != nil { t.Fatal(err) }
			if got := lootEnchantmentLevel(stack, "minecraft:fortune"); got != int(test.want) {
				t.Fatalf("fortune level = %d, want %d", got, int(test.want))
			}
			if got := lootEnchantmentLevel(stack, "minecraft:mending"); got != 1 {
				t.Fatalf("unmentioned mending level = %d, want 1", got)
			}
		})
	}
}

func TestDiamondOreFortuneMatchesJavaBonusBoundaries(t *testing.T) {
	tool := item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).
		WithForcedEnchantments(item.NewEnchantment(enchantment.Fortune, 3))
	// Java 26.3 XoroshiroRandomSource and ApplyBonusCount.OreDrops: clamped
	// minimum, positive bonus, and upper endpoint. The one-choice pool must
	// not consume a random draw before applying Fortune.
	for _, test := range []struct {
		seed  int64
		count int
	}{{6, 1}, {1, 2}, {3, 4}} {
		random := java26.NewXoroshiroRandom(test.seed)
		drops, err := Generate("minecraft:blocks/diamond_ore", test.seed, Context{ToolStack: tool, Random: &random})
		if err != nil {
			t.Fatal(err)
		}
		if len(drops) != 1 {
			t.Fatalf("seed %d: drops = %v, want one diamond stack", test.seed, drops)
		}
		name, _ := drops[0].Item().EncodeItem()
		if name != "minecraft:diamond" || drops[0].Count() != test.count {
			t.Fatalf("seed %d: got %d %s, want %d diamonds", test.seed, drops[0].Count(), name, test.count)
		}
	}
}
