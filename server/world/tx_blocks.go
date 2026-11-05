package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
)

// block reads a block from the position passed. If a chunk is not yet loaded
// at that position, the chunk is loaded, or generated if it could not be found
// in the world save, and the block returned.
func (tx *Tx) block(pos cube.Pos) Block {
	return tx.World().blockInChunk(tx.chunk(chunkPosFromBlockPos(pos)), pos)
}

// biome reads the Biome at the position passed. If a chunk is not yet loaded
// at that position, the chunk is loaded, or generated if it could not be found
// in the world save, and the Biome returned.
func (tx *Tx) biome(pos cube.Pos) Biome {
	if pos.OutOfBounds(tx.Range()) {
		// Fast way out.
		return ocean()
	}
	id := int(tx.chunk(chunkPosFromBlockPos(pos)).Biome(uint8(pos[0]), int16(pos[1]), uint8(pos[2])))
	b, ok := BiomeByID(id)
	if !ok {
		tx.World().log.Error("biome not found by ID", "ID", id)
		return unknownBiome{id: id}
	}
	return b
}

// SetOpts holds several parameters that may be set to disable updates in the
// World of different kinds as a result of a call to SetBlock.
type SetOpts struct {
	// DisableBlockUpdates makes SetBlock not update any neighbouring blocks as
	// a result of the SetBlock call.
	DisableBlockUpdates bool
	// DisableLiquidDisplacement disables the displacement of liquid blocks to
	// the second layer (or back to the first layer, if it already was on the
	// second layer). Disabling this is not widely recommended unless
	// performance is very important, or where it is known no liquid can be
	// present anyway.
	DisableLiquidDisplacement bool
	// DisableRedstoneUpdates makes SetBlock not invalidate the redstone engine
	// around the changed block. This is used by the redstone engine while
	// applying its own block-state updates to avoid duplicate same-tick work.
	DisableRedstoneUpdates bool

	// Source mutations publish both native layers together and apply Java's
	// independent notification and block-entity side-effect flags.
	nativeState             *NativeState
	disableRemovalHandler   bool
	disablePlacementHandler bool
	disableViewUpdates      bool
	// circuitPublication marks the engine's own output, rather than an
	// external replacement that may recreate a native component.
	circuitPublication bool
}

