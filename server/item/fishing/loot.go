package fishing

import (
	"fmt"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
)

// Loot returns source fishing-table output. The lure level affects hook wait
// time, not the Java fishing-table weights.
func Loot(luckLevel, _ int) (item.Stack, error) {
	return LootJava(luckLevel, false, rand.Int64())
}

// LootJava evaluates the builtin fishing table using a Java random stream and
// the source hook's open-water predicate.
func LootJava(luckLevel int, openWater bool, seed int64) (item.Stack, error) {
	random := java26.NewXoroshiroRandom(seed)
	stacks, err := loot.Generate("minecraft:gameplay/fishing", seed, loot.Context{
		Random: &random,
		Luck: float64(luckLevel),
		OpenWater: openWater,
		Entity: "minecraft:fishing_hook",
	})
	if err != nil { return item.Stack{}, err }
	if len(stacks) == 0 { return item.Stack{}, nil }
	if len(stacks) != 1 { return item.Stack{}, fmt.Errorf("fishing table generated %d stacks, expected exactly one", len(stacks)) }
	return stacks[0], nil
}

// LootWithRandom evaluates the source table using the sequence stream owned by
// the world that contains the fishing hook.
func LootWithRandom(luckLevel int, openWater bool, random *java26.Random) (item.Stack, error) {
	stacks, err := loot.Generate("minecraft:gameplay/fishing", 0, loot.Context{
		Random: random,
		Luck: float64(luckLevel),
		OpenWater: openWater,
		Entity: "minecraft:fishing_hook",
	})
	if err != nil { return item.Stack{}, err }
	if len(stacks) == 0 { return item.Stack{}, nil }
	if len(stacks) != 1 { return item.Stack{}, fmt.Errorf("fishing table generated %d stacks, expected exactly one", len(stacks)) }
	return stacks[0], nil
}
