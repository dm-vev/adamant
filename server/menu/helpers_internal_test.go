package menu

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

// helperOwner is the owner of the batches of the tests of the helpers of
// the operations: a game mode and unlocked recipes are all they read.
type helperOwner struct {
	Owner
	mode     world.GameMode
	unlocked []string
}

func (o *helperOwner) GameMode() world.GameMode    { return o.mode }
func (o *helperOwner) UnlockedRecipeIDs() []string { return o.unlocked }

// The tests below were the session's, of its own copies of these helpers,
// before they moved here.

func TestBindingBlocksArmourRemovalOutsideCreative(t *testing.T) {
	t.Parallel()
	bound := item.NewStack(item.Boots{Tier: item.ArmourTierLeather{}}, 1).WithEnchantments(item.NewEnchantment(enchantment.CurseOfBinding, 1))
	inv := Inventories{Main: inventory.New(36, nil), Armour: inventory.NewArmour(nil)}
	from := Slot{Inv: inv.Armour.Inventory(), Index: 3}
	to := Slot{Inv: inv.Main, Index: 0}
	batch := func(mode world.GameMode) *Tx { return Begin(Opts{Owner: &helperOwner{mode: mode}, Inv: inv}) }
	if err := batch(world.GameModeSurvival).ensureBindingAllowsRemoval(bound, from, to); err == nil {
		t.Fatal("survival removal of bound armour was allowed")
	}
	if err := batch(world.GameModeCreative).ensureBindingAllowsRemoval(bound, from, to); err != nil {
		t.Fatalf("creative removal of bound armour rejected: %v", err)
	}
}

// TestLimitedCraftingLocksRecipes checks crafting as
// RecipeCraftingHolder.setRecipeUsed: with limited_crafting on, a crafting
// recipe needs to be unlocked, and with it off any recipe crafts.
func TestLimitedCraftingLocksRecipes(t *testing.T) {
	t.Parallel()
	windCharge := recipe.NewShapeless([]recipe.Item{item.NewStack(item.BreezeRod{}, 1)}, item.NewStack(item.WindCharge{}, 4), "crafting_table")
	ids := recipe.JavaRecipeIDs(windCharge)
	if len(ids) != 1 || ids[0] != "minecraft:wind_charge" {
		t.Fatalf("wind charge recipe Java IDs = %v, want [minecraft:wind_charge]", ids)
	}
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	player := &helperOwner{}
	<-w.Exec(func(tx *world.Tx) {
		b := Begin(Opts{Tx: tx, Owner: player})
		if b.recipeLocked(windCharge) {
			t.Fatal("recipe locked with limited_crafting off")
		}
		tx.World().SetGameRule("limited_crafting", true)
		if !b.recipeLocked(windCharge) {
			t.Fatal("locked recipe crafts with limited_crafting on")
		}
		player.unlocked = ids[:1]
		if b.recipeLocked(windCharge) {
			t.Fatal("unlocked recipe locked with limited_crafting on")
		}
	})
}
