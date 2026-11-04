package session

import (
	"fmt"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// craftOwner is the player of the batches of the differential tests of
// crafts: a player in survival that unlocked no recipe.
type craftOwner struct{}

func (craftOwner) Close() error                { return nil }
func (craftOwner) H() *world.EntityHandle      { return nil }
func (craftOwner) Position() mgl64.Vec3        { return mgl64.Vec3{} }
func (craftOwner) Rotation() cube.Rotation     { return cube.Rotation{} }
func (craftOwner) Name() string                { return "owner" }
func (craftOwner) GameMode() world.GameMode    { return world.GameModeSurvival }
func (craftOwner) ExperienceLevel() int        { return 0 }
func (craftOwner) SetExperienceLevel(int)      {}
func (craftOwner) EnchantmentSeed() int64      { return 0 }
func (craftOwner) ResetEnchantmentSeed()       {}
func (craftOwner) Drop(s item.Stack) int       { return s.Count() }
func (craftOwner) UnlockedRecipeIDs() []string { return nil }
func (craftOwner) Message(...any)              {}
func (craftOwner) CloseMenu(bool)              {}

// craftOutcome is what a craft left: whether it was accepted, the UI slots
// of the grids and the result, the results the client would take one by
// one, and the slots the response names, with the count each holds.
func craftOutcome(err error, ui *inventory.Inventory, results []item.Stack, named map[string]int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "accepted %v\n", err == nil)
	for slot := craftingGridSmallOffset; slot <= craftingResult; slot++ {
		if it, _ := ui.Item(slot); !it.Empty() {
			fmt.Fprintf(&b, "ui %d: %d %v\n", slot, it.Count(), item.WriteNBT(it))
		}
	}
	for i, r := range results {
		fmt.Fprintf(&b, "result %d: %d %v\n", i, r.Count(), item.WriteNBT(r))
	}
	fmt.Fprintf(&b, "named %v\n", named)
	return b.String()
}

// TestCraftsMatchTheSession crafts recipes of the crafting table and
// dynamic recipes the session's way, handleCraft and tryDynamicCraft, and
// as menu.Tx.Craft and CraftDynamic, from the same grids, and compares what
// each left: a shaped recipe of a tag, a shapeless one, which makes two
// results past 16 crafts, a user-data recipe, one of the stonecutter, and
// wolf armour dyed, each in the grid of the inventory and of a crafting table, crafted
// 0 to 3 and 17 times, of grids that hold what the recipe takes, too little,
// more, a locked item, or the inputs where the other grid has them.
func TestCraftsMatchTheSession(t *testing.T) {
	t.Parallel()
	planks := func(n int) item.Stack { return item.NewStack(block.Planks{Wood: block.OakWood()}, n) }
	box := func(t block.ShulkerBoxType) block.ShulkerBox {
		b := block.NewShulkerBox()
		b.Type = t
		return b
	}
	red := box(block.RedShulkerBox())
	_ = red.Inventory(nil, cube.Pos{}).SetItem(0, item.NewStack(item.Apple{}, 3))
	blueDye := item.NewStack(item.Dye{Colour: item.ColourBlue()}, 1)
	recipes := []struct {
		name  string
		r     recipe.Recipe
		grids [][]item.Stack
	}{
		{"sticks", recipe.NewShaped([]recipe.Item{recipe.NewItemTag("minecraft:planks", 1), recipe.NewItemTag("minecraft:planks", 1)}, item.NewStack(item.Stick{}, 4), recipe.Shape{1, 2}, "crafting_table"),
			[][]item.Stack{{planks(1), {}, planks(1)}, {planks(3), planks(17), {}, {}, planks(2)}, {planks(1)}, {planks(2), item.NewStack(item.Apple{}, 1), planks(2)}, {planks(2).WithLockMode(item.LockInInventory), planks(2)}, {{}, {}, {}, {}, {}, {}, planks(1), planks(1)}}},
		{"wind charges", recipe.NewShapeless([]recipe.Item{item.NewStack(item.BreezeRod{}, 1)}, item.NewStack(item.WindCharge{}, 4), "crafting_table"),
			[][]item.Stack{{item.NewStack(item.BreezeRod{}, 17)}, {{}, {}, {}, item.NewStack(item.BreezeRod{}, 3)}, {item.NewStack(item.BreezeRod{}, 1), item.NewStack(item.BreezeRod{}, 1)}}},
		{"a shulker box dyed", recipe.NewUserDataShapeless([]recipe.Item{item.NewStack(box(block.RedShulkerBox()), 1), blueDye}, item.NewStack(box(block.BlueShulkerBox()), 1), "crafting_table"),
			[][]item.Stack{{item.NewStack(red, 1).WithCustomName("Supplies"), blueDye}, {blueDye, item.NewStack(red, 2), {}, item.NewStack(item.Dye{Colour: item.ColourBlue()}, 2)}, {item.NewStack(red, 1)}}},
		{"stone bricks of the stonecutter", recipe.NewShapeless([]recipe.Item{item.NewStack(block.Stone{}, 1)}, item.NewStack(block.StoneBricks{}, 1), "stonecutter"),
			[][]item.Stack{{item.NewStack(block.Stone{}, 1)}}},
		{"dynamic: wolf armour dyed", nil,
			[][]item.Stack{{item.NewStack(item.WolfArmor{}, 1), item.NewStack(item.Dye{Colour: item.ColourLime()}, 3)}, {item.NewStack(item.Dye{Colour: item.ColourLime()}, 3)}, {item.NewStack(item.WolfArmor{}, 1).WithLockMode(item.LockInInventory), item.NewStack(item.Dye{Colour: item.ColourLime()}, 1)}}},
	}

	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	cases, accepted := 0, 0
	for _, rc := range recipes {
		for g, grid := range rc.grids {
			for _, table := range []bool{false, true} {
				for _, times := range []int{0, 1, 2, 3, 17} {
					name := fmt.Sprintf("%s, grid %d, table %v, %d times", rc.name, g, table, times)
					old := craftTheSessionsWay(w, rc.r, grid, table, times)
					var moved string
					<-w.Exec(func(tx *world.Tx) { moved = craftTheMenusWay(tx, rc.r, grid, table, times) })
					if old != moved {
						t.Errorf("%s:\nthe session: %s\nthe menu: %s", name, old, moved)
					}
					cases++
					if strings.HasPrefix(old, "accepted true") {
						accepted++
					}
				}
			}
		}
	}
	if cases < 100 || accepted < 20 {
		t.Fatalf("%d cases, %d accepted", cases, accepted)
	}
}

