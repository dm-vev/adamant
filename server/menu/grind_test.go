package menu_test

import (
	"math"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// orbs removes the experience orbs of tx and returns the experience they
// held and where they were spawned.
func orbs(tx *world.Tx) (experience int, at []mgl64.Vec3) {
	for e := range tx.Entities() {
		if orb, ok := e.(*entity.Ent); ok && e.H().Type() == entity.ExperienceOrbType {
			experience += orb.Behaviour().(*entity.ExperienceOrbBehaviour).Experience()
			at = append(at, e.Position())
			tx.RemoveEntity(e)
		}
	}
	return experience, at
}

// TestGrind grinds two damaged pickaxes, the first of efficiency V and the
// curse of vanishing: the result has the durability of both and 5% more,
// keeps the curse alone and was never worked, and the orbs at the eyes of
// the player hold between half the cost of efficiency V and all of it.
func TestGrind(t *testing.T) {
	pickaxe := item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1)
	cost, _ := enchantment.Efficiency.Cost(5)
	low, high := int(math.Ceil(float64(cost)/2)), cost
	w, _ := newWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for range 50 {
			o := &stationOwner{mode: world.GameModeSurvival, pos: mgl64.Vec3{0.5, 1, 0.5}}
			b, inv := stationBatch(t, tx, cube.Pos{}, block.Grindstone{}, o, map[int]item.Stack{
				menu.UIGrindstoneFirst: pickaxe.Damage(200).WithAnvilCost(3).WithEnchantments(
					item.NewEnchantment(enchantment.Efficiency, 5), item.NewEnchantment(enchantment.CurseOfVanishing, 1)),
				menu.UIGrindstoneSecond: pickaxe.Damage(150),
			})
			if err := b.Grind(); err != nil {
				t.Fatal(err)
			}
			res := uiSlot(inv, menu.UIResult)
			// 50 + 100 + 5% of 250.
			if res.Durability() != 162 || res.AnvilCost() != 0 {
				t.Errorf("durability %d and work %d, want 162 and 0", res.Durability(), res.AnvilCost())
			}
			if e := res.Enchantments(); len(e) != 1 || e[0].Type() != enchantment.CurseOfVanishing {
				t.Errorf("enchantments %v, want the curse of vanishing alone", e)
			}
			if !uiSlot(inv, menu.UIGrindstoneFirst).Empty() || !uiSlot(inv, menu.UIGrindstoneSecond).Empty() {
				t.Error("an input is left")
			}
			want := []menu.Name{
				roleName(menu.RoleGrindstoneFirst, menu.UIGrindstoneFirst),
				roleName(menu.RoleGrindstoneSecond, menu.UIGrindstoneSecond),
				roleName(menu.RoleResult, menu.UIResult),
			}
			if got := names(b); !slices.Equal(got, want) {
				t.Errorf("changes named %v, want %v", got, want)
			}
			xp, at := orbs(tx)
			if xp < low || xp > high {
				t.Errorf("%d experience dropped, want %d to %d", xp, low, high)
			}
			for _, pos := range at {
				if pos != o.pos.Add(mgl64.Vec3{0, 1.62}) {
					t.Errorf("an orb at %v, not at the eyes", pos)
				}
			}
		}
	})
}

// TestGrindOne grinds an input alone, in either slot: a curse and nothing
// else drops no experience.
func TestGrindOne(t *testing.T) {
	book := item.NewStack(item.EnchantedBook{}, 1).WithEnchantments(item.NewEnchantment(enchantment.CurseOfBinding, 1))
	for _, slot := range []int{menu.UIGrindstoneFirst, menu.UIGrindstoneSecond} {
		w, _ := newWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			b, inv := stationBatch(t, tx, cube.Pos{}, block.Grindstone{}, &stationOwner{}, map[int]item.Stack{slot: book})
			if err := b.Grind(); err != nil {
				t.Fatal(err)
			}
			if res := uiSlot(inv, menu.UIResult); len(res.Enchantments()) != 1 {
				t.Errorf("the result is %v, want the book of the curse", res)
			}
			if xp, _ := orbs(tx); xp != 0 {
				t.Errorf("%d experience dropped for a curse", xp)
			}
		})
	}
}

// TestGrindRefused checks the requests of a grindstone refused before
// anything changes or drops.
func TestGrindRefused(t *testing.T) {
	pickaxe := item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.Efficiency, 1))
	for _, tc := range []struct {
		name          string
		b             world.Block
		first, second item.Stack
	}{
		{"no grindstone", block.Stone{}, pickaxe, item.Stack{}},
		{"nothing", block.Grindstone{}, item.Stack{}, item.Stack{}},
		{"two sticks in a slot", block.Grindstone{}, item.NewStack(item.Stick{}, 2), item.Stack{}},
		{"a pickaxe and a sword", block.Grindstone{}, pickaxe, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)},
		{"two books", block.Grindstone{}, item.NewStack(item.Book{}, 1), item.NewStack(item.Book{}, 1)},
		{"a locked first input", block.Grindstone{}, pickaxe.WithLockMode(item.LockInSlot), item.Stack{}},
		{"a locked second input", block.Grindstone{}, pickaxe, pickaxe.WithLockMode(item.LockInInventory)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := newWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				b, _ := stationBatch(t, tx, cube.Pos{}, tc.b, &stationOwner{}, map[int]item.Stack{
					menu.UIGrindstoneFirst: tc.first, menu.UIGrindstoneSecond: tc.second,
				})
				if err := b.Grind(); err == nil {
					t.Fatal("the grind was not refused")
				}
				if xp, at := orbs(tx); len(b.Changes()) != 0 || len(at) != 0 {
					t.Errorf("the refusal changed %d slots and dropped %d experience", len(b.Changes()), xp)
				}
			})
		})
	}
	t.Run("no menu", func(t *testing.T) {
		w, _ := newWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			b := menu.Begin(menu.Opts{Tx: tx, Owner: &stationOwner{}, Name: testName})
			if err := b.Grind(); err == nil {
				t.Fatal("a grind with no menu open was not refused")
			}
		})
	})
}