// setBlock writes a block to the position passed. If a chunk is not yet loaded
// at that position, the chunk is first loaded or generated if it could not be
// found in the world save. setBlock panics if the block passed has not yet
// been registered using RegisterBlock(). Nil may be passed as the block to set
// the block to air.
//
// A SetOpts struct may be passed to additionally modify behaviour of setBlock,
// specifically to improve performance under specific circumstances. Nil should
// be passed where performance is not essential, to make sure the world is
// updated adequately.
//
// setBlock should be avoided in situations where performance is critical when
// needing to set a lot of blocks to the world. BuildStructure may be used
// instead.
func (tx *Tx) setBlock(pos cube.Pos, b Block, opts *SetOpts) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	if tx.region != nil {
		if !tx.regionWrites(pos) {
			tx.deferToOwner(func(tx *Tx) { tx.setBlock(pos, b, opts) })
			return
		}
		tx.lockShared()
		defer tx.unlockShared()
	}
	if b == nil {
		b = w.conf.Blocks.Air()
	}
	if opts == nil {
		opts = &SetOpts{}
	}

	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	c := tx.chunk(chunkPosFromBlockPos(pos))

	rid := w.conf.Blocks.BlockRuntimeID(b)
	oldLight := w.cellLightAt(c, pos)
	redstoneAfterRelevant := isRedstoneRelevant(b)
	needOldBlock := !opts.DisableRedstoneUpdates || !redstoneAfterRelevant

	oldRID := c.Block(x, y, z, 0)
	needOldBlock = needOldBlock || w.conf.Blocks.NBTBlock(oldRID)
	oldBlock := w.conf.Blocks.BlockByRuntimeIDOrAir(oldRID)
	if w.conf.Blocks.NBTBlock(oldRID) {
		if blockEntity, ok := c.BlockEntities[pos]; ok {
			oldBlock = blockEntity
		}
	}
	b = w.replacePositionTrackingBlock(tx, pos, oldBlock, b)
	if handler, ok := oldBlock.(BlockRemovalHandler); ok && !opts.disableRemovalHandler {
		handler.Removed(pos, tx, b)
	}

	var before uint32
	if rid != w.conf.Blocks.AirRuntimeID() && !opts.DisableLiquidDisplacement {
		before = oldRID
	}
	lightBefore := w.cellLightAt(c, pos)

	c.modified = true
	if opts.nativeState == nil {
		c.SetBlock(x, y, z, 0, rid)
	} else {
		SetNativeState(c.Chunk, x, y, z, *opts.nativeState)
		c.surfaceBlockSet(x, y, z, opts.nativeState.Block)
	}
	if _, pending := c.missingBlockStates[pos]; pending && c.CanonicalizeMissingBlockState(pos, 0) {
		delete(c.missingBlockStates, pos)
	}
	c.blockVersion = blockVersionClock.Add(1)
	c.invalidateRandomTickSubChunks()
	c.invalidateSubChunkHeightMaps()
	c.invalidateNetworkSubChunkPayloads()
	if w.conf.Blocks.NBTBlock(rid) {
		c.updateBlockEntity(pos, b)
		c.invalidateBlockEntityPayloads()
	} else {
		c.updateBlockEntity(pos, nil)
		c.invalidateBlockEntityPayloads()
	}
	if len(c.faultyActors) != 0 {
		if a, ok := c.faultyActors[pos]; ok && c.tickerBlockEntityVersions[pos] != a.version {
			// The block entity that panicked was removed or replaced.
			w.forgetActor(c, pos)
		}
	}
	if len(w.blockFaults.m) != 0 {
		w.blockSet(pos, b)
	}

	if !opts.DisableLiquidDisplacement {
		var secondLayer Block

		airRID := w.conf.Blocks.AirRuntimeID()
		if rid == airRID {
			if li := c.Block(x, y, z, 1); li != airRID {
				c.SetBlock(x, y, z, 0, li)
				c.SetBlock(x, y, z, 1, airRID)
				c.invalidateRandomTickSubChunks()
				c.invalidateSubChunkHeightMaps()
				c.invalidateNetworkSubChunkPayloads()
				secondLayer = w.conf.Blocks.Air()
				b = w.conf.Blocks.BlockByRuntimeIDOrAir(li)
			}
		} else if w.conf.Blocks.LiquidDisplacingBlock(rid) {
			if w.conf.Blocks.LiquidBlock(before) {
				l := w.conf.Blocks.BlockByRuntimeIDOrAir(before)
				if b.(LiquidDisplacer).CanDisplace(l.(Liquid)) {
					c.SetBlock(x, y, z, 1, before)
					secondLayer = l
				}
			}
		} else if li := c.Block(x, y, z, 1); li != airRID {
			c.SetBlock(x, y, z, 1, airRID)
			secondLayer = w.conf.Blocks.Air()
		}

		if secondLayer != nil {
			w.viewBlockUpdate(c, pos, secondLayer, 1)
		}
	}
	w.relightIfChanged(c, pos, lightBefore)
	if redstoneAfterRelevant || (needOldBlock && isRedstoneRelevant(oldBlock)) {
		w.redstone.forget(pos)
	}
	w.relightIfChanged(c, pos, oldLight)
	w.redstone.replaceCircuit(pos, oldBlock, b, opts.circuitPublication)

	if !opts.disableViewUpdates {
		w.viewBlockUpdate(c, pos, b, 0)
		if opts.nativeState != nil {
			w.viewBlockUpdate(c, pos, w.conf.Blocks.BlockByRuntimeIDOrAir(opts.nativeState.Liquid), 1)
		}
	}

	if !opts.DisableBlockUpdates {
		tx.updateAroundSetBlock(pos, oldBlock, b)
	}
	if !opts.DisableRedstoneUpdates {
		w.redstone.invalidateAroundBlockChange(pos, oldBlock, b, RedstoneUpdateCauseBlockUpdate, w.Range())
	}
	w.updatePOI(pos, b)
	if handler, ok := b.(PlacementHandler); ok && !opts.disablePlacementHandler {
		handler.Placed(pos, tx, oldBlock)
	}
}

