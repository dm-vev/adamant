package session

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// TestBlockUpdatesAreEncodedOnce shows the changes of the blocks of a column
// to two sessions: both are given the same packets, an UpdateBlock for each
// change of a sub-chunk with few and one UpdateSubChunkBlocks for those of a
// sub-chunk with many, and each the block actor data of a chest.
func TestBlockUpdatesAreEncodedOnce(t *testing.T) {
	a, b := testSession("a"), testSession("b")
	for _, s := range []*Session{a, b} {
		s.br = world.DefaultBlockRegistry
	}
	updates := []world.BlockUpdate{
		{Pos: cube.Pos{1, 70, 1}, Block: block.Stone{}},
		{Pos: cube.Pos{2, 70, 1}, Block: block.NewChest()},
	}
	for x := range 9 {
		updates = append(updates, world.BlockUpdate{Pos: cube.Pos{x, 40, 3}, Block: block.Dirt{}})
	}
	updates = append(updates, world.BlockUpdate{Pos: cube.Pos{0, 40, 3}, Block: block.Water{Depth: 8, Still: true}, Layer: 1})
	u := &world.BlockUpdates{Updates: updates}
	a.ViewBlockUpdates(u)
	b.ViewBlockUpdates(u)
	drain := func(s *Session) (pks []proto.Packet) {
		for {
			select {
			case pk := <-s.packets:
				pks = append(pks, pk)
			default:
				return pks
			}
		}
	}
	pa, pb := drain(a), drain(b)
	if len(pa) != 4 || len(pb) != 4 {
		t.Fatalf("sessions given %d and %d packets, want 4 each", len(pa), len(pb))
	}
	for i := range 3 {
		if pa[i] != pb[i] {
			t.Fatalf("packet %d is not shared", i)
		}
	}
	single, ok := pa[0].(*sharedPacket).Packet.(*v2193.UpdateBlockPacket)
	if !ok || single.Position != [3]int32{1, 70, 1} || single.NewBlockRuntimeID != networkBlockID(world.DefaultBlockRegistry, block.Stone{}) {
		t.Fatalf("first packet %+v, want the stone", pa[0].(*sharedPacket).Packet)
	}
	var w wire.Writer
	pa[2].Encode(&w)
	var many v2193.UpdateSubChunkBlocksPacket
	many.Decode(wire.NewReader(w.B))
	if many.Position != [3]int32{0, 32, 0} || len(many.Blocks) != 9 || len(many.Extra) != 1 || many.Blocks[8].BlockPos != [3]int32{8, 40, 3} {
		t.Fatalf("sub-chunk packet at %v with %d blocks and %d extra", many.Position, len(many.Blocks), len(many.Extra))
	}
	if e := many.Extra[0]; e.SyncedUpdateEntityUniqueID != math.MaxUint64 || e.BlockRuntimeID != networkBlockID(world.DefaultBlockRegistry, block.Water{Depth: 8, Still: true}) {
		t.Fatalf("extra entry %+v, want the water syncing no entity", e)
	}
	for _, pks := range [][]proto.Packet{pa, pb} {
		actor, ok := pks[3].(*v2193.BlockActorDataPacket)
		if !ok || actor.Position != [3]int32{2, 70, 1} || actor.NBTData["id"] != "Chest" {
			t.Fatalf("last packet %+v, want the chest data", pks[3])
		}
	}
}
