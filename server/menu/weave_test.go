package menu_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// TestWeave checks the patterns a loom weaves and those it refuses, and that
// a refusal changes no slot.
func TestWeave(t *testing.T) {
	banner := block.Banner{Colour: item.ColourWhite()}
	dye := item.Dye{Colour: item.ColourRed()}
	creeper := item.BannerPattern{Type: item.CreeperBannerPattern()}
	tests := []struct {
		name                  string
		noMenu, noLoom        bool
		banner, dye, patternI item.Stack
		pattern               block.BannerPatternType
		times                 int
		// want is the count of the result, 0 if the loom refuses.
		want int
	}{
		{name: "border", banner: item.NewStack(banner, 3), dye: item.NewStack(dye, 3), pattern: block.BorderBannerPattern(), times: 1, want: 1},
		{name: "two borders", banner: item.NewStack(banner, 3), dye: item.NewStack(dye, 2), pattern: block.BorderBannerPattern(), times: 2, want: 2},
		{name: "creeper of its item", banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 1), patternI: item.NewStack(creeper, 1), pattern: block.CreeperBannerPattern(), times: 1, want: 1},
		{name: "creeper without its item", banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 1), pattern: block.CreeperBannerPattern(), times: 1},
		{name: "flower of the creeper item", banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 1), patternI: item.NewStack(creeper, 1), pattern: block.FlowerBannerPattern(), times: 1},
		{name: "no crafts", banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 1), pattern: block.BorderBannerPattern(), times: 0},
		{name: "too few banners", banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 2), pattern: block.BorderBannerPattern(), times: 2},
		{name: "too few dyes", banner: item.NewStack(banner, 2), dye: item.NewStack(dye, 1), pattern: block.BorderBannerPattern(), times: 2},
		{name: "illager banner", banner: item.NewStack(block.Banner{Illager: true}, 1), dye: item.NewStack(dye, 1), pattern: block.BorderBannerPattern(), times: 1},
		{name: "no banner", banner: item.NewStack(item.Apple{}, 1), dye: item.NewStack(dye, 1), pattern: block.BorderBannerPattern(), times: 1},
		{name: "no dye", banner: item.NewStack(banner, 1), dye: item.NewStack(item.Apple{}, 1), pattern: block.BorderBannerPattern(), times: 1},
		{name: "locked banner", banner: item.NewStack(banner, 1).WithLockMode(item.LockInInventory), dye: item.NewStack(dye, 1), pattern: block.BorderBannerPattern(), times: 1},
		{name: "locked dye", banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 1).WithLockMode(item.LockInSlot), pattern: block.BorderBannerPattern(), times: 1},
		{name: "locked pattern item", banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 1), patternI: item.NewStack(creeper, 1).WithLockMode(item.LockInInventory), pattern: block.BorderBannerPattern(), times: 1},
		{name: "no menu", noMenu: true, banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 1), pattern: block.BorderBannerPattern(), times: 1},
		{name: "no loom", noLoom: true, banner: item.NewStack(banner, 1), dye: item.NewStack(dye, 1), pattern: block.BorderBannerPattern(), times: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{0, 64, 0}
				tx.SetBlock(pos, block.Loom{}, nil)
				var m *menu.Menu
				if !test.noMenu {
					m = menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
				}
				if test.noLoom {
					tx.SetBlock(pos, block.Stone{}, nil)
				}
				b, inv := testBatch(tx, m, nil)
				_ = inv.UI.SetItem(menu.UILoomBanner, test.banner)
				_ = inv.UI.SetItem(menu.UILoomDye, test.dye)
				_ = inv.UI.SetItem(menu.UILoomPattern, test.patternI)

				err := b.Weave(test.pattern, test.times)
				if test.want == 0 {
					if err == nil {
						t.Fatal("the loom wove")
					}
					if n := len(b.Changes()); n != 0 {
						t.Errorf("a refusal made %d changes", n)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				changes := b.Changes()
				wantNames := []menu.Name{
					testName(menu.Slot{Index: menu.UILoomBanner}, menu.RoleLoomBanner),
					testName(menu.Slot{Index: menu.UILoomDye}, menu.RoleLoomDye),
					testName(menu.Slot{Index: menu.UIResult}, menu.RoleResult),
				}
				if len(changes) != len(wantNames) {
					t.Fatalf("%d changes, want %d", len(changes), len(wantNames))
				}
				for i, c := range changes {
					if c.Ref.Name != wantNames[i] {
						t.Errorf("change %d named %v, want %v", i, c.Ref.Name, wantNames[i])
					}
				}
				if got := changes[0].After.Count(); got != test.banner.Count()-test.times {
					t.Errorf("%d banners left, want %d", got, test.banner.Count()-test.times)
				}
				if got := changes[1].After.Count(); got != test.dye.Count()-test.times {
					t.Errorf("%d dyes left, want %d", got, test.dye.Count()-test.times)
				}
				res, _ := inv.UI.Item(menu.UIResult)
				woven, ok := res.Item().(block.Banner)
				if !ok || res.Count() != test.want {
					t.Fatalf("the result is %v, want %d banners", res, test.want)
				}
				if len(woven.Patterns) != 1 || woven.Patterns[0].Type != test.pattern || woven.Patterns[0].Colour != dye.Colour {
					t.Errorf("the result has patterns %v", woven.Patterns)
				}
				if kept, _ := inv.UI.Item(menu.UILoomPattern); !kept.Equal(test.patternI) {
					t.Errorf("the pattern item is %v, want %v kept", kept, test.patternI)
				}
			})
		})
	}
}
