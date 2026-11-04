package menu_test

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// fakeOwner is the player of a batch in the tests: its game mode,
// experience, enchantment seed and unlocked recipes are fields, and it
// records what it drops and is told.
type fakeOwner struct {
	mode     world.GameMode
	pos      mgl64.Vec3
	level    int
	seed     int64
	unlocked []string
	// refuseDrops has Drop drop nothing, as a cancelled HandleItemDrop.
	refuseDrops bool

	dropped  []item.Stack
	messages []string
	closed   int
}

func (o *fakeOwner) Close() error             { return nil }
func (o *fakeOwner) H() *world.EntityHandle   { return nil }
func (o *fakeOwner) Position() mgl64.Vec3     { return o.pos }
func (o *fakeOwner) Rotation() cube.Rotation  { return cube.Rotation{} }
func (o *fakeOwner) Name() string             { return "owner" }
func (o *fakeOwner) GameMode() world.GameMode { return o.mode }
func (o *fakeOwner) ExperienceLevel() int     { return o.level }
func (o *fakeOwner) SetExperienceLevel(l int) { o.level = l }
func (o *fakeOwner) EnchantmentSeed() int64   { return o.seed }
func (o *fakeOwner) ResetEnchantmentSeed()    { o.seed++ }
func (o *fakeOwner) UnlockedRecipeIDs() []string {
	return o.unlocked
}
func (o *fakeOwner) Message(a ...any) {
	for _, v := range a {
		if s, ok := v.(string); ok {
			o.messages = append(o.messages, s)
		}
	}
}
func (o *fakeOwner) CloseMenu(bool) { o.closed++ }
func (o *fakeOwner) Drop(s item.Stack) int {
	if o.refuseDrops {
		return 0
	}
	o.dropped = append(o.dropped, s)
	return s.Count()
}

var _ menu.Owner = (*fakeOwner)(nil)

// ownerBatch begins a batch for o over fresh inventories of a player with m
// open.
func ownerBatch(tx *world.Tx, m *menu.Menu, o *fakeOwner) (*menu.Tx, menu.Inventories) {
	inv := menu.Inventories{
		Main:       inventory.New(36, nil),
		OffHand:    inventory.New(1, nil),
		EnderChest: inventory.New(27, nil),
		UI:         inventory.New(54, nil),
		Armour:     inventory.NewArmour(nil),
	}
	return menu.Begin(menu.Opts{Tx: tx, Owner: o, Inv: inv, Menu: m, Viewer: &fakeViewer{}, Name: testName}), inv
}

// slotOf returns what slot index of inv holds.
func slotOf(inv *inventory.Inventory, index int) item.Stack {
	it, _ := inv.Item(index)
	return it
}
