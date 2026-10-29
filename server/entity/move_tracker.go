package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// moveTracked is an entity whose movement its viewers are shown by a
// world.MoveTracker, as BDS shows that of an actor.
type moveTracked interface {
	moveTracker() (t *world.MoveTracker, interval int)
}

// moveTracker returns the tracker of the movement of the entity and the
// ticks between the moves its viewers are shown: world.MoveInterval for a
// mob, which clients carry out over those ticks, and zero for an entity of
// another kind, such as an item or a projectile, shown at once on every
// tick it moves.
func (e *Ent) moveTracker() (*world.MoveTracker, int) {
	if _, ok := e.living().(Living); ok {
		return &e.data.Moves, world.MoveInterval
	}
	return &e.data.Moves, 0
}

// movesShown notes a position the viewers of the entity were shown other
// than by its tracker, as by a teleport, for the moves after it to start
// from.
func (e *Ent) movesShown(pos mgl64.Vec3, rot cube.Rotation, headYaw float64) {
	e.data.Moves.Shown(pos, rot, headYaw, e.data.OnGround)
}
