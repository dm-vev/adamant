package world

import (
	"fmt"
	"maps"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// columnTo converts a Column to a chunk.Column so that it can be written to
// a provider.
func (w *World) columnTo(col *Column, pos ChunkPos) *chunk.Column {
	scheduled := make([]chunk.SavedScheduledTick, 0, 8)
	scheduled = append(scheduled, w.scheduledBlockTicks.fromChunk(pos)...)
	scheduled = append(scheduled, w.scheduledFluidTicks.fromChunk(pos)...)
	for i := range scheduled {
		scheduled[i].NativeBlock = w.nativeScheduledTickState(col, scheduled[i].ScheduledTick)
	}
	c := &chunk.Column{
		Chunk:          col.Chunk,
		Entities:       make([]chunk.Entity, 0, len(col.Entities)),
		BlockEntities:  make([]chunk.BlockEntity, 0, len(col.BlockEntities)),
		ScheduledTicks: scheduled,
		InhabitedTime:  col.InhabitedTime,
		PostProcessing: col.postProcessing,
	}
	for _, e := range col.Entities {
		if e.t.EncodeEntity() == "minecraft:player" {
			// Player entities are persisted separately from chunk data and should not be stored in the
			// chunk provider. Keeping them out of the provider avoids stale player entities being read
			// back after a restart.
			continue
		}

		data, ok := w.saveEntityNBT(e)
		if !ok {
			continue
		}
		c.Entities = append(c.Entities, chunk.Entity{ID: e.UniqueID(), Data: data})
	}
	for pos, be := range col.BlockEntities {
		data, ok := w.saveBlockEntityNBT(pos, be)
		if !ok {
			continue
		}
		if _, actor := data["id"]; !actor {
			// A placeholder state without block actor data, such as one a
			// world holds for a state Adamant does not implement.
			continue
		}
		c.BlockEntities = append(c.BlockEntities, chunk.BlockEntity{Pos: pos, Data: BlockActorNBT(data, pos)})
	}
	// Scheduled ticks are emitted above in per-queue insertion order. Their
	// positions, rather than the generated source column, determine ownership.
	return c
}

// saveEntityNBT returns the NBT the entity of e is saved with, as EntityNBT
// does. A panic of encoding it leaves the entity out of the save of its
// column, which still saves: it is reported as a fault of saving, and false
// returned.
func (w *World) saveEntityNBT(e *EntityHandle) (data map[string]any, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			f := newFault(FaultSave, r)
			f.Type, f.Pos, f.Entity = e.t.EncodeEntity(), cube.PosFromVec3(e.data.Pos), e
			w.reportFault(f, quarantineKey{})
		}
	}()
	return w.EntityNBT(e), true
}

// saveBlockEntityNBT returns the NBT of the block entity be at pos, as
// saveEntityNBT does for an entity.
func (w *World) saveBlockEntityNBT(pos cube.Pos, be Block) (data map[string]any, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			f := newFault(FaultSave, r)
			f.Type, f.Pos = w.blockFaultType(be), pos
			w.reportFault(f, quarantineKey{})
		}
	}()
	return be.(NBTer).EncodeNBT(), true
}

// EntityNBT encodes the native NBT an entity is saved with, in the world
// save and in structure templates.
func (w *World) EntityNBT(e *EntityHandle) map[string]any {
	data := MergeNativeEntityNBT(e.encodeNBT(), e.t.EncodeNBT(&e.data))
	if encode := w.conf.Entities.Config().EncodeSharedNBT; encode != nil {
		maps.Copy(data, encode(&e.data))
	}
	data["identifier"] = BedrockEntityIdentifier(e.t.EncodeEntity())
	return data
}

// entityFromNBT decodes an entity of a type from the native NBT it was saved
// with, giving it the unique ID passed. It returns nil for NBT that Java
// Entity.load refuses, which drops the entity.
func (w *World) entityFromNBT(t EntityType, id int64, data map[string]any) *EntityHandle {
	return withBlockRegistryNBT(data, w.conf.Blocks, func() *EntityHandle {
		h := entityFromData(t, id, data)
		if h == nil {
			return nil
		}
		if decode := w.conf.Entities.Config().DecodeSharedNBT; decode != nil {
			decode(&h.data, data)
		}
		return h
	})
}

