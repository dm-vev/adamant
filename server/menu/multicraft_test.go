package menu_test

import (
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

func multi(id string) recipe.Multi { return recipe.NewMulti(uuid.MustParse(id)) }

const (
	repairUUID   = "00000000-0000-0000-0000-000000000001"
	fireworkUUID = "00000000-0000-0000-0000-000000000002"
	mapLockUUID  = "602234e4-cac1-4353-8bb7-b1ebff70024b"
	mapCloneUUID = "85939755-ba10-4d9d-a4cc-efb7a8e943c4"
)

// TestMultiCraftRepair checks a special recipe from start to end: the
// snapshot of the grid of the inventory, named for its role, the bows
// consumed in any order and taken in the order of their slots, the result
// derived by the menu, and the check of the caller.
func TestMultiCraftRepair(t *testing.T) {
	bow := item.NewStack(item.Bow{}, 1)
	want := bow.WithDurability(min(bow.MaxDurability(), 10+20+bow.MaxDurability()/20))
	for _, c := range []struct {
		name  string
		check func(item.Stack) bool
		ok    bool
	}{
		{"no check", nil, true},
		{"checked", func(r item.Stack) bool { return r.Equal(want) }, true},
		{"refused by the check", func(item.Stack) bool { return false }, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			inTx(t, func(tx *world.Tx) {
				b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
				_ = inv.UI.SetItem(menu.UICraftingSmall, bow.WithDurability(10))
				_ = inv.UI.SetItem(menu.UICraftingSmall+1, bow.WithDurability(20))
				mc, err := b.BeginMultiCraft(multi(repairUUID), 1)
				if err != nil {
					t.Fatal(err)
				}
				inputs := mc.Inputs()
				if len(inputs) != 4 || inputs[0].Index != menu.UICraftingSmall || inputs[0].Name != testName(inputs[0].Slot, menu.RoleCraftingGrid) {
					t.Fatalf("inputs %v, want the four slots of the grid of the inventory", inputs)
				}
				for _, i := range []int{1, 0} {
					if err := mc.Consume(inputs[i], 1); err != nil {
						t.Fatal(err)
					}
				}
				err = mc.Finish(c.check)
				if (err == nil) != c.ok {
					t.Fatalf("Finish: %v, want accepted %v", err, c.ok)
				}
				if !c.ok {
					if len(b.Changes()) != 0 {
						t.Errorf("a refused craft changed %v", b.Changes())
					}
					return
				}
				changes := b.Changes()
				if len(changes) != 3 || changes[0].Ref.Index != menu.UICraftingSmall || changes[1].Ref.Index != menu.UICraftingSmall+1 {
					t.Errorf("the changes %v, want the grid in the order of its slots, then the result", changes)
				}
				if got := slotOf(inv.UI, menu.UIResult); !got.Equal(want) {
					t.Errorf("the result is %v, want %v", got, want)
				}
			})
		})
	}
}

// TestMultiCraftRefusals checks what a special recipe refuses: no times, a
// second one in a batch, a consume outside its inputs, of an empty slot,
// of none, of more than the slot holds, of a slot twice, of a locked item,
// of a slot changed since the snapshot; and a finish of no consumption, of
// part of the inputs, or of the wrong count.
func TestMultiCraftRefusals(t *testing.T) {
	paper, gunpowder := item.NewStack(item.Paper{}, 2), item.NewStack(item.Gunpowder{}, 2)
	begin := func(tx *world.Tx, times int, grid ...item.Stack) (*menu.Tx, *menu.MultiCraft, []menu.Ref, menu.Inventories) {
		b, inv := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
		for i, it := range grid {
			_ = inv.UI.SetItem(menu.UICraftingSmall+i, it)
		}
		mc, err := b.BeginMultiCraft(multi(fireworkUUID), times)
		if err != nil {
			t.Fatal(err)
		}
		return b, mc, mc.Inputs(), inv
	}
	inTx(t, func(tx *world.Tx) {
		b, _ := ownerBatch(tx, nil, &fakeOwner{mode: world.GameModeSurvival})
		if _, err := b.BeginMultiCraft(multi(fireworkUUID), 0); err == nil {
			t.Error("a special recipe began for no times")
		}
		if _, err := b.BeginMultiCraft(multi(fireworkUUID), 1); err != nil {
			t.Fatal(err)
		}
		if _, err := b.BeginMultiCraft(multi(fireworkUUID), 1); err == nil {
			t.Error("a second special recipe began in a batch")
		}

		_, mc, inputs, inv := begin(tx, 1, paper, gunpowder)
		for _, c := range []struct {
			name  string
			r     menu.Ref
			count int
		}{
			{"a slot of the table", ref(inv.UI, menu.UICraftingLarge, 0), 1},
			{"the inventory", ref(inv.Main, menu.UICraftingSmall, 0), 1},
			{"an empty slot", inputs[2], 1},
			{"none", inputs[0], 0},
			{"more than there is", inputs[0], 3},
		} {
			if err := mc.Consume(c.r, c.count); err == nil {
				t.Errorf("consumed %s", c.name)
			}
		}
		if err := mc.Consume(inputs[0], 1); err != nil {
			t.Fatal(err)
		}
		if err := mc.Consume(inputs[0], 1); err == nil {
			t.Error("consumed a slot twice")
		}
		if err := mc.Finish(nil); err == nil {
			t.Error("finished with the gunpowder left")
		}

		_, mc, inputs, _ = begin(tx, 2, paper, gunpowder)
		_ = mc.Consume(inputs[0], 1)
		_ = mc.Consume(inputs[1], 2)
		if err := mc.Finish(nil); err == nil {
			t.Error("finished two crafts of one paper")
		}

		_, mc, _, _ = begin(tx, 1, paper, gunpowder)
		if err := mc.Finish(nil); err == nil {
			t.Error("finished with nothing consumed")
		}

		_, mc, inputs, inv = begin(tx, 1, paper.WithLockMode(item.LockInInventory), gunpowder)
		if err := mc.Consume(inputs[0], 1); err == nil {
			t.Error("consumed locked paper")
		}
		_ = inv.UI.SetItem(menu.UICraftingSmall+1, gunpowder.Grow(1))
		if err := mc.Consume(inputs[1], 1); err == nil {
			t.Error("consumed gunpowder changed since the snapshot")
		}

		b, mc, inputs, inv = begin(tx, 1, item.NewStack(item.Paper{}, 1), item.NewStack(item.Gunpowder{}, 1), item.NewStack(item.Gunpowder{}, 1))
		for _, r := range inputs[:3] {
			if err := mc.Consume(r, 1); err != nil {
				t.Fatal(err)
			}
		}
		if err := mc.Finish(nil); err != nil {
			t.Fatal(err)
		}
		want := item.NewStack(item.Firework{Duration: 1500 * time.Millisecond, Explosions: []item.FireworkExplosion{}}, 3)
		if got := slotOf(inv.UI, menu.UIResult); !got.Equal(want) {
			t.Errorf("the fireworks are %v, want %v", got, want)
		}
		if _, err := b.BeginMultiCraft(multi(fireworkUUID), 1); err != nil {
			t.Errorf("no special recipe began after one finished: %v", err)
		}
	})
}

