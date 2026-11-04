package item_test

import (
	"slices"
	"sort"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	_ "github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
)

// oldEnchantments is Enchantments as it was, before it kept its order: the
// enchantments of the registry, one for each ID registered, sorted by the ID
// each was registered with last. It reads the registry through
// EnchantmentByID, for the IDs from -64 to 8191.
func oldEnchantments() []item.EnchantmentType {
	var e []item.EnchantmentType
	for id := -64; id < 8192; id++ {
		if t, ok := item.EnchantmentByID(id); ok {
			e = append(e, t)
		}
	}
	ids := make(map[item.EnchantmentType]int, len(e))
	for _, t := range e {
		ids[t], _ = item.EnchantmentID(t)
	}
	sort.Slice(e, func(i, j int) bool {
		return ids[e[i]] < ids[e[j]]
	})
	return e
}

// customEnchantment is an enchantment of a plugin.
type customEnchantment struct{ name string }

func (c customEnchantment) Name() string                                      { return c.name }
func (customEnchantment) MaxLevel() int                                       { return 1 }
func (customEnchantment) Cost(int) (int, int)                                 { return 1000, 1000 }
func (customEnchantment) Rarity() item.EnchantmentRarity                      { return item.EnchantmentRarityVeryRare }
func (customEnchantment) CompatibleWithEnchantment(item.EnchantmentType) bool { return true }
func (customEnchantment) CompatibleWithItem(world.Item) bool                  { return false }

// TestEnchantmentsOrder checks that Enchantments returns the enchantments
// in the order it sorted them in before it kept the order, for the default
// registry and after enchantments are registered, also with IDs lower than
// some registered before and with an enchantment registered with two IDs,
// and that the slice it returns is the caller's. The enchantments
// registered cost more than any table offers and suit no item.
func TestEnchantmentsOrder(t *testing.T) {
	check := func(what string) {
		t.Helper()
		got, want := item.Enchantments(), oldEnchantments()
		if !slices.Equal(got, want) {
			t.Fatalf("%s: Enchantments() = %v, sorted as before %v", what, got, want)
		}
	}
	check("default")
	if n := len(item.Enchantments()); n < 30 {
		t.Fatalf("%d enchantments in the default registry", n)
	}

	got := item.Enchantments()
	first := got[0]
	got[0] = nil
	if e := item.Enchantments(); e[0] != first {
		t.Fatalf("writing into the slice returned changed the registry: %v", e[0])
	}

	item.RegisterEnchantment(7001, customEnchantment{"test a"})
	check("a at 7001")
	item.RegisterEnchantment(7000, customEnchantment{"test b"})
	check("b at 7000")
	item.RegisterEnchantment(6999, customEnchantment{"test c"})
	item.RegisterEnchantment(7002, customEnchantment{"test c"})
	check("c at 6999 and 7002")
	if e := item.Enchantments(); !slices.Equal(e[len(e)-4:], []item.EnchantmentType{
		customEnchantment{"test b"}, customEnchantment{"test a"}, customEnchantment{"test c"}, customEnchantment{"test c"},
	}) {
		t.Fatalf("the enchantments registered are last as %v", e[len(e)-4:])
	}

	// Registering while others read is as safe as it was.
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for range 200 {
				if e := item.Enchantments(); len(e) == 0 {
					t.Error("no enchantments")
					return
				}
			}
			item.RegisterEnchantment(7100+i, customEnchantment{"test concurrent"})
		})
	}
	wg.Wait()
	check("concurrent")
}