// nativeScheduledTickState keeps the actual native state when it still matches
// the queued Java type. A stale tick retains its old type rather than ticking a
// different block after the save is opened by BDS.
func (w *World) nativeScheduledTickState(col *Column, tick chunk.ScheduledTick) uint32 {
	withinHeight := !tick.Pos.OutOfBounds(w.Range())
	if withinHeight && tick.Kind == chunk.BlockTick {
		b := w.blockInChunk(col, tick.Pos)
		state := NativeStateAt(col.Chunk, uint8(tick.Pos[0]), int16(tick.Pos[1]), uint8(tick.Pos[2]))
		name, canonical := gamedata.BlockStateName(state.CanonicalID(w.conf.Blocks))
		if canonical && name == tick.Type || !canonical && scheduledTickType(w.conf.Blocks, b) == tick.Type {
			return w.conf.Blocks.BlockRuntimeID(b)
		}
	} else if withinHeight && tick.Kind == chunk.FluidTick {
		for layer := uint8(0); layer < 2; layer++ {
			rid := col.Block(uint8(tick.Pos[0]&15), int16(tick.Pos[1]), uint8(tick.Pos[2]&15), layer)
			b, ok := w.conf.Blocks.BlockByRuntimeID(rid)
			if liquid, isLiquid := b.(Liquid); ok && isLiquid && scheduledFluidTickMatches(liquid, tick.Type) {
				return rid
			}
		}
	}
	b, ok := w.resolveScheduledTickBlock(tick)
	if !ok {
		panic(fmt.Sprintf("save scheduled tick: unknown %v type %q", tick.Kind, tick.Type))
	}
	return w.conf.Blocks.BlockRuntimeID(b)
}

// columnFrom converts a chunk.Column to a Column after reading it from a
// provider. The Column is marked ready if ready is set.
func (w *World) columnFrom(c *chunk.Column, pos ChunkPos, ready bool) *Column {
	col := newColumn(c.Chunk)
	col.InhabitedTime = c.InhabitedTime
	col.postProcessing = c.PostProcessing
	states := c.Chunk.MissingBlockStates()
	if len(states) != 0 {
		col.missingBlockStates = make(map[cube.Pos]pendingMissingBlockState, len(states))
	}
	for _, state := range states {
		if state.Layer != 0 {
			continue
		}
		state.Position[0] += int(pos[0]) * 16
		state.Position[2] += int(pos[1]) * 16
		col.missingBlockStates[state.Position] = pendingMissingBlockState{properties: state.Properties}
	}
	col.Entities = make([]*EntityHandle, 0, len(c.Entities))
	if len(c.Entities) > 0 {
		col.entityIndices = make(map[*EntityHandle]int, len(c.Entities))
	}
	col.BlockEntities = make(map[cube.Pos]Block, len(c.BlockEntities))
	for _, e := range c.Entities {
		eid, ok := e.Data["identifier"].(string)
		if !ok {
			w.log.Error("read column: entity without identifier field", "ID", e.ID)
			continue
		}
		if eid == "minecraft:player" {
			// Players are managed separately from chunk entities, so ignore persisted player entries that
			// may have been saved by older versions.
			continue
		}
		t, ok := w.conf.Entities.Lookup(eid)
		if !ok {
			w.log.Error("read column: unknown entity type", "ID", e.ID, "type", eid)
			continue
		}
		h := w.entityFromNBT(t, e.ID, e.Data)
		if h == nil {
			w.log.Error("read column: entity has an invalid position or rotation", "ID", e.ID, "type", eid)
			continue
		}
		col.addEntity(h)
	}
	w.decodeBlockEntities(col, w.dropStaleBlockEntities(col, c.BlockEntities))
	blockTicks := make([]scheduledTick, 0, len(c.ScheduledTicks))
	fluidTicks := make([]scheduledTick, 0, len(c.ScheduledTicks))
	for _, saved := range c.ScheduledTicks {
		update := saved.ScheduledTick
		var b Block
		var ok bool
		if update.Type == "" {
			b, ok = w.conf.Blocks.BlockByRuntimeID(saved.NativeBlock)
		} else {
			b, ok = w.resolveScheduledTickBlock(update)
		}
		if !ok {
			w.log.Error("read column: cannot resolve scheduled tick type", "type", update.Type, "kind", update.Kind, "position", update.Pos)
			continue
		}
		typeName := update.Type
		if typeName == "" {
			if liquid, fluid := b.(Liquid); fluid {
				update.Kind = chunk.FluidTick
				typeName = scheduledFluidTickType(liquid)
			} else {
				typeName = scheduledTickType(w.conf.Blocks, b)
			}
		}
		tick := scheduledTick{
			pos:      update.Pos,
			btype:    typeName,
			t:        int64(saved.Delay),
			deferred: true,
			priority: update.Priority,
		}
		switch update.Kind {
		case chunk.BlockTick:
			blockTicks = append(blockTicks, tick)
		case chunk.FluidTick:
			fluidTicks = append(fluidTicks, tick)
		default:
			w.log.Error("read column: invalid scheduled tick kind", "kind", update.Kind, "position", update.Pos)
		}
	}
	w.scheduledBlockTicks.add(blockTicks)
	w.scheduledFluidTicks.add(fluidTicks)
	if ready {
		col.markReady()
	}
	return col
}

