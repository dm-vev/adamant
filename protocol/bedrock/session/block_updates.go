package session

import (
	"math"

	"github.com/dm-vev/adamant/protocol/bedrock/netconn"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
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
	packets []*netconn.Shared
	actors  []world.BlockUpdate
}

// ViewBlockUpdates ...
func (s *Session) ViewBlockUpdates(u *world.BlockUpdates) {
	if s == nop {
		return
	}
	if len(u.Updates) > 0 {
		s.blockUpdatesMu.Lock()
		if len(s.blockUpdates) > 0 {
			var reenqueue []world.BlockUpdate
			for _, update := range u.Updates {
				if _, pending := s.blockUpdates[blockUpdateKey{pos: update.Pos, layer: uint8(update.Layer)}]; pending {
					reenqueue = append(reenqueue, update)
				}
			}
			s.blockUpdatesMu.Unlock()
			for _, update := range reenqueue {
				s.enqueueBlockUpdate(update.Pos, update.Block, update.Layer)
			}
		} else {
			s.blockUpdatesMu.Unlock()
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
	if s != nop {
		s.enqueueBlockUpdate(pos, b, layer)
	}
}

type subChunkUpdateGroup struct {
	pos   cube.Pos
	total int
	main  int
	extra int
	pk    *mc.UpdateSubChunkBlocksPacket
}

// blockUpdatePackets returns the packets of the changes of blocks passed: an
// UpdateBlock for each change of a sub-chunk with few, an
// UpdateSubChunkBlocks for the changes of one with many, as BDS sends those
// of a fill.
func blockUpdatePackets(w *wireTables, updates []world.BlockUpdate) *sharedBlockUpdates {
	br, hashes := w.br, w.hashes()
	if len(updates) == 0 {
		return &sharedBlockUpdates{blocks: hashes}
	}
	if len(updates) < subChunkBlocksAt {
		pks := make([]mc.UpdateBlockPacket, len(updates))
		sharedVals := make([]netconn.Shared, len(updates))
		ptrs := make([]*netconn.Shared, len(updates))
		shared := &sharedBlockUpdates{blocks: hashes, packets: ptrs}
		for i, u := range updates {
			if u.Layer == 0 {
				if _, ok := clientBlockActor(u.Block); ok {
					shared.actors = append(shared.actors, u)
				}
			}
			pks[i] = mc.UpdateBlockPacket{
				Position:          blockPos(u.Pos),
				NewBlockRuntimeID: hashes.blockID(br, u.Block),
				Flags:             mc.BlockUpdateNetwork,
				Layer:             uint32(u.Layer),
			}
			sharedVals[i].Packet = &pks[i]
			ptrs[i] = &sharedVals[i]
		}
		return shared
	}
	return blockUpdatePacketsGrouped(br, hashes, updates)
}

func findSubChunkGroup(stack []subChunkUpdateGroup, overflow map[cube.Pos]*subChunkUpdateGroup, sub cube.Pos) *subChunkUpdateGroup {
	for i := range stack {
		if stack[i].pos == sub {
			return &stack[i]
		}
	}
	if overflow != nil {
		return overflow[sub]
	}
	return nil
}

func blockUpdatePacketsGrouped(br world.BlockRegistry, hashes *blockHashes, updates []world.BlockUpdate) *sharedBlockUpdates {
	shared := &sharedBlockUpdates{blocks: hashes}
	var stackGroups [8]subChunkUpdateGroup
	nGroups := 0
	var overflow map[cube.Pos]*subChunkUpdateGroup

	for _, u := range updates {
		sub := subChunkOf(u.Pos)
		g := findSubChunkGroup(stackGroups[:nGroups], overflow, sub)
		if g == nil {
			if nGroups < len(stackGroups) {
				stackGroups[nGroups] = subChunkUpdateGroup{pos: sub}
				g = &stackGroups[nGroups]
				nGroups++
			} else {
				if overflow == nil {
					overflow = make(map[cube.Pos]*subChunkUpdateGroup, 8)
				}
				g = &subChunkUpdateGroup{pos: sub}
				overflow[sub] = g
			}
		}
		g.total++
		if u.Layer == 0 {
			g.main++
			if _, ok := clientBlockActor(u.Block); ok {
				shared.actors = append(shared.actors, u)
			}
		} else {
			g.extra++
		}
	}

	totalPackets, singleCount := countGroupedBlockPackets(stackGroups[:nGroups], overflow)
	shared.packets = make([]*netconn.Shared, 0, totalPackets)
	sharedVals := make([]netconn.Shared, totalPackets)
	var singlePks []mc.UpdateBlockPacket
	if singleCount > 0 {
		singlePks = make([]mc.UpdateBlockPacket, singleCount)
	}
	sharedIdx, singleIdx := 0, 0

	for _, u := range updates {
		sub := subChunkOf(u.Pos)
		g := findSubChunkGroup(stackGroups[:nGroups], overflow, sub)
		if g.total < subChunkBlocksAt {
			singlePks[singleIdx] = mc.UpdateBlockPacket{
				Position:          blockPos(u.Pos),
				NewBlockRuntimeID: hashes.blockID(br, u.Block),
				Flags:             mc.BlockUpdateNetwork,
				Layer:             uint32(u.Layer),
			}
			sharedVals[sharedIdx].Packet = &singlePks[singleIdx]
			shared.packets = append(shared.packets, &sharedVals[sharedIdx])
			singleIdx++
			sharedIdx++
			continue
		}
		pk := g.pk
		if pk == nil {
			pk = &mc.UpdateSubChunkBlocksPacket{
				Position: [3]int32{int32(sub[0] << 4), int32(sub[1] << 4), int32(sub[2] << 4)},
			}
			if g.main > 0 {
				pk.Blocks = make([]mc.BlockChangeEntry, 0, g.main)
			}
			if g.extra > 0 {
				pk.Extra = make([]mc.BlockChangeEntry, 0, g.extra)
			}
			g.pk = pk
			sharedVals[sharedIdx].Packet = pk
			shared.packets = append(shared.packets, &sharedVals[sharedIdx])
			sharedIdx++
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

func countGroupedBlockPackets(stack []subChunkUpdateGroup, overflow map[cube.Pos]*subChunkUpdateGroup) (total, singles int) {
	for i := range stack {
		if stack[i].total >= subChunkBlocksAt {
			total++
		} else {
			total += stack[i].total
			singles += stack[i].total
		}
	}
	for _, g := range overflow {
		if g.total >= subChunkBlocksAt {
			total++
		} else {
			total += g.total
			singles += g.total
		}
	}
	return total, singles
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
