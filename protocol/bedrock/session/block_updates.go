package session

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// subChunkBlocksAt is the number of changes of a sub-chunk shown together
// from which they go as one UpdateSubChunkBlocks rather than an UpdateBlock
// each, which costs less until then.
const subChunkBlocksAt = 8

// networkBlockID returns the ID clients of the model know a block by: its
// network block hash, as BDS sends block IDs with block network ID hashes
// enabled. A session has the tables of its protocol at hand, see
// Session.blockHash.
func networkBlockID(br world.BlockRegistry, b world.Block) uint32 {
	return tablesOf(nil, br).blockID(b)
}

// blockHash returns the ID the client of the session knows a block by, as
// networkBlockID, from the tables of its protocol and registry.
func (s *Session) blockHash(b world.Block) uint32 {
	return s.tables().blockID(b)
}

// sharedBlockUpdates is what the viewers of a world.BlockUpdates share of it:
// its packets, encoded once, and the changes that carry a block actor, whose
// data each viewer is sent as it sees it. The packets name blocks by the
// hashes of one table; a viewer of another makes its own.
type sharedBlockUpdates struct {
	blocks  *blockHashes
	packets []*sharedPacket
	actors  []world.BlockUpdate
}

// ViewBlockUpdates ...
func (s *Session) ViewBlockUpdates(u *world.BlockUpdates) {
	if s == Nop {
		return
	}
	for _, update := range u.Updates {
		s.blockUpdatesMu.Lock()
		_, pending := s.blockUpdates[blockUpdateKey{pos: update.Pos, layer: uint8(update.Layer)}]
		s.blockUpdatesMu.Unlock()
		if pending {
			s.enqueueBlockUpdate(update.Pos, update.Block, update.Layer)
		}
	}
	shared, _ := u.Shared.(*sharedBlockUpdates)
	if shared == nil || shared.blocks != s.tables().hashes() {
		mine := blockUpdatePackets(s.tables(), u.Updates)
		if shared == nil {
			u.Shared = mine
		}
		shared = mine
	}
	for _, pk := range shared.packets {
		s.writePacket(pk)
	}
	for _, a := range shared.actors {
		if data := s.blockActorUpdateNBT(a.Pos, a.Block); data != nil {
			s.writePacket(&mc.BlockActorDataPacket{Position: blockPos(a.Pos), NBTData: data})
		}
	}
}

// ViewBlockUpdate ...
func (s *Session) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	if s != Nop {
		s.enqueueBlockUpdate(pos, b, layer)
	}
}

// blockUpdatePackets returns the packets of the changes of blocks passed: an
// UpdateBlock for each change of a sub-chunk with few, an
// UpdateSubChunkBlocks for the changes of one with many, as BDS sends those
// of a fill.
func blockUpdatePackets(w *wireTables, updates []world.BlockUpdate) *sharedBlockUpdates {
	br, hashes := w.br, w.hashes()
	shared := &sharedBlockUpdates{blocks: hashes}
	counts := make(map[cube.Pos]int, 4)
	for _, u := range updates {
		counts[subChunkOf(u.Pos)]++
		if u.Layer == 0 {
			if _, ok := clientBlockActor(u.Block); ok {
				shared.actors = append(shared.actors, u)
			}
		}
	}
	var many map[cube.Pos]*mc.UpdateSubChunkBlocksPacket
	for _, u := range updates {
		sub := subChunkOf(u.Pos)
		if counts[sub] < subChunkBlocksAt {
			shared.packets = append(shared.packets, &sharedPacket{Packet: &mc.UpdateBlockPacket{
				Position:          blockPos(u.Pos),
				NewBlockRuntimeID: hashes.blockID(br, u.Block),
				Flags:             mc.BlockUpdateNetwork,
				Layer:             uint32(u.Layer),
			}})
			continue
		}
		pk := many[sub]
		if pk == nil {
			if many == nil {
				many = make(map[cube.Pos]*mc.UpdateSubChunkBlocksPacket)
			}
			pk = &mc.UpdateSubChunkBlocksPacket{Position: [3]int32{int32(sub[0] << 4), int32(sub[1] << 4), int32(sub[2] << 4)}}
			many[sub] = pk
			shared.packets = append(shared.packets, &sharedPacket{Packet: pk})
		}
		entry := mc.BlockChangeEntry{
			BlockPos:       blockPos(u.Pos),
			BlockRuntimeID: hashes.blockID(br, u.Block),
			Flags:          mc.BlockUpdateNetwork,
			// BDS sends -1: the change syncs no entity.
			SyncedUpdateEntityUniqueID: math.MaxUint64,
		}
		if u.Layer == 0 {
			pk.Blocks = append(pk.Blocks, entry)
		} else {
			pk.Extra = append(pk.Extra, entry)
		}
	}
	return shared
}

// blockActorUpdateNBT returns the block actor data a client is sent with a
// change to a block at pos, or nil for a block without one.
func (s *Session) blockActorUpdateNBT(pos cube.Pos, b world.Block) map[string]any {
	v, ok := clientBlockActor(b)
	if !ok {
		return nil
	}
	if updater, ok := v.(world.UpdateNBTer); ok {
		return s.clientUpdateNBT(updater.UpdateNBT())
	}
	if data := v.EncodeNBT(); data["id"] != nil {
		return clientActorNBT(data, pos)
	}
	return nil
}

// subChunkOf returns the position of the sub-chunk holding a block, in
// sub-chunks.
func subChunkOf(pos cube.Pos) cube.Pos {
	return cube.Pos{pos[0] >> 4, pos[1] >> 4, pos[2] >> 4}
}

// blockPos returns a block position as the protocol carries it.
func blockPos(pos cube.Pos) [3]int32 {
	return [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])}
}
