package session

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// seedController is a player with a fixed enchantment seed.
type seedController struct {
	Controllable
	seed int64
}

func (c seedController) EnchantmentSeed() int64 { return c.seed }

// TestEnchantmentOptionsMatchTheSession sweeps menu.EnchantmentOptions
// against the copy the session used before it: for 200 seeds and a few far
// ones, an item of every enchantable type and every tier, a book, an item
// enchanted already and one that cannot be, around an enchanting table with
// no bookshelves, five and fifteen. The options must be the same, and so
// must the verdict on the input. (item.Enchantments sorts the enchantments
// on every call, which makes each draw cost: a sweep of 1,000 seeds passed
// once, in two minutes.)
func TestEnchantmentOptionsMatchTheSession(t *testing.T) {
	t.Parallel()
	// One item of each enchantable type: the variants of a type, its
	// tiers and colours, differ in their enchantment value at most, which
	// the tiers of swords and armour cover.
	var inputs []item.Stack
	seen := map[reflect.Type]bool{}
	for _, it := range world.Items() {
		if _, ok := it.(item.Enchantable); ok && !seen[reflect.TypeOf(it)] {
			seen[reflect.TypeOf(it)] = true
			inputs = append(inputs, item.NewStack(it, 1))
		}
	}
	if len(inputs) < 15 {
		t.Fatalf("only %d enchantable items are registered", len(inputs))
	}
	for _, tier := range item.ToolTiers() {
		inputs = append(inputs, item.NewStack(item.Sword{Tier: tier}, 1))
	}
	for _, tier := range item.ArmourTiers() {
		inputs = append(inputs, item.NewStack(item.Helmet{Tier: tier}, 1))
	}
	inputs = append(inputs,
		item.NewStack(item.Book{}, 1),
		item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 1)),
		item.NewStack(item.Stick{}, 1),
	)
	seeds := []int64{-1 << 63, -1, 1<<63 - 1, 0x5eed}
	for seed := range int64(200) {
		seeds = append(seeds, seed)
	}

	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	s := &Session{}
	for _, shelves := range []int{0, 5, 15} {
		<-w.Exec(func(tx *world.Tx) {
			pos := cube.Pos{0, 64, 0}
			placeBookshelves(tx, pos, shelves)
			tx.SetBlock(pos, block.EnchantingTable{}, nil)
			for _, input := range inputs {
				for _, seed := range seeds {
					costs, enchants := s.determineAvailableEnchantments(tx, seedController{seed: seed}, pos, input)
					options, ok := menu.EnchantmentOptions(tx, seed, pos, input)
					if ok != (len(enchants) != 0) {
						t.Fatalf("%d shelves, %v, seed %d: ok = %v, the session found %d options", shelves, input, seed, ok, len(enchants))
					}
					if !ok {
						continue
					}
					for i, option := range options {
						if option.Cost != costs[i] || !reflect.DeepEqual(option.Enchantments, enchants[i]) {
							t.Fatalf("%d shelves, %v, seed %d, option %d: %d %v, the session %d %v", shelves, input, seed, i, option.Cost, option.Enchantments, costs[i], enchants[i])
						}
					}
				}
			}
		})
	}
}

// placeBookshelves places n bookshelves, at most 16, two blocks away from
// pos, where an enchanting table counts them, and clears the ring between.
func placeBookshelves(tx *world.Tx, pos cube.Pos, n int) {
	placed := 0
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			for y := 0; y <= 1; y++ {
				p := pos.Add(cube.Pos{x, y, z})
				switch {
				case x >= -1 && x <= 1 && z >= -1 && z <= 1:
					tx.SetBlock(p, nil, nil)
				case placed < n:
					tx.SetBlock(p, block.Bookshelf{}, nil)
					placed++
				default:
					tx.SetBlock(p, nil, nil)
				}
			}
		}
	}
}
