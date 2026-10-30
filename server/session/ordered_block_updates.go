package session

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"time"
)

const (
	maxPendingBlockUpdates = 8192
	maxBlockUpdatesPerTick = 128
)

type blockUpdateKey struct {
	pos   cube.Pos
	layer uint8
}

type blockUpdateData struct {
	pos               v2193.BlockPos
	runtimeID         uint32
	layer             uint32
	flags             uint32
	repeatBlockUpdate bool
	nbt               map[string]any
	actorOnly         bool
	actors            []map[string]any
	afterPackets      []proto.Packet
	after             *cube.Pos
}

func (s *Session) enqueueBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	update := blockUpdateData{
		pos:       v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		runtimeID: networkBlockID(s.br, b),
		layer:     uint32(layer),
		flags:     v2193.BlockUpdateNetwork,
	}
	if _, bell := b.(block.Bell); bell {
		update.flags |= v2193.BlockUpdateNeighbours
	}
	if v, ok := clientBlockActor(b); ok {
		if updater, ok := v.(world.UpdateNBTer); ok {
			update.nbt = s.clientUpdateNBT(updater.UpdateNBT())
			if _, bell := b.(block.Bell); bell && update.nbt != nil {
				update.nbt = clientActorNBT(update.nbt, pos)
			}
		} else if nbtData := v.EncodeNBT(); nbtData["id"] != nil {
			update.nbt = clientActorNBT(nbtData, pos)
		}
	}

	s.blockUpdatesMu.Lock()
	if s.blockUpdates == nil {
		s.blockUpdates = make(map[blockUpdateKey]blockUpdateData, 64)
	}
	key := blockUpdateKey{pos: pos, layer: uint8(layer)}
	previous, exists := s.blockUpdates[key]
	if !exists && len(s.blockUpdates) >= maxPendingBlockUpdates {
		s.blockUpdatesMu.Unlock()
		s.logBlockUpdateDrop()
		return
	}
	// The newer state supersedes actor data, but an already queued sound
	// still belongs to the earlier accepted interaction and must be heard.
	update.afterPackets = previous.afterPackets
	s.blockUpdates[key] = update
	s.blockUpdatesMu.Unlock()
}

func (s *Session) flushBlockUpdates() {
	s.blockUpdatesMu.Lock()
	updates := s.blockUpdates
	if len(updates) == 0 {
		s.blockUpdatesMu.Unlock()
		return
	}
	s.blockUpdates = make(map[blockUpdateKey]blockUpdateData, 64)
	s.blockUpdatesMu.Unlock()

	sent := 0
	visiting := make(map[blockUpdateKey]bool)
	var send func(blockUpdateKey)
	send = func(key blockUpdateKey) {
		update, ok := updates[key]
		if !ok || sent >= maxBlockUpdatesPerTick || visiting[key] {
			return
		}
		visiting[key] = true
		if update.after != nil {
			parent := blockUpdateKey{pos: *update.after}
			send(parent)
			if _, pending := updates[parent]; pending {
				delete(visiting, key)
				return
			}
		}
		delete(visiting, key)
		if sent >= maxBlockUpdatesPerTick {
			return
		}
		if !update.actorOnly {
			s.writePacket(&v2193.UpdateBlockPacket{
				Position:          update.pos,
				NewBlockRuntimeID: update.runtimeID,
				Flags:             update.flags,
				Layer:             update.layer,
			})
			if update.repeatBlockUpdate {
				s.writePacket(&v2193.UpdateBlockPacket{Position: update.pos, NewBlockRuntimeID: update.runtimeID, Flags: v2193.BlockUpdateNeighbours, Layer: update.layer})
			}
			if update.nbt != nil {
				s.writePacket(&v2193.BlockActorDataPacket{
					Position: update.pos,
					NBTData:  update.nbt,
				})
			}
		}
		for _, data := range update.actors {
			s.writePacket(&v2193.BlockActorDataPacket{Position: update.pos, NBTData: data})
		}
		for _, p := range update.afterPackets {
			s.writePacket(p)
		}
		sent++
		delete(updates, key)
	}
	for key := range updates {
		send(key)
	}
	if len(updates) == 0 {
		return
	}

	s.blockUpdatesMu.Lock()
	if len(s.blockUpdates) == 0 {
		s.blockUpdates = updates
	} else {
		for key, update := range updates {
			// Updates enqueued during this flush describe newer world state.
			if _, newer := s.blockUpdates[key]; !newer {
				s.blockUpdates[key] = update
			}
		}
	}
	s.blockUpdatesMu.Unlock()
}

