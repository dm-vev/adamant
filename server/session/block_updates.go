package session

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	maxPendingBlockUpdates = 8192
	maxBlockUpdatesPerTick = 128
	maxPendingBellPackets  = 4 * maxBlockUpdatesPerTick
)

type blockUpdateKey struct {
	pos   cube.Pos
	layer uint8
}

type blockUpdateData struct {
	pos               protocol.BlockPos
	runtimeID         uint32
	layer             uint32
	flags             uint32
	repeatBlockUpdate bool
	nbt               map[string]any
	actorOnly         bool
	actors            []map[string]any
	afterPackets      []packet.Packet
	after             *cube.Pos
	// Support changes retain intermediate states separately from their deferred
	// actor snapshots. Removal seals that sequence ahead of the new block.
	// Circuit states wait until their callback has also emitted its sound.
	bellSupport      bool
	bellRemoval      bool
	publicationReady <-chan struct{}
	priorStates      []packet.Packet
	beforePackets    []packet.Packet
}

func (u blockUpdateData) statePackets() []packet.Packet {
	states := append([]packet.Packet(nil), u.priorStates...)
	if !u.actorOnly {
		states = append(states, &packet.UpdateBlock{Position: u.pos, NewBlockRuntimeID: u.runtimeID, Flags: u.flags, Layer: u.layer})
		if u.repeatBlockUpdate {
			states = append(states, &packet.UpdateBlock{Position: u.pos, NewBlockRuntimeID: u.runtimeID, Flags: packet.BlockUpdateNeighbours, Layer: u.layer})
		}
	}
	return states
}

func (u blockUpdateData) packets() []packet.Packet {
	packets := append([]packet.Packet(nil), u.beforePackets...)
	packets = append(packets, u.statePackets()...)
	if !u.actorOnly && u.nbt != nil {
		packets = append(packets, &packet.BlockActorData{Position: u.pos, NBTData: u.nbt})
	}
	for _, data := range u.actors {
		packets = append(packets, &packet.BlockActorData{Position: u.pos, NBTData: data})
	}
	return append(packets, u.afterPackets...)
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
		flags:     packet.BlockUpdateNetwork,
	}
	if _, bell := b.(block.Bell); bell {
		update.flags |= packet.BlockUpdateNeighbours
	}
	if attack, ok := b.(interface{ BlockInteractionUpdate() bool }); ok && attack.BlockInteractionUpdate() {
		update.flags |= packet.BlockUpdateNeighbours
		// DoorBlock publishes its lower half before its upper half, including
		// an upper permutation whose network open bit remains unchanged.
		top := false
		switch b := b.(type) {
		case block.WoodDoor:
			top = b.Top
		case block.CopperDoor:
			top = b.Top
		case block.IronDoor:
			top = b.Top
		}
		if top {
			below := pos.Side(cube.FaceDown)
			update.after = &below
		}
	}
	if deferred, ok := b.(interface{ BlockUpdateReady() <-chan struct{} }); ok {
		update.publicationReady = deferred.BlockUpdateReady()
	}
	if dependent, ok := b.(interface{ BlockUpdateAfter() (cube.Face, bool) }); ok {
		if face, enabled := dependent.BlockUpdateAfter(); enabled {
			parent := pos.Side(face)
			update.after = &parent
		}
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
	update.beforePackets = previous.beforePackets
	if update.publicationReady == nil {
		update.publicationReady = previous.publicationReady
	}
	if previous.bellRemoval {
		update.flags |= packet.BlockUpdateNeighbours
		update.beforePackets = previous.packets()
		update.afterPackets = nil
	} else if previous.bellSupport {
		update.bellSupport = true
		update.publicationReady = previous.publicationReady
		update.priorStates = previous.statePackets()
		if previous.nbt != nil && !previous.actorOnly {
			update.actors = append(update.actors, previous.nbt)
		}
		update.actors = append(update.actors, previous.actors...)
	}
	if len(update.beforePackets)+len(update.priorStates)+len(update.actors) > maxPendingBellPackets {
		// Retain the latest authoritative state while bounding per-position
		// history, just as the queue bounds the number of pending positions.
		update.beforePackets, update.priorStates, update.actors = nil, nil, nil
		s.logBlockUpdateDrop()
	}
	s.blockUpdates[key] = update
	s.blockUpdatesMu.Unlock()
}

func (s *Session) flushBlockUpdates() {
	s.blockUpdatesMu.Lock()
	defer s.blockUpdatesMu.Unlock()
	// Packet publication only enters the bounded, non-blocking outgoing queue.
	// Keep selection, partial sends and enqueue merging under the same lock:
	// detaching pending entries would lose support/removal markers if a newer
	// update arrived before an unsent entry was returned to the map.
	updates := s.blockUpdates
	if len(updates) == 0 {
		return
	}

	sent := 0
	visiting := make(map[blockUpdateKey]bool)
	var send func(blockUpdateKey)
	send = func(key blockUpdateKey) {
		update, ok := updates[key]
		if !ok || sent >= maxBlockUpdatesPerTick || visiting[key] {
			return
		}
		// A removal marker belongs to the foreground replacement that follows;
		// a concurrent flush must not consume it between those notifications.
		if update.bellRemoval {
			return
		}
		if update.publicationReady != nil {
			select {
			case <-update.publicationReady:
			default:
				return
			}
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
		start := sent
		packets := update.packets()
		for i, p := range packets {
			if _, blockUpdate := p.(*packet.UpdateBlock); blockUpdate {
				if sent >= maxBlockUpdatesPerTick {
					// Keep the exact remaining sequence across the batch boundary.
					update.beforePackets = packets[i:]
					update.priorStates, update.actors, update.afterPackets = nil, nil, nil
					update.actorOnly, update.repeatBlockUpdate, update.nbt = true, false, nil
					updates[key] = update
					return
				}
				sent++
			}
			s.writePacket(p)
		}
		if sent == start {
			sent++
		}
		delete(updates, key)
	}
	for key := range updates {
		send(key)
	}
}

// enqueueBellSupportUpdate defers a support actor until all states currently
// pending at this position have been sent. A nil bell seals those notifications
// before the foreground removal, including when the state already flushed.
func (s *Session) enqueueBellSupportUpdate(pos cube.Pos, bell *block.Bell, ready <-chan struct{}) {
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
	if bell == nil {
		update.bellRemoval = true
		update.publicationReady = nil
	} else {
		if len(update.beforePackets)+len(update.priorStates)+len(update.actors) >= maxPendingBellPackets {
			s.logBlockUpdateDrop()
			return
		}
		update.bellSupport = true
		update.publicationReady = ready
		update.actors = append(update.actors, clientActorNBT(bell.EncodeNBT(), pos))
	}
	s.blockUpdates[key] = update
}

// enqueuePacketAfterBlock retains an effect behind a pending state/actor update.
// An already flushed state needs only the effect; queue limits still apply.
func (s *Session) enqueuePacketAfterBlock(pos cube.Pos, p packet.Packet) {
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
		update = blockUpdateData{pos: protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}, runtimeID: networkBlockID(s.br, bell), flags: packet.BlockUpdateNeighbours}
	} else {
		update.flags = packet.BlockUpdateNetwork | packet.BlockUpdateNeighbours
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
	update.pos = protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	update.runtimeID = networkBlockID(s.br, bell)
	update.flags = packet.BlockUpdateNetwork | packet.BlockUpdateNeighbours
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
