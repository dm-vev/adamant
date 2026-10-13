package player

import "testing"

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
