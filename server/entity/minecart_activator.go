package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TickAfterBlockUpdates samples the native rail-activator component after
// circuit publication. Its entity event takes effect on the next normal tick,
// independently of movement, inventory transfer and lifetime accounting.
func (m *Minecart) TickAfterBlockUpdates(tx *world.Tx, _ int64) {
	b := minecartBaseBehaviour(m.Behaviour())
	pos := cube.PosFromVec3(minecartCentre(m.Position()))
	if isRailAt(tx, pos.Side(cube.FaceDown)) {
		pos = pos.Side(cube.FaceDown)
	}
	rail, ok := tx.BlockLoaded(pos)
	activator, active := rail.(block.ActivatorRail)
	b.activatorPowered = ok && active && activator.Powered
	b.activatorPending = true
}
