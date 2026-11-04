package player

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// ExploreMap explores the map of the ID passed around the block the player
// is in, as a map the player holds is explored, and returns its data. It
// fails if no map of the ID exists.
func (p *Player) ExploreMap(id int64) (world.MapData, error) {
	return p.tx.UpdateMap(id, cube.PosFromVec3(p.Position()))
}