// setBlockEntity updates block entity data without triggering block updates.
func (tx *Tx) setBlockEntity(pos cube.Pos, b Block) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	if tx.region != nil {
		if !tx.regionWrites(pos) {
			tx.deferToOwner(func(tx *Tx) { tx.setBlockEntity(pos, b) })
			return
		}
		tx.lockShared()
		defer tx.unlockShared()
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))

	rid := w.conf.Blocks.BlockRuntimeID(b)
	if !w.conf.Blocks.NBTBlock(rid) || c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 0) != rid {
		tx.setBlock(pos, b, nil)
		return
	}
	b = w.replacePositionTrackingBlock(tx, pos, c.BlockEntities[pos], b)
	c.updateBlockEntity(pos, b)
	c.blockVersion = blockVersionClock.Add(1)
	// Chunk and sub-chunk responses cache encoded block entities.
	c.invalidateBlockEntityPayloads()
	c.modified = true
}

// setBiome sets the Biome at the position passed. If a chunk is not yet loaded
// at that position, the chunk is first loaded or generated if it could not be
// found in the world save.
func (tx *Tx) setBiome(pos cube.Pos, b Biome) {
	if pos.OutOfBounds(tx.Range()) {
		// Fast way out.
		return
	}
	if tx.region != nil && !tx.regionWrites(pos) {
		tx.deferToOwner(func(tx *Tx) { tx.setBiome(pos, b) })
		return
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))
	c.modified = true
	c.SetBiome(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), uint32(b.EncodeBiome()))
	c.invalidateNetworkBiomePayload()
}