// dropStaleBlockEntities removes saved block entities of blocks that have
// none, as BDS discards block actors without a matching block. Worlds saved
// while such a block was still unimplemented hold these records. The column
// is marked modified so it is saved again without them.
func (w *World) dropStaleBlockEntities(col *Column, entities []chunk.BlockEntity) []chunk.BlockEntity {
	var kept []chunk.BlockEntity
	for i, be := range entities {
		rid := col.Chunk.Block(uint8(be.Pos[0]), int16(be.Pos[1]), uint8(be.Pos[2]), 0) //nolint:staticcheck // QF1008: the chunk of the column is read, not the column.
		if w.conf.Blocks.NBTBlock(rid) {
			if kept != nil {
				kept = append(kept, be)
			}
			continue
		}
		if kept == nil {
			kept = append(make([]chunk.BlockEntity, 0, len(entities)), entities[:i]...)
		}
	}
	if kept == nil {
		return entities
	}
	col.setModified()
	w.log.Debug("read column: dropped block entities of blocks without one", "count", len(entities)-len(kept))
	return kept
}

// decodeBlockEntities uses the same registry and position tracking for saved
// and generated block entities.
func (w *World) decodeBlockEntities(c *Column, entities []chunk.BlockEntity) {
	for _, be := range entities {
		rid := c.Chunk.Block(uint8(be.Pos[0]), int16(be.Pos[1]), uint8(be.Pos[2]), 0) //nolint:staticcheck // QF1008: the chunk of the column is read, not the column.
		b, ok := w.conf.Blocks.BlockByRuntimeID(rid)
		if !ok {
			w.log.Error("read column: no block with runtime ID", "ID", rid)
			continue
		}
		nb, ok := b.(NBTer)
		if !ok {
			w.log.Error("read column: block with nbt does not implement NBTer", "block", fmt.Sprintf("%#v", b))
			continue
		}
		decoded := DecodeNBT(nb, be.Data, w.conf.Blocks).(Block)
		decoded = restoreJavaBlockActor(c.Chunk, be.Pos, decoded)
		if tracked, ok := decoded.(PositionTrackingBlock); ok && tracked.TrackingHandle() != 0 {
			decoded = tracked.WithTrackingHandle(w.TrackPosition(be.Pos, tracked.TrackingHandle()))
		}
		c.updateBlockEntity(be.Pos, decoded)
	}
}
