package loot

import (
	"maps"
	"slices"
	"testing"
)

// TestLootFunctionsRun runs the generations of TestLootTablesMatch with every loot function counted, and
// requires each to run, so that the comparison is seen to cover every handler of lootFunctions.
func TestLootFunctionsRun(t *testing.T) {
	counts := map[string]int{}
	saved := maps.Clone(lootFunctions)
	for k, f := range saved {
		lootFunctions[k] = func(it lootItem, m *modifier, c *Context) (lootItem, error) {
			counts[k]++
			return f(it, m, c)
		}
	}
	defer func() { lootFunctions = saved }()
	for _, id := range slices.Sorted(maps.Keys(tables)) {
		lootTableLine(id)
	}
	for _, k := range slices.Sorted(maps.Keys(saved)) {
		if counts[k] == 0 {
			t.Errorf("no generation of TestLootTablesMatch runs %s", k)
		}
		t.Logf("%s %d", k, counts[k])
	}
}
