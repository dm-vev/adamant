package menu_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// stationOwner is the player of the batches of the tests of the enchanting
// table, the anvil, the grindstone and the smithing table: a game mode,
// levels and an enchantment seed, and a position with the eyes of a player.
type stationOwner struct {
	mode   world.GameMode
	level  int
	seed   int64
	resets int
	pos    mgl64.Vec3
}

func (o *stationOwner) Close() error                   { return nil }
func (o *stationOwner) H() *world.EntityHandle         { return nil }
func (o *stationOwner) Position() mgl64.Vec3           { return o.pos }
func (o *stationOwner) Rotation() cube.Rotation        { return cube.Rotation{} }
func (o *stationOwner) EyeHeight() float64             { return 1.62 }
func (o *stationOwner) Name() string                   { return "station" }
func (o *stationOwner) GameMode() world.GameMode       { return o.mode }
func (o *stationOwner) ExperienceLevel() int           { return o.level }
func (o *stationOwner) SetExperienceLevel(level int)   { o.level = level }
func (o *stationOwner) EnchantmentSeed() int64         { return o.seed }
func (o *stationOwner) ResetEnchantmentSeed()          { o.resets++; o.seed++ }
func (o *stationOwner) Drop(item.Stack) int            { return 0 }
func (o *stationOwner) UnlockedRecipeIDs() []string    { return nil }
func (o *stationOwner) Message(...any)                 {}
func (o *stationOwner) CloseMenu(clientRequested bool) {}

// stationBatch places b at pos, opens its menu and begins a batch of o
// over fresh inventories, whose UI slots are set to the stacks of ui.
func stationBatch(t *testing.T, tx *world.Tx, pos cube.Pos, b world.Block, o *stationOwner, ui map[int]item.Stack) (*menu.Tx, menu.Inventories) {
	t.Helper()
	tx.SetBlock(pos, b, nil)
	m := menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
	inv := menu.Inventories{
		Main:       inventory.New(36, nil),
		OffHand:    inventory.New(1, nil),
		EnderChest: inventory.New(27, nil),
		UI:         inventory.New(54, nil),
		Armour:     inventory.NewArmour(nil),
	}
	for slot, s := range ui {
		if err := inv.UI.SetItem(slot, s); err != nil {
			t.Fatal(err)
		}
	}
	return menu.Begin(menu.Opts{Tx: tx, Owner: o, Inv: inv, Menu: m, Name: testName}), inv
}

// uiSlot returns what a UI slot of inv holds.
func uiSlot(inv menu.Inventories, slot int) item.Stack {
	s, _ := inv.UI.Item(slot)
	return s
}

// names returns the names of the changes of a batch, in order.
func names(b *menu.Tx) []menu.Name {
	var n []menu.Name
	for _, c := range b.Changes() {
		n = append(n, c.Ref.Name)
	}
	return n
}

// roleName is the name testName gives a UI slot for a role.
func roleName(r menu.Role, slot int) menu.Name {
	return testName(menu.Slot{Index: slot}, r)
}
