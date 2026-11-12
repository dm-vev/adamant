package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TickPrecipitation exposes one chunk precipitation step to world_test.
func (w *World) TickPrecipitation(tx *Tx, pos ChunkPos) {
	w.tickPrecipitation(tx, pos, &precipitationSite{})
}

// FindLightningRod exposes the lightning rod search to world_test.
func (tx *Tx) FindLightningRod(center cube.Pos) (cube.Pos, bool) { return tx.findLightningRod(center) }

// FreshLight computes the light of the chunks within two of the one at pos
// from scratch, filling clones of the chunks within three of it and spreading
// their light. It returns a function to look up the sky and block light of a
// position in them.
func (tx *Tx) FreshLight(pos cube.Pos) func(cube.Pos) (sky, block uint8) {
	centre := chunkPosFromBlockPos(pos)
	clones := map[ChunkPos]*chunk.Chunk{}
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			p := ChunkPos{centre[0] + x, centre[1] + z}
			c := tx.chunk(p).Chunk.Clone()
			chunk.LightArea([]*chunk.Chunk{c}, int(p[0]), int(p[1])).Fill()
			clones[p] = c
		}
	}
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			area := make([]*chunk.Chunk, 0, 9)
			for dz := int32(-1); dz <= 1; dz++ {
				for dx := int32(-1); dx <= 1; dx++ {
					area = append(area, clones[ChunkPos{centre[0] + x + dx, centre[1] + z + dz}])
				}
			}
			chunk.LightArea(area, int(centre[0]+x)-1, int(centre[1]+z)-1).Spread()
		}
	}
	return func(p cube.Pos) (uint8, uint8) {
		c := clones[chunkPosFromBlockPos(p)]
		return c.SkyLight(uint8(p[0]&15), int16(p[1]), uint8(p[2]&15)), c.SubChunk(int16(p[1])).BlockLight(uint8(p[0]&15), uint8(p[1]&15), uint8(p[2]&15))
	}
}

// ReadyChunk returns the chunk of the loaded column at pos for tests.
func (tx *Tx) ReadyChunk(pos ChunkPos) *chunk.Chunk {
	c, ok := tx.World().chunks[pos]
	if !ok {
		return nil
	}
	return c.Chunk
}

// RegionPhases returns how many region phases the world ran.
func (w *World) RegionPhases() int64 { return w.regions.phases.Load() }

// RegionGroups exposes regionsOf to world_test: it groups the columns at
// the chunk positions passed into regions and returns the positions of each
// region.
func RegionGroups(columns []ChunkPos) [][]ChunkPos {
	refs := make([]columnRef, len(columns))
	for i, pos := range columns {
		refs[i] = columnRef{pos: pos}
	}
	var out [][]ChunkPos
	for _, group := range regionsOf(refs) {
		var positions []ChunkPos
		for _, ref := range group {
			positions = append(positions, ref.pos)
		}
		out = append(out, positions)
	}
	return out
}

// ColumnModified reports whether the loaded column at pos changed since it
// was last saved.
func (tx *Tx) ColumnModified(pos ChunkPos) bool {
	c, ok := tx.World().chunks[pos]
	return ok && c.modified.Load()
}

// RaidSpawnPosition exposes the search for where a raid wave spawns to
// world_test: for a raid around center, cooldown ticks before its wave.
func (tx *Tx) RaidSpawnPosition(center cube.Pos, cooldown int) (cube.Pos, bool) {
	return tx.raidSpawnPosition(&Raid{center: center, cooldown: cooldown})
}

// StrikeLightning exposes one lightning strike on the chunk at pos to
// world_test.
func (w *World) StrikeLightning(tx *Tx, pos ChunkPos) { w.strikeLightning(tx, pos) }

// EntityColumn returns the position of the column the entity of h is listed
// in, which it leaves for the column it moved into at its next tick.
func (tx *Tx) EntityColumn(h *EntityHandle) (ChunkPos, bool) {
	s, ok := tx.World().entities[h]
	if !ok {
		return ChunkPos{}, false
	}
	return s.pos, true
}

// ColumnTouched reports whether the loaded column at pos was touched since
// it was last saved.
func (tx *Tx) ColumnTouched(pos ChunkPos) bool {
	c, ok := tx.World().chunks[pos]
	return ok && c.touched.Load()
}

// EntityFromNBT decodes an entity of type t from the NBT data, as a world
// reads one from its provider.
func (w *World) EntityFromNBT(t EntityType, id int64, data map[string]any) *EntityHandle {
	return w.entityFromNBT(t, id, data)
}

// ScheduledTicks returns the block and then the fluid ticks the queues hold
// for the column at pos, as a save of it writes them.
func (tx *Tx) ScheduledTicks(pos ChunkPos) []chunk.SavedScheduledTick {
	w := tx.World()
	return append(w.scheduledBlockTicks.fromChunk(pos), w.scheduledFluidTicks.fromChunk(pos)...)
}
