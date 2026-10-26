package world

import "github.com/df-mc/dragonfly/server/block/cube"

// blockDestroyer breaks a block as Level.destroyBlock does. The block
// package registers it.
var blockDestroyer func(tx *Tx, pos cube.Pos, drops bool)

// RegisterBlockDestroyer sets the function DestroyBlock breaks blocks with.
func RegisterBlockDestroyer(f func(tx *Tx, pos cube.Pos, drops bool)) {
	blockDestroyer = f
}

// DestroyBlock ports Level.destroyBlock: the block at pos breaks with its
// particles and, if drops is true, drops what it drops by hand.
func (tx *Tx) DestroyBlock(pos cube.Pos, drops bool) {
	if blockDestroyer == nil {
		tx.SetBlock(pos, nil, nil)
		return
	}
	blockDestroyer(tx, pos, drops)
}

// firePlacer places fire as flint and steel and fire charges do. The block
// package registers it.
var firePlacer func(tx *Tx, pos cube.Pos) bool

// RegisterFirePlacer sets the function PlaceFire places fire with.
func RegisterFirePlacer(f func(tx *Tx, pos cube.Pos) bool) {
	firePlacer = f
}

// PlaceFire ports BaseFireBlock.canBePlacedAt and getState for flint and
// steel and fire charges: fire, or soul fire on soul fire base blocks, goes
// into air where it survives or completes a Nether portal frame. It reports
// if the fire was placed.
func (tx *Tx) PlaceFire(pos cube.Pos) bool {
	return firePlacer != nil && firePlacer(tx, pos)
}
