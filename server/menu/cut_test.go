package menu_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// TestCut checks the cuts of a stonecutter and those it refuses, and that a
// refusal changes no slot.
func TestCut(t *testing.T) {
	stone := item.NewStack(block.Stone{}, 1)
	bricks := recipe.NewShapeless([]recipe.Item{stone}, item.NewStack(block.StoneBricks{}, 1), "stonecutter")
	slabs := recipe.NewShapeless([]recipe.Item{stone}, item.NewStack(block.Slab{Block: block.Stone{}}, 2), "stonecutter")
	// An input of variants matches by name: a locked stone too, which the
	// lock refuses.
	anyStone := recipe.NewShapeless([]recipe.Item{stone.WithValue("variants", true)}, item.NewStack(block.StoneBricks{}, 1), "stonecutter")
	tests := []struct {
		name                  string
		noMenu, noStonecutter bool
		r                     recipe.Recipe
		input                 item.Stack
		times                 int
		want                  []item.Stack // nil if the stonecutter refuses
	}{
		{name: "bricks", r: bricks, input: item.NewStack(block.Stone{}, 4), times: 1, want: []item.Stack{item.NewStack(block.StoneBricks{}, 1)}},
		{name: "three bricks", r: bricks, input: item.NewStack(block.Stone{}, 4), times: 3, want: []item.Stack{item.NewStack(block.StoneBricks{}, 3)}},
		{name: "66 slabs in two stacks", r: slabs, input: item.NewStack(block.Stone{}, 40), times: 33, want: []item.Stack{item.NewStack(block.Slab{Block: block.Stone{}}, 64), item.NewStack(block.Slab{Block: block.Stone{}}, 2)}},
		{name: "a shaped recipe", r: recipe.NewShaped([]recipe.Item{stone}, item.NewStack(block.StoneBricks{}, 1), recipe.NewShape(1, 1), "stonecutter"), input: item.NewStack(block.Stone{}, 1), times: 1},
		{name: "a recipe of the crafting table", r: recipe.NewShapeless([]recipe.Item{stone}, item.NewStack(block.StoneBricks{}, 1), "crafting_table"), input: item.NewStack(block.Stone{}, 1), times: 1},
		{name: "no cuts", r: bricks, input: item.NewStack(block.Stone{}, 1), times: 0},
		{name: "too little stone", r: bricks, input: item.NewStack(block.Stone{}, 1), times: 2},
		{name: "the wrong input", r: bricks, input: item.NewStack(block.Granite{}, 1), times: 1},
		{name: "a locked input", r: anyStone, input: item.NewStack(block.Stone{}, 1).WithLockMode(item.LockInInventory), times: 1},
		{name: "an input of variants", r: anyStone, input: item.NewStack(block.Stone{}, 1), times: 1, want: []item.Stack{item.NewStack(block.StoneBricks{}, 1)}},
		{name: "no menu", noMenu: true, r: bricks, input: item.NewStack(block.Stone{}, 1), times: 1},
		{name: "no stonecutter", noStonecutter: true, r: bricks, input: item.NewStack(block.Stone{}, 1), times: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{0, 64, 0}
				tx.SetBlock(pos, block.Stonecutter{}, nil)
				var m *menu.Menu
				if !test.noMenu {
					m = menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
				}
				if test.noStonecutter {
					tx.SetBlock(pos, block.Stone{}, nil)
				}
				b, inv := testBatch(tx, m, nil)
				_ = inv.UI.SetItem(menu.UIStonecutterInput, test.input)

				err := b.Cut(test.r, test.times)
				if test.want == nil {
					if err == nil {
						t.Fatal("the stonecutter cut")
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
				if len(changes) == 0 || changes[0].Ref.Name != testName(menu.Slot{Index: menu.UIStonecutterInput}, menu.RoleStonecutterInput) {
					t.Fatalf("the input was not taken first: %v", changes)
				}
				if got := changes[0].After.Count(); got != test.input.Count()-test.times {
					t.Errorf("%d stone left, want %d", got, test.input.Count()-test.times)
				}
				if len(test.want) == 1 {
					if len(changes) != 2 {
						t.Fatalf("%d changes, want the input and the result", len(changes))
					}
					if res, _ := inv.UI.Item(menu.UIResult); !res.Equal(test.want[0]) {
						t.Errorf("the result is %v, want %v", res, test.want[0])
					}
					return
				}
				// The client takes each of several results.
				if len(changes) != 1 {
					t.Fatalf("%d changes before the results were taken", len(changes))
				}
				for i, want := range test.want {
					if err := b.TakeResult(i); err != nil {
						t.Fatal(err)
					}
					if res, _ := inv.UI.Item(menu.UIResult); !res.Equal(want) {
						t.Errorf("result %d is %v, want %v", i, res, want)
					}
				}
			})
		})
	}
}
