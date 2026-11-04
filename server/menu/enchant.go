package menu

import (
	"math"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// EnchantmentOptions returns the options of the enchanting table at pos for
// input, drawn from seed and the bookshelves around pos. ok is false for an
// input that cannot be enchanted: one that is not enchantable or already
// holds enchantments. The options are those Java's EnchantmentMenu.
// slotsChanged and getEnchantmentList give for the same seed, which is a
// Java int: seed is cut to its low 32 bits. An option whose Cost is 0 is
// not offered.
func EnchantmentOptions(tx *world.Tx, seed int64, pos cube.Pos, input item.Stack) (options [3]EnchantmentOption, ok bool) {
	enchantable, ok := input.Item().(item.Enchantable)
	if !ok || len(input.Enchantments()) > 0 {
		return options, false
	}
	javaSeed := int32(seed)
	bookshelves := searchBookshelves(tx, pos)

	// The costs come from one stream of the seed, then each enchantment
	// list from a stream of the seed plus its slot.
	random := vanilla.NewLegacyRandom(int64(javaSeed))
	for i := range options {
		if cost := enchantmentCost(&random, i, bookshelves); cost >= i+1 {
			options[i].Cost = cost
		}
	}
	table := tableEnchantments()
	for i := range options {
		if options[i].Cost > 0 {
			random = vanilla.NewLegacyRandom(int64(javaSeed + int32(i)))
			options[i].Enchantments = enchantmentList(&random, input, enchantable.EnchantmentValue(), options[i].Cost, table)
		}
	}
	return options, true
}

// enchantmentCost returns the cost of the option of slot of a table with
// that many bookshelves, as EnchantmentHelper.getEnchantmentCost.
func enchantmentCost(random *vanilla.Random, slot, bookshelves int) int {
	bookshelves = min(bookshelves, 15)
	selected := int(random.NextInt(8)) + 1 + bookshelves>>1 + int(random.NextInt(int32(bookshelves+1)))
	switch slot {
	case 0:
		return max(selected/3, 1)
	case 1:
		return selected*2/3 + 1
	default:
		return max(selected, bookshelves*2)
	}
}

// enchantmentList returns the enchantments of an option of the cost, as
// EnchantmentMenu.getEnchantmentList: those selectEnchantments draws, less
// one at random for a book that draws several.
func enchantmentList(random *vanilla.Random, input item.Stack, value, cost int, table []item.EnchantmentType) []item.Enchantment {
	list := selectEnchantments(random, input, value, cost, table)
	if _, book := input.Item().(item.Book); book && len(list) > 1 {
		i := int(random.NextInt(int32(len(list))))
		list = slices.Delete(list, i, i+1)
	}
	return list
}

// tableEnchantments returns the enchantments a table draws from, in order:
// those of Java's #in_enchanting_table that are registered, in the order of
// data/minecraft/tags/enchantment/non_treasure.json, which it names, then
// the other registered enchantments that are not treasure.
func tableEnchantments() []item.EnchantmentType {
	registered := item.Enchantments()
	byID := make(map[string]item.EnchantmentType, len(registered))
	for _, e := range registered {
		byID[strings.TrimPrefix(item.JavaEnchantmentID(e), "minecraft:")] = e
	}
	table := make([]item.EnchantmentType, 0, len(registered))
	for _, id := range inEnchantingTable {
		if e, ok := byID[id]; ok {
			table = append(table, e)
			delete(byID, id)
		}
	}
	for _, e := range registered {
		if _, other := byID[strings.TrimPrefix(item.JavaEnchantmentID(e), "minecraft:")]; other && !treasure(e) {
			table = append(table, e)
		}
	}
	return table
}

// inEnchantingTable is #minecraft:in_enchanting_table: #non_treasure, in
// its order.
var inEnchantingTable = []string{
	"protection", "fire_protection", "feather_falling", "blast_protection", "projectile_protection",
	"respiration", "aqua_affinity", "thorns", "depth_strider", "sharpness", "smite", "bane_of_arthropods",
	"knockback", "fire_aspect", "looting", "sweeping_edge", "efficiency", "silk_touch", "unbreaking",
	"fortune", "power", "punch", "flame", "infinity", "luck_of_the_sea", "lure", "loyalty", "impaling",
	"riptide", "channeling", "multishot", "quick_charge", "piercing", "density", "breach", "lunge",
}

// treasureEnchantment represents an enchantment that may be a treasure enchantment.
type treasureEnchantment interface {
	item.EnchantmentType
	Treasure() bool
}

// treasure reports if e is a treasure enchantment, which a table never
// offers.
func treasure(e item.EnchantmentType) bool {
	t, ok := e.(treasureEnchantment)
	return ok && t.Treasure()
}

// offeredByTable reports if an enchanting table may offer e for it, as
// EnchantmentHelper.getAvailableEnchantmentResults filters the enchantments
// of #in_enchanting_table: a treasure enchantment never, any other for a
// book, and otherwise only for its primary items, which may be fewer than
// the items an anvil puts it on.
func offeredByTable(e item.EnchantmentType, it world.Item) bool {
	if treasure(e) {
		return false
	}
	if _, book := it.(item.Book); book {
		return true
	}
	return item.IsPrimaryItem(e, it)
}

// selectEnchantments draws the enchantments of an item of the enchantment
// value from table for the cost, as EnchantmentHelper.selectEnchantment:
// the cost is raised and spread, then one enchantment of those whose costs
// hold it is drawn by weight, and more while a draw of 50 is at most the
// cost, which halves each time, among those compatible with the last.
func selectEnchantments(random *vanilla.Random, stack item.Stack, value, cost int, table []item.EnchantmentType) []item.Enchantment {
	// Guard against invalid enchantment values from custom items, which a
	// draw would panic on.
	value = max(value, 0)
	cost += 1 + int(random.NextInt(int32(value/4+1))) + int(random.NextInt(int32(value/4+1)))
	span := (random.NextFloat() + random.NextFloat() - 1) * 0.15
	// Math.round of the float cost + cost*span. The conversion keeps the
	// product from being fused into the sum, which Java rounds apart.
	c := float32(cost)
	cost = clamp(int(math.Floor(float64(c+float32(c*span))+0.5)), 1, math.MaxInt32)

	it := stack.Item()
	available := make([]item.Enchantment, 0, len(table))
	for _, enchant := range table {
		if !offeredByTable(enchant, it) {
			continue
		}
		// The highest level whose costs hold the cost.
		for i := enchant.MaxLevel(); i > 0; i-- {
			if minCost, maxCost := enchant.Cost(i); cost >= minCost && cost <= maxCost {
				available = append(available, item.NewEnchantment(enchant, i))
				break
			}
		}
	}
	if len(available) == 0 {
		return nil
	}
	selected := make([]item.Enchantment, 0, 4)
	enchant, ok := weightedRandomEnchantment(random, available)
	if !ok {
		return nil
	}
	selected = append(selected, enchant)
	for int(random.NextInt(50)) <= cost {
		// Only the enchantments compatible with the last drawn are left,
		// which leaves it out too.
		last := selected[len(selected)-1].Type()
		if available = filter(available, func(e item.Enchantment) bool {
			return compatibleEnchantments(last, e.Type())
		}); len(available) == 0 {
			break
		}
		if enchant, ok = weightedRandomEnchantment(random, available); !ok {
			break
		}
		selected = append(selected, enchant)
		cost /= 2
	}
	return selected
}

// compatibleEnchantments reports if a and b may go on one item, as
// Enchantment.areCompatible: they differ and neither refuses the other.
func compatibleEnchantments(a, b item.EnchantmentType) bool {
	return a != b && a.CompatibleWithEnchantment(b) && b.CompatibleWithEnchantment(a)
}

// searchBookshelves returns the number of bookshelves that power the table
// at pos, at most 15, as EnchantmentMenu.slotsChanged counts them: each of
// EnchantingTableBlock.BOOKSHELF_OFFSETS, two blocks out on the level of the
// table or the one above, that holds a bookshelf with a block of
// #enchantment_power_transmitter halfway to the table (isValidBookShelf).
// Halfway is the offset halved toward zero: (1, y, 0) for (2, y, 1).
func searchBookshelves(tx *world.Tx, pos cube.Pos) (shelves int) {
	for y := 0; y <= 1; y++ {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				if x > -2 && x < 2 && z > -2 && z < 2 {
					continue
				}
				if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(block.Bookshelf); !ok {
					continue
				}
				if powerTransmitter(tx.Block(pos.Add(cube.Pos{x / 2, y, z / 2}))) {
					shelves++
				}
			}
		}
	}
	return min(shelves, 15)
}

