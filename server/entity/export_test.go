package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// ForceVillageSiegeTonight makes the coming night of w a siege night, as one
// in ten VillageSiege rolls.
func ForceVillageSiegeTonight(w *world.World) {
	s := customSpawnersOf(w)
	s.siegeTonight, s.siegeRolledAt = true, int64(w.Time())-int64(w.Time())%24000+18000
}

// OminousBannerStack returns the ominous banner raiders carry.
var OminousBannerStack = ominousBannerStack

// DragonFlame returns the cloud the flaming phase of a dragon breathed.
func DragonFlame(d *EnderDragon) *world.EntityHandle { return d.behaviour().ph.flame }

// DragonFlameCount returns and SetDragonFlameCount sets how often the dragon
// breathed since it landed.
func DragonFlameCount(d *EnderDragon) int       { return d.behaviour().ph.flameCount }
func SetDragonFlameCount(d *EnderDragon, n int) { d.behaviour().ph.flameCount = n }

// DragonInWall reports if blocks the dragon cannot break slow it.
func DragonInWall(d *EnderDragon) bool { return d.behaviour().inWall }

// DragonNodes returns the nodes the dragon flies between.
func DragonNodes(tx *world.Tx, d *EnderDragon) [24]cube.Pos {
	d.behaviour().ensureDragonNodes(tx)
	return d.behaviour().nodes
}

// DragonClosestNode returns the node closest to the dragon.
func DragonClosestNode(tx *world.Tx, d *EnderDragon) int { return d.behaviour().closestNode(tx, d.Ent) }

// DragonFindPath returns the nodes of the path the dragon finds.
func DragonFindPath(tx *world.Tx, d *EnderDragon, start, end int, final *cube.Pos) ([]cube.Pos, bool) {
	p := d.behaviour().findPath(tx, start, end, final)
	return p.nodes[:p.n], p.valid
}

// NewDragonBreathCloud returns a cloud of the dragon's breath.
var NewDragonBreathCloud = newDragonBreathCloud