// buildStructure builds a Structure passed at a specific position in the
// world. Unlike setBlock, it takes a Structure implementation, which provides
// blocks to be placed at a specific location. buildStructure is specifically
// optimised to be able to process a large batch of chunks simultaneously and
// will do so within much less time than separate setBlock calls would. The
// method operates on a per-chunk basis, setting all blocks within a single
// chunk part of the Structure before moving on to the next chunk.
func (tx *Tx) buildStructure(pos cube.Pos, s Structure) {
	if tx.region != nil {
		// A structure is relit across the chunks around it at once.
		tx.deferToOwner(func(tx *Tx) { tx.buildStructure(pos, s) })
		return
	}
	w := tx.World()
	dim := s.Dimensions()
	width, height, length := dim[0], dim[1], dim[2]
	if width <= 0 || height <= 0 || length <= 0 {
		return
	}
	maxX, maxY, maxZ := pos[0]+width, pos[1]+height, pos[2]+length
	f := func(x, y, z int) Block {
		return tx.block(cube.Pos{pos[0] + x, pos[1] + y, pos[2] + z})
	}

	// We approach this on a per-chunk basis, so that we can keep only one chunk
	// in memory at a time while not needing to acquire a new chunk lock for
	// every block. This also allows us not to send block updates, but instead
	// send a single chunk update once.
	var touched []ChunkPos
	defer func() { w.relightChunks(touched) }()
	for chunkX := pos[0] >> 4; chunkX <= (maxX-1)>>4; chunkX++ {
		for chunkZ := pos[2] >> 4; chunkZ <= (maxZ-1)>>4; chunkZ++ {
			chunkPos := ChunkPos{int32(chunkX), int32(chunkZ)}
			c := tx.chunk(chunkPos)

			baseX, baseZ := chunkX<<4, chunkZ<<4
			for i, sub := range c.Sub() {
				baseY := (i + (w.Range()[0] >> 4)) << 4
				if baseY>>4 < pos[1]>>4 {
					continue
				} else if baseY >= maxY {
					break
				}

				for localY := 0; localY < 16; localY++ {
					yOffset := baseY + localY
					if yOffset > w.Range()[1] || yOffset >= maxY {
						// We've hit the height limit for blocks.
						break
					} else if yOffset < w.Range()[0] || yOffset < pos[1] {
						// We've got a block below the minimum, but other blocks might still reach above
						// it, so don't break but continue.
						continue
					}
					for localX := 0; localX < 16; localX++ {
						xOffset := baseX + localX
						if xOffset < pos[0] || xOffset >= maxX {
							continue
						}
						for localZ := 0; localZ < 16; localZ++ {
							zOffset := baseZ + localZ
							if zOffset < pos[2] || zOffset >= maxZ {
								continue
							}
							b, liq := s.At(xOffset-pos[0], yOffset-pos[1], zOffset-pos[2], f)
							lightPos := cube.Pos{xOffset, yOffset, zOffset}
							oldLight := w.cellLightAt(c, lightPos)
							if b != nil {
								nbtPos := cube.Pos{xOffset, yOffset, zOffset}
								oldRID := sub.Block(uint8(xOffset), uint8(yOffset), uint8(zOffset), 0)
								oldBlock := w.conf.Blocks.BlockByRuntimeIDOrAir(oldRID)
								if w.conf.Blocks.NBTBlock(oldRID) {
									if blockEntity, ok := c.BlockEntities[nbtPos]; ok {
										oldBlock = blockEntity
									}
								}
								b = w.replacePositionTrackingBlock(tx, nbtPos, oldBlock, b)
								if handler, ok := oldBlock.(BlockRemovalHandler); ok {
									handler.Removed(nbtPos, tx, b)
								}
								rid := w.conf.Blocks.BlockRuntimeID(b)
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 0, rid)
								c.invalidateRandomTickSubChunks()
								c.invalidateSubChunkHeightMaps()
								c.invalidateNetworkSubChunkPayloads()

								if w.conf.Blocks.NBTBlock(rid) {
									c.updateBlockEntity(nbtPos, b)
									c.invalidateBlockEntityPayloads()
								} else {
									c.updateBlockEntity(nbtPos, nil)
									c.invalidateBlockEntityPayloads()
								}
								w.updatePOI(nbtPos, b)
								w.redstone.replaceCircuit(nbtPos, oldBlock, b, false)
							}
							if liq != nil {
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 1, w.conf.Blocks.BlockRuntimeID(liq))
							} else if len(sub.Layers()) > 1 {
								sub.SetBlock(uint8(xOffset), uint8(yOffset), uint8(zOffset), 1, w.conf.Blocks.AirRuntimeID())
							}
							w.relightIfChanged(c, lightPos, oldLight)
						}
					}
				}
			}
			c.SetBlock(0, 0, 0, 0, c.Block(0, 0, 0, 0)) // Make sure the heightmap is recalculated.
			// The structure set its blocks past SetBlock.
			c.surface = nil
			c.modified = true
			touched = append(touched, chunkPos)

			// After setting all blocks of the structure within a single chunk,
			// we show the new chunk to all viewers once.
			for viewer := range c.viewers {
				viewer.ViewChunk(chunkPos, w.Dimension(), c)
			}
		}
	}
}

// liquid attempts to return a Liquid block at the position passed. This
// Liquid may be in the foreground or in any other layer. If found, the Liquid
// is returned. If not, the bool returned is false.
func (tx *Tx) liquid(pos cube.Pos) (Liquid, bool) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return nil, false
	}
	return w.liquidInColumn(tx.chunk(chunkPosFromBlockPos(pos)), pos)
}

// mayContainLiquid reports whether a block from lo to hi, inclusive, may
// hold a liquid: it is false only if none of the sub chunks the range
// touches has a liquid in the palette of any of its layers.
func (tx *Tx) mayContainLiquid(lo, hi cube.Pos) bool {
	w := tx.World()
	r := w.Range()
	minY, maxY := max(lo[1], r[0]), min(hi[1], r[1])
	if minY > maxY {
		return false
	}
	low, high := chunkPosFromBlockPos(lo), chunkPosFromBlockPos(hi)
	for cx := low[0]; cx <= high[0]; cx++ {
		for cz := low[1]; cz <= high[1]; cz++ {
			if w.subChunksMayContainLiquid(tx.chunk(ChunkPos{cx, cz}), minY, maxY) {
				return true
			}
		}
	}
	return false
}

