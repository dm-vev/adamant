package enchantment_test

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
)

// javaCosts holds, for each enchantment of Java 26.3
// (data/minecraft/enchantment/*.json), its weight, max_level, and the base
// and per_level_above_first of its min_cost and max_cost.
var javaCosts = map[string][6]int{
	"aqua_affinity":         {2, 1, 1, 0, 41, 0},
	"bane_of_arthropods":    {5, 5, 5, 8, 25, 8},
	"binding_curse":         {1, 1, 25, 0, 50, 0},
	"blast_protection":      {2, 4, 5, 8, 13, 8},
	"breach":                {2, 4, 15, 9, 65, 9},
	"channeling":            {1, 1, 25, 0, 50, 0},
	"density":               {5, 5, 5, 8, 25, 8},
	"depth_strider":         {2, 3, 10, 10, 25, 10},
	"efficiency":            {10, 5, 1, 10, 51, 10},
	"feather_falling":       {5, 4, 5, 6, 11, 6},
	"fire_aspect":           {2, 2, 10, 20, 60, 20},
	"fire_protection":       {5, 4, 10, 8, 18, 8},
	"flame":                 {2, 1, 20, 0, 50, 0},
	"fortune":               {2, 3, 15, 9, 65, 9},
	"frost_walker":          {2, 2, 10, 10, 25, 10},
	"impaling":              {2, 5, 1, 8, 21, 8},
	"infinity":              {1, 1, 20, 0, 50, 0},
	"knockback":             {5, 2, 5, 20, 55, 20},
	"looting":               {2, 3, 15, 9, 65, 9},
	"loyalty":               {5, 3, 12, 7, 50, 0},
	"luck_of_the_sea":       {2, 3, 15, 9, 65, 9},
	"lunge":                 {5, 3, 5, 8, 25, 8},
	"lure":                  {2, 3, 15, 9, 65, 9},
	"mending":               {2, 1, 25, 25, 75, 25},
	"multishot":             {2, 1, 20, 0, 50, 0},
	"piercing":              {10, 4, 1, 10, 50, 0},
	"power":                 {10, 5, 1, 10, 16, 10},
	"projectile_protection": {5, 4, 3, 6, 9, 6},
	"protection":            {10, 4, 1, 11, 12, 11},
	"punch":                 {2, 2, 12, 20, 37, 20},
	"quick_charge":          {5, 3, 12, 20, 50, 0},
	"respiration":           {2, 3, 10, 10, 40, 10},
	"riptide":               {2, 3, 17, 7, 50, 0},
	"sharpness":             {10, 5, 1, 11, 21, 11},
	"silk_touch":            {1, 1, 15, 0, 65, 0},
	"smite":                 {5, 5, 5, 8, 25, 8},
	"soul_speed":            {1, 3, 10, 10, 25, 10},
	"sweeping_edge":         {2, 3, 5, 9, 20, 9},
	"swift_sneak":           {1, 3, 25, 25, 75, 25},
	"thorns":                {1, 3, 10, 20, 60, 20},
	"unbreaking":            {5, 3, 5, 8, 55, 8},
	"vanishing_curse":       {1, 1, 25, 0, 50, 0},
	"wind_burst":            {2, 3, 15, 9, 65, 9},
}

// TestJavaCostsAndWeights checks the weight, the max level and the cost of
// every level of each registered enchantment against Java's. An enchanting
// table draws its enchantments by those weights, among those whose costs
// hold its own (EnchantmentHelper.getAvailableEnchantmentResults).
func TestJavaCostsAndWeights(t *testing.T) {
	for _, e := range item.Enchantments() {
		id := strings.TrimPrefix(item.JavaEnchantmentID(e), "minecraft:")
		want, ok := javaCosts[id]
		if !ok {
			t.Errorf("%s is not a Java enchantment", id)
			continue
		}
		if w := e.Rarity().Weight(); w != want[0] || e.MaxLevel() != want[1] {
			t.Errorf("%s: weight %d and max level %d, want %d and %d", id, w, e.MaxLevel(), want[0], want[1])
		}
		for level := 1; level <= e.MaxLevel(); level++ {
			minCost, maxCost := e.Cost(level)
			wantMin, wantMax := want[2]+want[3]*(level-1), want[4]+want[5]*(level-1)
			if minCost != wantMin || maxCost != wantMax {
				t.Errorf("%s %d: costs %d to %d, want %d to %d", id, level, minCost, maxCost, wantMin, wantMax)
			}
		}
	}
}