// placeGrid puts grid in the grid of ui that a crafting table or the
// inventory has.
func placeGrid(ui *inventory.Inventory, grid []item.Stack, table bool) {
	offset := craftingGridSmallOffset
	if table {
		offset = craftingGridLargeOffset
	}
	for i, it := range grid {
		_ = ui.SetItem(offset+i, it)
	}
}

// craftTheSessionsWay crafts r, or a dynamic recipe if r is nil, as the
// session's handlers do.
func craftTheSessionsWay(w *world.World, r recipe.Recipe, grid []item.Stack, table bool, times int) string {
	s := craftingTestSession()
	if table {
		<-w.Exec(func(tx *world.Tx) {
			tx.SetBlock(cube.Pos{}, block.CraftingTable{}, nil)
			s.menu.Store(menu.OpenBlock(tx, cube.Pos{}, s, nil))
		})
		s.openedContainerID.Store(uint32(v2193.ContainerTypeWorkbench))
	}
	placeGrid(s.ui, grid, table)
	h := craftingTestHandler()
	h.currentRequest = -1
	var err error
	if r == nil {
		err = h.tryDynamicCraft(s, nil, times)
	} else {
		s.recipes[1] = r
		err = h.handleCraft(&v2193.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: byte(times)}, s, nil)
	}
	results := h.pendingResults
	if len(results) == 1 {
		results = nil
	}
	named := map[string]int{}
	for c, slots := range h.changes {
		for slot, info := range slots {
			named[fmt.Sprintf("%d:%d", c, slot)] = int(info.after.Count)
		}
	}
	return craftOutcome(err, s.ui, results, named)
}

// craftTheMenusWay crafts r, or a dynamic recipe if r is nil, as a batch of
// the menu does, in tx.
func craftTheMenusWay(tx *world.Tx, r recipe.Recipe, grid []item.Stack, table bool, times int) string {
	s := craftingTestSession()
	var m *menu.Menu
	if table {
		tx.SetBlock(cube.Pos{}, block.CraftingTable{}, nil)
		m = menu.OpenBlock(tx, cube.Pos{}, s, nil)
	}
	placeGrid(s.ui, grid, table)
	b := menu.Begin(menu.Opts{Tx: tx, Owner: craftOwner{}, Inv: menu.Inventories{Main: s.inv, OffHand: s.offHand, UI: s.ui, Armour: inventory.NewArmour(nil)}, Menu: m, Name: s.nameSlot})
	var err error
	if r == nil {
		err = b.CraftDynamic(times)
	} else {
		err = b.Craft(r, times)
	}
	named := map[string]int{}
	for _, c := range b.Changes() {
		container, slot := nameParts(c.Ref.Name)
		named[fmt.Sprintf("%d:%d", container, slot)] = c.After.Count()
	}
	var results []item.Stack
	if err == nil && len(b.Changes()) > 0 && slotIsEmpty(s.ui, craftingResult) {
		// Several results wait in the batch for the client to take them.
		for i := 0; b.TakeResult(i) == nil; i++ {
			it, _ := s.ui.Item(craftingResult)
			results = append(results, it)
		}
		_ = s.ui.SetItem(craftingResult, item.Stack{})
	}
	b.Commit()
	return craftOutcome(err, s.ui, results, named)
}

func slotIsEmpty(inv *inventory.Inventory, slot int) bool {
	it, _ := inv.Item(slot)
	return it.Empty()
}