// setLiquid sets a Liquid at a specific position in the World. Unlike
// setBlock, setLiquid will not necessarily overwrite any existing blocks. It
// will instead be in the same position as a block currently there, unless
// there already is a Liquid at that position, in which case it will be
// overwritten. If nil is passed for the Liquid, any Liquid currently present
// will be removed.
func (tx *Tx) setLiquid(pos cube.Pos, b Liquid) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return
	}
	if tx.region != nil {
		if !tx.regionWrites(pos) {
			tx.deferToOwner(func(tx *Tx) { tx.setLiquid(pos, b) })
			return
		}
		tx.lockShared()
		defer tx.unlockShared()
	}
	chunkPos := chunkPosFromBlockPos(pos)
	c := tx.chunk(chunkPos)
	lightBefore := w.cellLightAt(c, pos)
	defer w.relightIfChanged(c, pos, lightBefore)
	if b == nil {
		w.removeLiquids(c, pos)
		tx.doBlockUpdatesAround(pos)
		w.redstone.invalidateAround(pos, pos, RedstoneUpdateCauseBlockUpdate, w.Range())
		return
	}
	x, y, z := uint8(pos[0]), int16(pos[1]), uint8(pos[2])
	if !replaceable(w, c, pos, b) {
		if displacer, ok := w.blockInChunk(c, pos).(LiquidDisplacer); !ok || !displacer.CanDisplace(b) {
			return
		}
	}
	rid := w.conf.Blocks.BlockRuntimeID(b)
	if w.removeLiquids(c, pos) {
		c.SetBlock(x, y, z, 0, rid)
		c.invalidateRandomTickSubChunks()
		c.invalidateSubChunkHeightMaps()
		c.invalidateNetworkSubChunkPayloads()
		w.viewBlockUpdate(c, pos, b, 0)
	} else {
		c.SetBlock(x, y, z, 1, rid)
		w.viewBlockUpdate(c, pos, b, 1)
	}
	c.modified = true

	tx.doBlockUpdatesAround(pos)
	w.redstone.invalidateAround(pos, pos, RedstoneUpdateCauseBlockUpdate, w.Range())
	if handler, ok := b.(LiquidPlacementHandler); ok {
		handler.LiquidPlaced(pos, tx)
	}
}

// additionalLiquid checks if the block at a position has additional liquid on
// another layer and returns the liquid if so.
func (tx *Tx) additionalLiquid(pos cube.Pos) (Liquid, bool) {
	w := tx.World()
	if pos.OutOfBounds(w.Range()) {
		// Fast way out.
		return nil, false
	}
	c := tx.chunk(chunkPosFromBlockPos(pos))
	id := c.Block(uint8(pos[0]), int16(pos[1]), uint8(pos[2]), 1)

	b, ok := w.conf.Blocks.BlockByRuntimeID(id)
	if !ok {
		w.log.Error("additionalLiquid: no block with runtime ID", "ID", id)
		return nil, false
	}
	liq, ok := b.(Liquid)
	return liq, ok
}

