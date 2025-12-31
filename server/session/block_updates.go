package session

import (
	"maps"
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
}

func (s *Session) enqueueBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	update := blockUpdateData{
		pos:       protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])},
		runtimeID: world.BlockRuntimeID(b),
		layer:     uint32(layer),
	}
	if v, ok := b.(world.NBTer); ok {
		if nbtData := v.EncodeNBT(); nbtData != nil {
			// Clone to avoid mutating block-owned NBT maps that may be reused elsewhere.
			nbtCopy := maps.Clone(nbtData)
			nbtCopy["x"], nbtCopy["y"], nbtCopy["z"] = int32(pos.X()), int32(pos.Y()), int32(pos.Z())
			update.nbt = nbtCopy
		}
	}

	s.blockUpdatesMu.Lock()
	if s.blockUpdates == nil {
		s.blockUpdates = make(map[blockUpdateKey]blockUpdateData, 64)
	}
	if len(s.blockUpdates) >= maxPendingBlockUpdates {
		s.blockUpdatesMu.Unlock()
		s.logBlockUpdateDrop()
		return
	}
	s.blockUpdates[blockUpdateKey{pos: pos, layer: uint8(layer)}] = update
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
	var leftover map[blockUpdateKey]blockUpdateData
	for key, update := range updates {
		if sent < maxBlockUpdatesPerTick {
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
			sent++
			continue
		}
		if leftover == nil {
			leftover = make(map[blockUpdateKey]blockUpdateData, len(updates)-sent)
		}
		leftover[key] = update
	}
	if len(leftover) == 0 {
		return
	}

	s.blockUpdatesMu.Lock()
	if len(s.blockUpdates) == 0 {
		s.blockUpdates = leftover
	} else {
		for key, update := range leftover {
			s.blockUpdates[key] = update
		}
	}
	s.blockUpdatesMu.Unlock()
}

func (s *Session) logBlockUpdateDrop() {
	drops := s.blockUpdateDrops.Add(1)
	now := time.Now().UnixNano()
	last := s.blockUpdateLastLog.Load()
	if now-last > int64(time.Second) && s.blockUpdateLastLog.CompareAndSwap(last, now) {
		s.conf.Log.Warn("dropping block updates due to backlog", "dropped", drops)
	}
}
