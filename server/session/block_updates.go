package session

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
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
	pos       protocol.BlockPos
	runtimeID uint32
	layer     uint32
	nbt       map[string]any
	actorOnly bool
	actors    []map[string]any
	after     *cube.Pos
}

// networkBlockID returns the ID clients know a block by: its network block
// hash, as BDS sends block IDs with block network ID hashes enabled.
func networkBlockID(br world.BlockRegistry, b world.Block) uint32 {
	hash, _ := br.RuntimeIDToHash(br.BlockRuntimeID(b))
	return hash
}

func (s *Session) enqueueBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	update := blockUpdateData{
		pos:       protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		runtimeID: networkBlockID(s.br, b),
		layer:     uint32(layer),
	}
	if v, ok := clientBlockActor(b); ok {
		if updater, ok := v.(world.UpdateNBTer); ok {
			update.nbt = s.clientUpdateNBT(updater.UpdateNBT())
		} else if nbtData := v.EncodeNBT(); nbtData["id"] != nil {
			update.nbt = clientActorNBT(nbtData, pos)
		}
	}

	s.blockUpdatesMu.Lock()
	if s.blockUpdates == nil {
		s.blockUpdates = make(map[blockUpdateKey]blockUpdateData, 64)
	}
	key := blockUpdateKey{pos: pos, layer: uint8(layer)}
	if _, exists := s.blockUpdates[key]; !exists && len(s.blockUpdates) >= maxPendingBlockUpdates {
		s.blockUpdatesMu.Unlock()
		s.logBlockUpdateDrop()
		return
	}
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
			s.writePacket(&packet.UpdateBlock{
				Position:          update.pos,
				NewBlockRuntimeID: update.runtimeID,
				Flags:             packet.BlockUpdateNetwork,
				Layer:             update.layer,
			})
			if update.nbt != nil {
				s.writePacket(&packet.BlockActorData{
					Position: update.pos,
					NBTData:  update.nbt,
				})
			}
		}
		for _, data := range update.actors {
			s.writePacket(&packet.BlockActorData{Position: update.pos, NBTData: data})
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
		update = blockUpdateData{pos: protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}, actorOnly: true}
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