// enqueuePacketAfterBlock retains an effect behind a pending state/actor update.
// An already flushed state needs only the effect; queue limits still apply.
func (s *Session) enqueuePacketAfterBlock(pos cube.Pos, p proto.Packet) {
	s.blockUpdatesMu.Lock()
	defer s.blockUpdatesMu.Unlock()
	if s.blockUpdates == nil {
		s.blockUpdates = make(map[blockUpdateKey]blockUpdateData, 64)
	}
	key := blockUpdateKey{pos: pos}
	update, exists := s.blockUpdates[key]
	if !exists {
		if len(s.blockUpdates) >= maxPendingBlockUpdates {
			s.logBlockUpdateDrop()
			return
		}
		update = blockUpdateData{pos: v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}, actorOnly: true}
	}
	if len(update.afterPackets) >= maxBlockUpdatesPerTick {
		s.logBlockUpdateDrop()
		return
	}
	update.afterPackets = append(update.afterPackets, p)
	s.blockUpdates[key] = update
}

// enqueueBellRingUpdate preserves the repeated state and actor notification
// observed on BDS, without bypassing the existing block queue/backlog limits.
func (s *Session) enqueueBellRingUpdate(pos cube.Pos, bell block.Bell) {
	data := clientActorNBT(bell.EncodeNBT(), pos)
	s.blockUpdatesMu.Lock()
	defer s.blockUpdatesMu.Unlock()
	if s.blockUpdates == nil {
		s.blockUpdates = make(map[blockUpdateKey]blockUpdateData, 64)
	}
	key := blockUpdateKey{pos: pos}
	update, exists := s.blockUpdates[key]
	if !exists {
		if len(s.blockUpdates) >= maxPendingBlockUpdates {
			s.logBlockUpdateDrop()
			return
		}
		// The ordinary network update was already flushed. Only the second,
		// neighbour-only update remains, followed by the actor pair.
		update = blockUpdateData{pos: v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}, runtimeID: networkBlockID(s.br, bell), flags: v2193.BlockUpdateNeighbours}
	} else {
		update.flags = v2193.BlockUpdateNetwork | v2193.BlockUpdateNeighbours
		update.repeatBlockUpdate = true
	}
	if len(update.actors)+2 > maxBlockUpdatesPerTick {
		s.logBlockUpdateDrop()
		return
	}
	update.nbt = nil
	update.actors = append(update.actors, data, data)
	s.blockUpdates[key] = update
}

func (s *Session) enqueueBellUseRejection(pos cube.Pos, bell block.Bell) {
	s.blockUpdatesMu.Lock()
	defer s.blockUpdatesMu.Unlock()
	if s.blockUpdates == nil {
		s.blockUpdates = make(map[blockUpdateKey]blockUpdateData, 64)
	}
	key := blockUpdateKey{pos: pos}
	update, exists := s.blockUpdates[key]
	if !exists && len(s.blockUpdates) >= maxPendingBlockUpdates {
		s.logBlockUpdateDrop()
		return
	}
	// A rejected use confirms the state only; it must not restart client
	// animation by sending another idle actor compound.
	// Preserve animation/effects from an earlier accepted ring that has
	// not yet passed the backlog; this rejection adds no new actor data.
	update.pos = v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	update.runtimeID = networkBlockID(s.br, bell)
	update.flags = v2193.BlockUpdateNetwork | v2193.BlockUpdateNeighbours
	update.actorOnly = false
	s.blockUpdates[key] = update
}

// enqueueBlockActorUpdate retains actor notifications without coalescing native
// duplicates. The containing block and its parent are flushed first. A later
// block replacement supersedes these notifications through enqueueBlockUpdate.
func (s *Session) enqueueBlockActorUpdate(pos, parent cube.Pos, data map[string]any) {
	s.blockUpdatesMu.Lock()
	defer s.blockUpdatesMu.Unlock()
	if s.blockUpdates == nil {
		s.blockUpdates = make(map[blockUpdateKey]blockUpdateData, 64)
	}
	key := blockUpdateKey{pos: pos}
	update, exists := s.blockUpdates[key]
	if !exists {
		if len(s.blockUpdates) >= maxPendingBlockUpdates {
			s.logBlockUpdateDrop()
			return
		}
		update = blockUpdateData{pos: v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}, actorOnly: true}
	}
	if len(update.actors) >= maxBlockUpdatesPerTick {
		s.logBlockUpdateDrop()
		return
	}
	update.after = &parent
	update.actors = append(update.actors, data)
	s.blockUpdates[key] = update
}

func (s *Session) logBlockUpdateDrop() {
	drops := s.blockUpdateDrops.Add(1)
	now := time.Now().UnixNano()
	last := s.blockUpdateLastLog.Load()
	if now-last > int64(time.Second) && s.blockUpdateLastLog.CompareAndSwap(last, now) {
		s.conf.Log.Warn("dropping block updates due to backlog", "dropped", drops)
	}
}
