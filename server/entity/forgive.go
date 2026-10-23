package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// forgiver is a neutral mob that can drop its persistent anger at a player.
type forgiver interface {
	forgive(playerUUID string)
}

// ForgiveDeadPlayer ports ServerPlayer.tellNeutralMobsThatIDied: with the
// forgive_dead_players gamerule on, neutral mobs within 32 blocks
// horizontally and 10 vertically of the dead player's block stop being angry
// at that player.
func ForgiveDeadPlayer(tx *world.Tx, player world.Entity) {
	if !tx.World().GameRuleBool("forgive_dead_players") {
		return
	}
	pos := cube.PosFromVec3(player.Position()).Vec3()
	box := cube.Box(pos[0]-32, pos[1]-10, pos[2]-32, pos[0]+33, pos[1]+11, pos[2]+33)
	id := player.H().UUID().String()
	for e := range tx.EntitiesWithin(box) {
		if n, ok := e.(forgiver); ok {
			n.forgive(id)
		}
	}
}

func (z *ZombifiedPiglin) forgive(id string) { s := z.z(); s.forgiveAnger(&s.ai, id) }

func (e *Enderman) forgive(id string) { s := e.s(); s.forgiveAnger(&s.ai, id) }

func (w *Wolf) forgive(id string) { b := w.behaviour(); b.forgiveAnger(&b.ai, id) }

func (m *IronGolem) forgive(id string) { b := m.b(); b.forgiveAnger(&b.ai, id) }

func (b *Bee) forgive(id string) { s := b.state(); s.forgiveAnger(&s.ai, id) }
