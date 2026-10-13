package player

// LockRecipes removes Java recipe identifiers from the player's unlocked recipe state.
// It returns the number of identifiers that were unlocked before the call.
func (p *Player) LockRecipes(ids []string) int {
	changed := 0
	for _, id := range ids {
		if _, ok := p.unlockedRecipes[id]; ok {
			delete(p.unlockedRecipes, id)
			changed++
		}
	}
	return changed
}
