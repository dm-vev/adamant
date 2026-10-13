package world

import "github.com/df-mc/dragonfly/server/block/cube"

// TickPrecipitation exposes one chunk precipitation step to world_test.
func (w *World) TickPrecipitation(tx *Tx, pos ChunkPos) { w.tickPrecipitation(tx, pos) }

// FindLightningRod exposes the lightning rod search to world_test.
func (tx *Tx) FindLightningRod(center cube.Pos) (cube.Pos, bool) { return tx.findLightningRod(center) }