// light returns the light level at the position passed. This is the highest of
// the sky and block light. The light value returned is a value in the range
// 0-15, where 0 means there is no light present, whereas 15 means the block is
// fully lit.
func (tx *Tx) light(pos cube.Pos) uint8 {
	w := tx.World()
	w.flushLight()
	if pos[1] < w.ra[0] {
		// Fast way out.
		w.flushLight()
		return 0
	}
	if !hasSkyLight(w.conf.Dim) {
		// Without a sky, only blocks light the world.
		return tx.blockLight(pos)
	}
	if pos[1] > w.ra[1] {
		// Above the rest of the world, so full skylight.
		w.flushLight()
		return 15
	}
	chunkPos := chunkPosFromBlockPos(pos)
	c, ok := w.loadedChunk(chunkPos)
	w.flushLightAt(chunkPos, c)
	if !ok {
		return 0
	}
	return c.Light(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
}

// skyLight returns the skylight level at the position passed. This light level
// is not influenced by blocks that emit light, such as torches. The light
// value, similarly to light, is a value in the range 0-15, where 0 means no
// light is present.
func (tx *Tx) skyLight(pos cube.Pos) uint8 {
	w := tx.World()
	if pos[1] < w.ra[0] || !hasSkyLight(w.conf.Dim) {
		// Fast way out.
		w.flushLight()
		return 0
	}
	if pos[1] > w.ra[1] {
		// Above the rest of the world, so full skylight.
		w.flushLight()
		return 15
	}
	chunkPos := chunkPosFromBlockPos(pos)
	c := tx.chunk(chunkPos)
	w.flushLightAt(chunkPos, c)
	return c.SkyLight(uint8(pos[0]), int16(pos[1]), uint8(pos[2]))
}

// doBlockUpdatesAround schedules block updates directly around and on the
// position passed.
func (tx *Tx) doBlockUpdatesAround(pos cube.Pos) {
	w := tx.World()
	if w == nil || pos.OutOfBounds(w.Range()) {
		return
	}
	tx.lockShared()
	defer tx.unlockShared()
	changed := pos

	tx.updateNeighbour(pos, changed)
	pos.Neighbours(func(pos cube.Pos) {
		tx.updateNeighbour(pos, changed)
	}, w.Range())
}

func (tx *Tx) updateNeighbour(pos, changedNeighbour cube.Pos) {
	tx.lockShared()
	defer tx.unlockShared()
	deferred := true
	if b, loaded := tx.BlockLoaded(pos); loaded {
		if listener, ok := b.(NeighbourChangeListener); ok {
			listener.NeighbourChanged(pos, changedNeighbour, tx)
			deferred = false
		}
		if liquid, ok := tx.additionalLiquid(pos); ok {
			if listener, ok := liquid.(NeighbourChangeListener); ok {
				listener.NeighbourChanged(pos, changedNeighbour, tx)
			}
		}
	}
	if deferred {
		tx.World().updateNeighbour(pos, changedNeighbour)
	}
}

// chunk reads a chunk from the position passed. If a chunk at that position is
// not yet loaded, the chunk is loaded from the provider, or generated if it
// did not yet exist. Additionally, chunks newly loaded have the light in them
// calculated before they are returned.
func (tx *Tx) chunk(pos ChunkPos) *Column {
	if r := tx.region; r != nil {
		return r.columnOrVoid(tx.w, pos)
	}
	w := tx.World()
	if w.lastChunk != nil && w.lastChunkPos == pos {
		return w.lastChunk
	}
	c, ok := w.chunks[pos]
	if ok && c.settled {
		w.lastChunkPos, w.lastChunk = pos, c
		return c
	}
	if ok {
		w.waitColumnReady(pos, c)
		c.ensureLight(w, pos)
		w.finishChunkRequest(tx, pos, c)
		if c.settled = c.Ready() && c.poisIndexed && c.circuitsIndexed; c.settled {
			delete(w.unsettled, pos)
		}
		return c
	}
	c, err := w.loadChunk(pos)
	w.waitColumnReady(pos, c)
	c.ensureLight(w, pos)
	w.finishChunkRequest(tx, pos, c)
	if err != nil {
		w.log.Error("load chunk: "+err.Error(), "X", pos[0], "Z", pos[1])
	}
	return c
}

// LoadArea loads the chunks overlapping the horizontal block area from min to
// max, generating those that do not exist yet ahead of the chunks nothing
// waits for. It does not wait for them: the returned channel is closed once
// every chunk is ready, so a caller off the world goroutine can wait for an
// area without holding up the ticks of the world.
func (tx *Tx) LoadArea(min, max cube.Pos) <-chan struct{} {
	w := tx.World()
	done := make(chan struct{})
	if tx.region != nil {
		// Regions load no chunks: the owner starts loading them after the
		// phase.
		tx.deferToOwner(func(tx *Tx) {
			loaded := tx.LoadArea(min, max)
			go func() {
				<-loaded
				close(done)
			}()
		})
		return done
	}
	minX, minZ, maxX, maxZ := int32(min[0]>>4), int32(min[2]>>4), int32(max[0]>>4), int32(max[2]>>4)
	pending := int((maxX - minX + 1) * (maxZ - minZ + 1))
	if pending <= 0 {
		close(done)
		return done
	}
	ready := func(*Tx, *Column) {
		if pending--; pending == 0 {
			close(done)
		}
	}
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			pos := ChunkPos{x, z}
			if !w.loadChunkAsync(tx, pos, ready) {
				ready(tx, nil)
				continue
			}
			if w.generatorQueue != nil {
				w.generatorQueue.prioritize(pos)
			}
		}
	}
	return done
}
