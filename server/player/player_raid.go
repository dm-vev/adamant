package player

import (
	"sort"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
)

// raidState is the raid omen of the player and the recipes it has unlocked.
type raidState struct {
	unlockedRecipes  map[string]struct{}
	raidOmenPosition *cube.Pos
}

func (p *Player) unlockedRecipeIDs() []string {
	ids := make([]string, 0, len(p.unlockedRecipes))
	for id := range p.unlockedRecipes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// UnlockRecipes unlocks the recipes of the Java Edition IDs passed for the
// player, keeping those of registered recipes, recipe.KnownJavaID, in its
// persistent unlock state, and shows the client the IDs passed as newly
// unlocked.
func (p *Player) UnlockRecipes(ids []string) {
	for _, id := range ids {
		if recipe.KnownJavaID(id) {
			p.unlockedRecipes[id] = struct{}{}
		}
	}
	p.client().ViewRecipesUnlocked(ids)
}

// UnlockedRecipeIDs returns the player's persisted Java recipe keys.
func (p *Player) UnlockedRecipeIDs() []string { return p.unlockedRecipeIDs() }

// RaidOmenPosition returns the village position recorded when Bad Omen became
// Raid Omen. The position is zero when there is no pending Raid Omen.
func (p *Player) RaidOmenPosition() cube.Pos {
	if p.raidOmenPosition == nil {
		return cube.Pos{}
	}
	return *p.raidOmenPosition
}

// RaidOmenLevel returns the current Raid Omen level, or zero when absent.
func (p *Player) RaidOmenLevel() int {
	for _, eff := range p.Effects() {
		if eff.Type() == effect.RaidOmen {
			return eff.Level()
		}
	}
	return 0
}

// GrantHeroOfTheVillage grants the raid victory effect for the specified number
// of game ticks.
func (p *Player) GrantHeroOfTheVillage(_ *world.Tx, durationTicks int) {
	if durationTicks <= 0 {
		return
	}
	p.AddEffect(effect.New(effect.HeroOfTheVillage, 1, time.Duration(durationTicks)*time.Second/20))
}

// TryActivateBadOmen converts Bad Omen to Raid Omen when a raid can begin at
// the player's current position.
func (p *Player) TryActivateBadOmen(tx *world.Tx, amplifier int) bool {
	if p.GameMode() == world.GameModeSpectator || tx.World().Difficulty() == world.DifficultyPeaceful {
		return false
	}
	pos := cube.PosFromVec3(p.Position())
	if !tx.TryApplyBadOmen(p, pos, amplifier) {
		return false
	}
	p.raidOmenPosition = &pos
	p.AddEffect(effect.New(effect.RaidOmen, amplifier+1, effect.RaidOmenDuration))
	return true
}

// ActivateRaidOmen attempts to start or extend the raid at the stored position.
func (p *Player) ActivateRaidOmen(tx *world.Tx) bool {
	if p.raidOmenPosition == nil || p.GameMode() == world.GameModeSpectator {
		return false
	}
	_ = tx.TryApplyRaidOmen(p)
	p.raidOmenPosition = nil
	return true
}
