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
