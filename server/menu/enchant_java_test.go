package menu_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// javaOptions are the options Java 26.3's enchanting table offers for an
// item, with a number of bookshelves, for an enchantment seed. They were
// worked out from Java's code apart from this port:
// EnchantmentMenu.slotsChanged and getEnchantmentList, EnchantmentHelper.
// getEnchantmentCost, selectEnchantment and getAvailableEnchantmentResults,
// WeightedRandom.getRandomItem and LegacyRandomSource, with the enchantments,
// their order in #in_enchanting_table and the item tags of
// server-26.3.jar's data. Each option is "<cost>:" and its enchantments, by
// Java ID, then level; a cost of 0 is an option the table does not offer.
var javaOptions = []struct {
	it      world.Item
	shelves int
	seed    int64
	want    string
}{
	{item.Pickaxe{Tier: item.ToolTierIron}, 0, 0, "2: efficiency 1 unbreaking 1; 2: unbreaking 1 efficiency 1; 6: efficiency 1 unbreaking 1"},
	{item.Pickaxe{Tier: item.ToolTierIron}, 0, 7, "2: unbreaking 1 efficiency 1; 5: unbreaking 1 efficiency 1; 3: unbreaking 1 efficiency 1"},
	// The second option costs selected*2/3+1 = 1 for a draw of 1, less
	// than its slot+1, so it is not offered.
	{item.Pickaxe{Tier: item.ToolTierIron}, 0, -1, "1: efficiency 1; 0:; 6: unbreaking 1 efficiency 1"},
	{item.Pickaxe{Tier: item.ToolTierIron}, 0, 2491, "2: unbreaking 1; 5: efficiency 1; 7: unbreaking 2"},
	{item.Pickaxe{Tier: item.ToolTierIron}, 0, 2147483647, "1: efficiency 1; 5: efficiency 1; 7: unbreaking 1"},
	{item.Bow{}, 5, 3, "3: power 1; 3: power 1; 10: power 2 unbreaking 1"},
	{item.Bow{}, 5, -123456789, "3: power 1; 10: unbreaking 1; 10: power 2"},
	{item.Trident{}, 15, 11, "8: unbreaking 1 impaling 2; 10: unbreaking 1; 30: riptide 2 unbreaking 3 impaling 4"},
	{item.Trident{}, 15, 24301, "8: unbreaking 1; 9: unbreaking 1; 30: riptide 2 unbreaking 3"},
	{item.Mace{}, 15, 42, "4: unbreaking 1; 9: density 2; 30: unbreaking 3 breach 3"},
	{item.Mace{}, 15, -2147483648, "5: density 1; 11: density 2; 30: density 4 unbreaking 3"},
	{item.Chestplate{Tier: item.ArmourTierIron{}}, 15, 5, "5: projectile_protection 2 unbreaking 1; 12: projectile_protection 2 unbreaking 2 thorns 1; 30: protection 3 unbreaking 3"},
	{item.Chestplate{Tier: item.ArmourTierIron{}}, 15, 99, "6: protection 1; 15: blast_protection 2 unbreaking 2; 30: protection 3 unbreaking 3"},
	{item.Spear{Tier: item.ToolTierCopper}, 5, 17, "2: sharpness 1; 7: smite 1; 10: bane_of_arthropods 2"},
	{item.Spear{Tier: item.ToolTierCopper}, 5, 256, "3: smite 1; 7: smite 1; 13: sharpness 2"},
	{item.Helmet{Tier: item.ArmourTierGold{}}, 15, 8, "9: protection 2 unbreaking 1; 9: protection 2 aqua_affinity 1; 30: protection 3 respiration 3"},
	{item.Helmet{Tier: item.ArmourTierGold{}}, 15, 1234567, "6: projectile_protection 1; 15: protection 3; 30: fire_protection 4"},
	{item.FishingRod{}, 15, 77, "7: unbreaking 1; 10: unbreaking 1; 30: unbreaking 3"},
	// One bookshelf: the third option costs max(selected, 2) = 2 for a
	// draw of 2, less than 3.
	{item.Crossbow{}, 1, 13, "2: piercing 1; 3: piercing 1; 0:"},
	{item.Boots{Tier: item.ArmourTierDiamond{}}, 15, 3, "7: protection 2 unbreaking 1; 14: unbreaking 2 depth_strider 2; 30: depth_strider 3 protection 3"},
}

