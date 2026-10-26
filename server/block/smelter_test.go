package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

// javaStack returns a stack of the Java item passed.
func javaStack(t *testing.T, id string, count int) item.Stack {
	t.Helper()
	it, ok := item.JavaItem(id)
	if !ok {
		t.Fatalf("no item for %v", id)
	}
	return item.NewStack(it, count)
}

func javaItemName(s item.Stack) string {
	if s.Empty() {
		return ""
	}
	name, _ := s.Item().EncodeItem()
	return name
}

// TestCookingRecipesFollowJava checks recipes from the Java 26.3 recipe data
// that the hand-written Bedrock recipes lacked or got wrong.
func TestCookingRecipesFollowJava(t *testing.T) {
	for _, c := range []struct {
		kind       item.CookingKind
		input      string
		product    string
		experience float32
		ok         bool
	}{
		{item.Smelting, "minecraft:oak_leaves", "minecraft:leaf_litter", 0.1, true},
		{item.Smelting, "minecraft:cactus", "minecraft:green_dye", 1, true},
		{item.Smelting, "minecraft:netherrack", "minecraft:netherbrick", 0.1, true},
		{item.Blasting, "minecraft:iron_spear", "minecraft:iron_nugget", 0.1, true},
		{item.Blasting, "minecraft:raw_iron", "minecraft:iron_ingot", 0.7, true},
		{item.Smoking, "minecraft:beef", "minecraft:cooked_beef", 0.35, true},
		{item.CampfireCooking, "minecraft:potato", "minecraft:baked_potato", 0.35, true},
		// Nether stems do not burn into charcoal, nor is ore smoked.
		{item.Smelting, "minecraft:crimson_stem", "", 0, false},
		{item.Smoking, "minecraft:raw_iron", "", 0, false},
	} {
		r, ok := item.CookingRecipeFor(c.kind, javaStack(t, c.input, 1).Item())
		if ok != c.ok || javaItemName(r.Product) != c.product || r.Experience != c.experience {
			t.Errorf("%v recipe for %v = %v %v %v, want %v %v %v", c.kind, c.input, javaItemName(r.Product), r.Experience, ok, c.product, c.experience, c.ok)
		}
	}
}

// TestCookingFuelFollowsJava checks burn times from Java's cooking_fuel
// components, halved in smokers and blast furnaces.
func TestCookingFuelFollowsJava(t *testing.T) {
	for _, c := range []struct {
		id        string
		ticks     int
		fastTicks int
	}{
		{"minecraft:coal", 1600, 800},
		{"minecraft:oak_boat", 1200, 600},
		{"minecraft:oak_slab", 150, 75},
		{"minecraft:oak_hanging_sign", 800, 400},
		{"minecraft:white_carpet", 67, 33},
		{"minecraft:dried_kelp_block", 4001, 2000},
		{"minecraft:oak_sapling", 100, 50},
		{"minecraft:lava_bucket", 20000, 10000},
		{"minecraft:copper_chest", 0, 0},
	} {
		it := javaStack(t, c.id, 1).Item()
		if got, fast := item.CookingBurnTime(it, false), item.CookingBurnTime(it, true); got != c.ticks || fast != c.fastTicks {
			t.Errorf("burn time of %v = %v/%v, want %v/%v", c.id, got, fast, c.ticks, c.fastTicks)
		}
	}
	if r, ok := item.CookingFuelRemainder(javaStack(t, "minecraft:lava_bucket", 1).Item()); !ok || javaItemName(r) != "minecraft:bucket" {
		t.Errorf("lava bucket leaves %v, want a bucket", javaItemName(r))
	}
}