// powerTransmitter reports if b is in #enchantment_power_transmitter,
// which holds #replaceable: air, water, short grass, snow and others.
func powerTransmitter(b world.Block) bool {
	if _, air := b.(block.Air); air {
		return true
	}
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	in, _ := gamedata.BlockTagContains("minecraft:enchantment_power_transmitter", state.Name)
	return in
}

// weightedRandomEnchantment draws one of enchants by the weights of their
// rarities, as WeightedRandom.getRandomItem: false if the weights add up to
// nothing.
func weightedRandomEnchantment(random *vanilla.Random, enchants []item.Enchantment) (item.Enchantment, bool) {
	var totalWeight int
	for _, e := range enchants {
		totalWeight += e.Type().Rarity().Weight()
	}
	if totalWeight <= 0 || totalWeight > math.MaxInt32 {
		return item.Enchantment{}, false
	}
	r := int(random.NextInt(int32(totalWeight)))
	for _, e := range enchants {
		r -= e.Type().Rarity().Weight()
		if r < 0 {
			return e, true
		}
	}
	return item.Enchantment{}, false
}

// clamp clamps a value into the given range.
func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// filter iterates over elements of collection, returning an array of all
// elements function c returns true for.
func filter[E any](s []E, c func(E) bool) []E {
	a := make([]E, 0, len(s))
	for _, e := range s {
		if c(e) {
			a = append(a, e)
		}
	}
	return a
}
