package menu

import (
	"math"
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// EnchantmentOptions returns the options of the enchanting table at pos for
// input, drawn from seed and the bookshelves around pos. ok is false for an
// input that cannot be enchanted: one that is not enchantable or already
// holds enchantments. The draws are those of EnchantmentMenu: the seeded
// source draws the base cost, then, for each option in turn, its bonus,
// its cost and its enchantments.
func EnchantmentOptions(tx *world.Tx, seed int64, pos cube.Pos, input item.Stack) (options [3]EnchantmentOption, ok bool) {
	// First ensure that the item is enchantable and does not already have any enchantments.
	enchantable, ok := input.Item().(item.Enchantable)
	if !ok {
		// We can't enchant this item.
		return options, false
	}
	if len(input.Enchantments()) > 0 {
		// We can't enchant this item.
		return options, false
	}

	// Search for bookshelves around the enchanting table. Bookshelves help boost the value of the enchantments that
	// are selected, resulting in enchantments that are rarer but also more expensive.
	source := uint64(seed)
	random := rand.New(rand.NewPCG(source, source))
	bookshelves := searchBookshelves(tx, pos)
	value := enchantable.EnchantmentValue()

	// Calculate the base cost, used to calculate the upper, middle, and lower level costs.
	baseCost := random.IntN(8) + 1 + (bookshelves >> 1) + random.IntN(bookshelves+1)

	// Calculate the upper, middle, and lower level costs.
	upperLevelCost := max(baseCost/3, 1)
	middleLevelCost := baseCost*2/3 + 1
	lowerLevelCost := max(baseCost, bookshelves*2)

	// Create a list of available enchantments for each slot.
	for i, cost := range [3]int{upperLevelCost, middleLevelCost, lowerLevelCost} {
		options[i] = EnchantmentOption{Cost: cost, Enchantments: createEnchantments(random, input, value, cost)}
	}
	return options, true
}

// treasureEnchantment represents an enchantment that may be a treasure enchantment.
type treasureEnchantment interface {
	item.EnchantmentType
	Treasure() bool
}

// createEnchantments creates a list of enchantments for the given item stack and returns them.
func createEnchantments(random *rand.Rand, stack item.Stack, value, level int) []item.Enchantment {
	if value < 0 {
		// Guard against invalid enchantment values from custom items to avoid panics in IntN.
		value = 0
	}
	// Calculate the "random bonus" for this level. This factor is used in calculating the enchantment cost, used
	// during the selection of enchantments.
	randomBonus := (random.Float64() + random.Float64() - 1.0) * 0.15

	// Calculate the enchantment cost and clamp it to ensure it is always at least one with triangular distribution.
	cost := level + 1 + random.IntN(value/4+1) + random.IntN(value/4+1)
	cost = clamp(int(math.Round(float64(cost)+float64(cost)*randomBonus)), 1, math.MaxInt32)

	// Books are applicable to all enchantments, so make sure we have a flag for them here.
	it := stack.Item()
	_, book := it.(item.Book)

	// Now that we have our enchantment cost, we need to select the available enchantments. First, we iterate through
	// each possible enchantment.
	availableEnchants := make([]item.Enchantment, 0, len(item.Enchantments()))
	for _, enchant := range item.Enchantments() {
		if t, ok := enchant.(treasureEnchantment); ok && t.Treasure() {
			// We then have to ensure that the enchantment is not a treasure enchantment, as those cannot be selected through
			// the enchanting table.
			continue
		}
		if !book && !enchant.CompatibleWithItem(it) {
			// The enchantment is not compatible with the item.
			continue
		}

		// Now iterate through each possible level of the enchantment.
		for i := enchant.MaxLevel(); i > 0; i-- {
			// Use the level to calculate the minimum and maximum costs for this enchantment.
			if minCost, maxCost := enchant.Cost(i); cost >= minCost && cost <= maxCost {
				// If the cost is within the bounds, add the enchantment to the list of available enchantments.
				availableEnchants = append(availableEnchants, item.NewEnchantment(enchant, i))
				break
			}
		}
	}
	if len(availableEnchants) == 0 {
		// No available enchantments, so we can't really do much here.
		return nil
	}

	// Now we need to select the enchantments.
	selectedEnchants := make([]item.Enchantment, 0, len(availableEnchants))

	// Select the first enchantment using a weighted random algorithm, favouring enchantments that have a higher weight.
	// These weights are based on the enchantment's rarity, with common and uncommon enchantments having a higher weight
	// than rare and very rare enchantments.
	enchant := weightedRandomEnchantment(random, availableEnchants)
	selectedEnchants = append(selectedEnchants, enchant)

	// Remove the selected enchantment from the list of available enchantments, so we don't select it again.
	ind := slices.Index(availableEnchants, enchant)
	availableEnchants = slices.Delete(availableEnchants, ind, ind+1)

	// Based on the cost, select a random amount of additional enchantments.
	for random.IntN(50) <= cost {
		// Ensure that we don't have any conflicting enchantments. If so, remove them from the list of available
		// enchantments.
		lastEnchant := selectedEnchants[len(selectedEnchants)-1]
		if availableEnchants = filter(availableEnchants, func(enchant item.Enchantment) bool {
			return lastEnchant.Type().CompatibleWithEnchantment(enchant.Type())
		}); len(availableEnchants) == 0 {
			// We've exhausted all available enchantments.
			break
		}

		// Select another enchantment using the same weighted random algorithm.
		enchant = weightedRandomEnchantment(random, availableEnchants)
		selectedEnchants = append(selectedEnchants, enchant)

		// Remove the selected enchantment from the list of available enchantments, so we don't select it again.
		ind = slices.Index(availableEnchants, enchant)
		availableEnchants = slices.Delete(availableEnchants, ind, ind+1)

		// Halve the cost, so we have a lower chance of selecting another enchantment.
		cost /= 2
	}
	return selectedEnchants
}

// searchBookshelves searches for nearby bookshelves around the position passed, and returns the amount found.
func searchBookshelves(tx *world.Tx, pos cube.Pos) (shelves int) {
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			for y := 0; y <= 1; y++ {
				if x == 0 && z == 0 {
					// Ignore the centre block.
					continue
				}
				if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(block.Air); !ok {
					// There must be a one block space between the bookshelf and the player.
					continue
				}

				// Check for a bookshelf two blocks away.
				if _, ok := tx.Block(pos.Add(cube.Pos{x * 2, y, z * 2})).(block.Bookshelf); ok {
					shelves++
				}
				if x != 0 && z != 0 {
					// Check for a bookshelf two blocks away on the X axis.
					if _, ok := tx.Block(pos.Add(cube.Pos{x * 2, y, z})).(block.Bookshelf); ok {
						shelves++
					}
					// Check for a bookshelf two blocks away on the Z axis.
					if _, ok := tx.Block(pos.Add(cube.Pos{x, y, z * 2})).(block.Bookshelf); ok {
						shelves++
					}
				}

				if shelves >= 15 {
					// We've found enough bookshelves.
					return 15
				}
			}
		}
	}
	return shelves
}

// weightedRandomEnchantment returns a random enchantment from the given list of enchantments using the rarity weight of
// each enchantment.
func weightedRandomEnchantment(rs *rand.Rand, enchants []item.Enchantment) item.Enchantment {
	if len(enchants) == 0 {
		// Defensive fallback to avoid panics if callers pass an empty list.
		return item.Enchantment{}
	}
	var totalWeight int
	for _, e := range enchants {
		totalWeight += e.Type().Rarity().Weight()
	}
	if totalWeight <= 0 {
		// All weights are invalid; fall back to a deterministic choice.
		return enchants[0]
	}
	r := rs.IntN(totalWeight)
	for _, e := range enchants {
		r -= e.Type().Rarity().Weight()
		if r < 0 {
			return e
		}
	}
	panic("should never happen")
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