// TestEnchantmentOptionsAsJava checks the options of an enchanting table
// against those Java computes for the same seed (javaOptions).
func TestEnchantmentOptionsAsJava(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	for _, c := range javaOptions {
		<-w.Exec(func(tx *world.Tx) {
			pos := cube.Pos{0, 64, 0}
			placeBookshelves(tx, pos, c.shelves)
			tx.SetBlock(pos, block.EnchantingTable{}, nil)
			options, ok := menu.EnchantmentOptions(tx, c.seed, pos, item.NewStack(c.it, 1))
			if !ok {
				t.Fatalf("%T cannot be enchanted", c.it)
			}
			if got := formatOptions(options); got != c.want {
				t.Errorf("%T, %d bookshelves, seed %d:\n got: %s\nwant: %s", c.it, c.shelves, c.seed, got, c.want)
			}
		})
	}
}

// formatOptions formats options as javaOptions holds them.
func formatOptions(options [3]menu.EnchantmentOption) string {
	parts := make([]string, 0, len(options))
	for _, o := range options {
		var b strings.Builder
		fmt.Fprintf(&b, "%d:", o.Cost)
		for _, e := range o.Enchantments {
			fmt.Fprintf(&b, " %s %d", strings.TrimPrefix(item.JavaEnchantmentID(e.Type()), "minecraft:"), e.Level())
		}
		parts = append(parts, b.String())
	}
	return strings.Join(parts, "; ")
}

// placeBookshelves places n bookshelves, at most 32, two blocks out from
// pos on its level and the one above, where an enchanting table counts
// them, and clears the ring between with air.
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

// TestBookshelvesAsJava counts the bookshelves of an enchanting table as
// EnchantmentMenu.slotsChanged does through EnchantingTableBlock.
// isValidBookShelf: a bookshelf at an offset two blocks out powers the
// table if the block at half the offset, rounded toward zero, is in
// #enchantment_power_transmitter (#replaceable). For (2, 0, 1) that is
// (1, 0, 0), not (1, 0, 1).
func TestBookshelvesAsJava(t *testing.T) {
	for _, tc := range []struct {
		name   string
		blocks map[cube.Pos]world.Block
		want   int
	}{
		{"one behind air", map[cube.Pos]world.Block{{2, 0, 0}: block.Bookshelf{}}, 1},
		{"one behind stone", map[cube.Pos]world.Block{{2, 0, 0}: block.Bookshelf{}, {1, 0, 0}: block.Stone{}}, 0},
		{"one behind short grass", map[cube.Pos]world.Block{{2, 0, 0}: block.Bookshelf{}, {1, 0, 0}: block.ShortGrass{}}, 1},
		{"one behind water", map[cube.Pos]world.Block{{2, 0, 0}: block.Bookshelf{}, {1, 0, 0}: block.Water{Depth: 8, Still: true}}, 1},
		{"one off the axis, stone at the corner", map[cube.Pos]world.Block{{2, 1, 1}: block.Bookshelf{}, {1, 1, 1}: block.Stone{}}, 1},
		{"one off the axis, stone on the axis", map[cube.Pos]world.Block{{2, 1, 1}: block.Bookshelf{}, {1, 1, 0}: block.Stone{}}, 0},
		{"one at the corner, stone at the corner", map[cube.Pos]world.Block{{-2, 0, -2}: block.Bookshelf{}, {-1, 0, -1}: block.Stone{}}, 0},
		{"one two blocks up", map[cube.Pos]world.Block{{2, 2, 0}: block.Bookshelf{}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{0, 64, 0}
				placeBookshelves(tx, pos, 0)
				tx.SetBlock(pos, block.EnchantingTable{}, nil)
				for p, b := range tc.blocks {
					tx.SetBlock(pos.Add(p), b, nil)
				}
				if got := menu.Bookshelves(tx, pos); got != tc.want {
					t.Errorf("%d bookshelves, want %d", got, tc.want)
				}
			})
		})
	}
}
