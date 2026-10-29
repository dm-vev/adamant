package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestLockRecipesRemovesOnlyUnlockedIDs(t *testing.T) {
	p := &Player{playerData: &playerData{unlockedRecipes: map[string]struct{}{
		"minecraft:test": {}, "minecraft:other": {},
	}}}
	if got := p.LockRecipes([]string{"minecraft:test", "minecraft:test", "minecraft:missing"}); got != 1 {
		t.Fatalf("lock count=%d, want 1", got)
	}
	if got := p.UnlockedRecipeIDs(); len(got) != 1 || got[0] != "minecraft:other" {
		t.Fatalf("remaining recipe IDs=%v, want [minecraft:other]", got)
	}
}

// TestConfigKeepsUnlockedRecipes applies a config with unlocked recipes:
// the player keeps them, in a map that takes more. The map was dropped, so
// unlocking a recipe, as a knowledge book does, wrote to a nil map.
func TestConfigKeepsUnlockedRecipes(t *testing.T) {
	var data world.EntityData
	Config{UnlockedRecipes: []string{"minecraft:stick"}}.Apply(&data)
	p := &Player{playerData: data.Data.(*playerData)}
	if got := p.UnlockedRecipeIDs(); len(got) != 1 || got[0] != "minecraft:stick" {
		t.Fatalf("unlocked recipe IDs=%v, want [minecraft:stick]", got)
	}
	p.unlockedRecipes["minecraft:ladder"] = struct{}{}
}