// mapProvider is a provider that keeps maps, numbered from 42.
type mapProvider struct {
	world.NopProvider
	mu     sync.Mutex
	maps   map[int64]world.MapData
	nextID int64
}

func (p *mapProvider) LoadMap(id int64) (world.MapData, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	data, ok := p.maps[id]
	if !ok {
		return world.MapData{}, world.ErrInvalidMapData
	}
	return data, nil
}

func (p *mapProvider) StoreMap(data world.MapData) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.maps[data.ID] = data
	return nil
}

func (p *mapProvider) AllocateMapID() (int64, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextID++
	return p.nextID, nil
}

func (p *mapProvider) JavaMapID(id int64) (int32, error)   { return int32(id), nil }
func (p *mapProvider) NativeMapID(id int32) (int64, error) { return int64(id), nil }

// TestMultiCraftMapsAtTheCartographyTable checks the map recipes of a
// cartography table: its two inputs, named for their roles, a map locked
// into a new map of the world, a recipe of the crafting table refused, and
// a map cloned at the crafting table only.
func TestMultiCraftMapsAtTheCartographyTable(t *testing.T) {
	p := &mapProvider{maps: map[int64]world.MapData{42: {ID: 42, Scale: 1, Colors: make([]byte, 128*128)}}, nextID: 42}
	w := world.Config{Provider: p, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos, block.CartographyTable{}, nil)
		m := menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
		b, inv := ownerBatch(tx, m, &fakeOwner{mode: world.GameModeSurvival})
		source := item.NewStack(item.FilledMap{NativeID: 42, HasNativeID: true}, 1)
		_ = inv.UI.SetItem(menu.UICartographyInput, source)
		_ = inv.UI.SetItem(menu.UICartographyAdditional, item.NewStack(block.GlassPane{}, 1))
		if _, err := b.BeginMultiCraft(multi(mapCloneUUID), 1); err == nil {
			t.Fatal("the map cloning of the crafting table began at a cartography table")
		}
		mc, err := b.BeginMultiCraft(multi(mapLockUUID), 1)
		if err != nil {
			t.Fatal(err)
		}
		inputs := mc.Inputs()
		if len(inputs) != 2 || inputs[0].Name != testName(inputs[0].Slot, menu.RoleCartographyInput) ||
			inputs[1].Index != menu.UICartographyAdditional || inputs[1].Name != testName(inputs[1].Slot, menu.RoleCartographyAdditional) {
			t.Fatalf("inputs %v, want the input and the additional slot of the cartography table", inputs)
		}
		for _, r := range inputs {
			if err := mc.Consume(r, 1); err != nil {
				t.Fatal(err)
			}
		}
		if err := mc.Finish(func(r item.Stack) bool { return r.Equal(source) }); err != nil {
			t.Fatal(err)
		}
		result, ok := slotOf(inv.UI, menu.UIResult).Item().(item.FilledMap)
		if !ok || result.NativeID != 43 {
			t.Fatalf("the result is %v, want map 43", slotOf(inv.UI, menu.UIResult))
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if locked := p.maps[43]; !locked.Locked || p.maps[42].Locked {
			t.Errorf("map 43 locked %v, map 42 locked %v: want the new map locked", locked.Locked, p.maps[42].Locked)
		}
	})
}