// TestSmelterCooksLikeJava follows AbstractFurnaceBlockEntity.serverTick: a
// furnace cooks raw iron in 200 ticks on coal that burns 1600, a blast
// furnace in 100 on coal that burns 800. The fuel lit in the first tick
// burns down from the second.
func TestSmelterCooksLikeJava(t *testing.T) {
	for _, c := range []struct {
		kind       item.CookingKind
		fast       bool
		cook, burn int
	}{
		{item.Smelting, false, 200, 1600},
		{item.Blasting, true, 100, 800},
	} {
		s := newSmelter()
		_ = s.inventory.SetItem(0, javaStack(t, "minecraft:raw_iron", 2))
		_ = s.inventory.SetItem(1, javaStack(t, "minecraft:coal", 1))
		for range c.cook - 1 {
			s.tickSmelting(c.kind, c.fast)
		}
		if out, _ := s.inventory.Item(2); !out.Empty() {
			t.Fatalf("fast=%v: cooked after %v ticks", c.fast, c.cook-1)
		}
		lit, _ := s.tickSmelting(c.kind, c.fast)
		out, _ := s.inventory.Item(2)
		fuel, _ := s.inventory.Item(1)
		if !lit || javaItemName(out) != "minecraft:iron_ingot" || out.Count() != 1 || !fuel.Empty() || s.litDuration != c.burn || s.litTime != c.burn-c.cook+1 {
			t.Fatalf("fast=%v after %v ticks: lit %v, output %v x%v, fuel %v, burn %v/%v", c.fast, c.cook, lit, javaItemName(out), out.Count(), fuel.Count(), s.litTime, s.litDuration)
		}
	}
}

// TestSmelterProgressDecaysWithoutFuel loses two ticks of cooking progress
// a tick once the fuel runs out, rather than all of it.
func TestSmelterProgressDecaysWithoutFuel(t *testing.T) {
	s := newSmelter()
	_ = s.inventory.SetItem(0, javaStack(t, "minecraft:raw_iron", 1))
	// A stick burns for 100 ticks, half the time raw iron takes.
	_ = s.inventory.SetItem(1, javaStack(t, "minecraft:stick", 1))
	for range 100 {
		s.tickSmelting(item.Smelting, false)
	}
	if s.cookingTimer != 100 {
		t.Fatalf("cooking progress on a stick = %v, want 100", s.cookingTimer)
	}
	for range 5 {
		s.tickSmelting(item.Smelting, false)
	}
	if s.cookingTimer != 90 {
		t.Fatalf("cooking progress 5 ticks after the fuel ran out = %v, want 90", s.cookingTimer)
	}
	_ = s.inventory.SetItem(0, javaStack(t, "minecraft:raw_gold", 1))
	if s.cookingTimer != 0 {
		t.Fatalf("cooking progress after swapping the input = %v, want 0", s.cookingTimer)
	}
}

// TestSmelterExperienceFromRecipes awards experience for the recipes cooked
// when a player takes the result, as getRecipesToAwardAndPopExperience: ten
// raw iron at 0.7 give exactly 7, and the recipes survive saving.
func TestSmelterExperienceFromRecipes(t *testing.T) {
	f := NewFurnace(0)
	_ = f.inventory.SetItem(0, javaStack(t, "minecraft:raw_iron", 10))
	_ = f.inventory.SetItem(1, javaStack(t, "minecraft:coal", 2))
	for range 2000 {
		f.tickSmelting(item.Smelting, false)
	}
	if out, _ := f.inventory.Item(2); out.Count() != 10 {
		t.Fatalf("cooked %v iron ingots, want 10", out.Count())
	}
	loaded := f.DecodeNBT(f.EncodeNBT()).(Furnace)
	if got := loaded.Experience(); got != 7 {
		t.Fatalf("experience after loading = %v, want 7", got)
	}
	if got := loaded.ResetExperience(); got != 7 {
		t.Fatalf("experience awarded = %v, want 7", got)
	}
	if got := loaded.ResetExperience(); got != 0 {
		t.Fatalf("experience awarded twice = %v", got)
	}
}

// TestSmelterDriesWetSpongeIntoBucket fills an empty bucket in the fuel slot
// with the water of a dried sponge, as AbstractFurnaceBlockEntity.burn.
func TestSmelterDriesWetSpongeIntoBucket(t *testing.T) {
	s := newSmelter()
	_ = s.inventory.SetItem(0, item.NewStack(Sponge{Wet: true}, 1))
	_ = s.inventory.SetItem(1, javaStack(t, "minecraft:coal", 1))
	s.tickSmelting(item.Smelting, false)
	_ = s.inventory.SetItem(1, item.NewStack(item.Bucket{}, 1))
	for range 199 {
		s.tickSmelting(item.Smelting, false)
	}
	fuel, _ := s.inventory.Item(1)
	out, _ := s.inventory.Item(2)
	if b, ok := fuel.Item().(item.Bucket); !ok || b.Empty() || javaItemName(out) != "minecraft:sponge" {
		t.Fatalf("fuel slot %v, output %v; want a water bucket and a sponge", javaItemName(fuel), javaItemName(out))
	}
}
