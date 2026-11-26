package menu_test

import (
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/menu"
	"github.com/dm-vev/adamant/server/world"
)

// BenchmarkEnchantmentOptions measures the options of an enchanting table
// with no bookshelves, which an enchanting request computes twice: for an
// iron pickaxe, as BenchmarkItemStackRequest enchants, and for a book, which
// every enchantment that is no treasure applies to.
func BenchmarkEnchantmentOptions(b *testing.B) {
	for name, input := range map[string]item.Stack{
		"pickaxe": item.NewStack(item.Pickaxe{Tier: item.ToolTierIron}, 1),
		"book":    item.NewStack(item.Book{}, 1),
	} {
		b.Run(name, func(b *testing.B) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				b.ReportAllocs()
				for b.Loop() {
					if _, ok := menu.EnchantmentOptions(tx, 7, cube.Pos{}, input); !ok {
						b.Fatal("no options")
					}
				}
			})
		})
	}
}
