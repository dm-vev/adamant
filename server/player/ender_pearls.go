package player

import "github.com/df-mc/dragonfly/server/entity"

// vanishEnderPearls ports ThrownEnderPearl.tick's ender_pearls_vanish_on_death
// check: pearls thrown by a player that has died are discarded instead of
// teleporting the respawned player later.
func (p *Player) vanishEnderPearls() {
	if !p.tx.World().GameRuleBool("ender_pearls_vanish_on_death") {
		return
	}
	for e := range p.tx.Entities() {
		pearl, ok := e.(*entity.Ent)
		if !ok || pearl.H().Type() != entity.EnderPearlType {
			continue
		}
		if b, ok := pearl.Behaviour().(*entity.ProjectileBehaviour); ok && b.Owner() == p.H() {
			_ = pearl.CloseIn(p.tx)
		}
	}
}
