package world

// TickPrecipitation exposes one chunk precipitation step to world_test.
func (w *World) TickPrecipitation(tx *Tx, pos ChunkPos) { w.tickPrecipitation(tx, pos) }
