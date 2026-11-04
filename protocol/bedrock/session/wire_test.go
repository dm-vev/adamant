package session

import (
	"maps"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// otherProtocol is a protocol the adapter does not speak, which tests give
// tables of their own.
var otherProtocol = &versions.Protocol{Codec: codec.Codec{Number: 2300, Version: "1.26.90", New: mc.New, FromClient: mc.FromClient}, Names: []string{"1.26.90"}}

// TestTablesArePerProtocolAndRegistry checks the tables of a protocol and a
// registry are built once and those of the default protocol, 2193, are its
// own: its item IDs, and block hashes of its own, as the game holds the
// blocks of 2225. The model and a protocol without data name the blocks as
// the game does.
func TestTablesArePerProtocolAndRegistry(t *testing.T) {
	br := world.DefaultBlockRegistry
	model := tablesOf(nil, br)
	if model != tablesOf(versions.Default(), br) || model.proto != versions.Default() {
		t.Fatal("nil does not name the tables of the default protocol")
	}
	other := tablesOf(otherProtocol, br)
	if other == model || other != tablesOf(otherProtocol, br) {
		t.Fatal("the tables of another protocol are not its own, built once")
	}
	if tablesOf(versions.Model(), br).hashes() != hashesOf(br) || other.hashes() != hashesOf(br) {
		t.Fatal("a protocol does not name the blocks as the game does")
	}
	if model.hashes() == hashesOf(br) {
		t.Fatal("2193 names the blocks as the game does")
	}
	if a, b := model.recipeData(), other.recipeData(); a == b || a.packet == b.packet {
		t.Fatal("two protocols share the recipes encoded for one")
	}
	if model.creativePacket() == other.creativePacket() || model.creativePacket() != model.creativePacket() {
		t.Fatal("the creative inventory is not encoded once per protocol")
	}
	rid, meta, ok := model.itemRuntimeID(block.Stone{})
	if it, found := model.itemByRuntimeID(rid, meta); !ok || !found || it != (block.Stone{}) {
		t.Fatalf("stone is %d:%d (%v), back %v (%v)", rid, meta, ok, it, found)
	}
}

// TestProtocolOfConnection takes the protocol a connection tells, and the
// default from one that does not.
func TestProtocolOfConnection(t *testing.T) {
	if p := protocolOf(benchmarkConn{}); p != versions.Default() {
		t.Fatalf("protocol %v of a connection that does not tell", p)
	}
	if p := protocolOf(protocolConn{p: otherProtocol}); p != otherProtocol {
		t.Fatalf("protocol %v, want %v", p, otherProtocol)
	}
	s := testSession("p")
	s.conf.Protocol = otherProtocol
	if s.tables().proto != otherProtocol {
		t.Fatal("a session not made by New takes the tables of another protocol")
	}
}

type protocolConn struct {
	benchmarkConn
	p *versions.Protocol
}

func (c protocolConn) Protocol() *versions.Protocol { return c.p }

// copiedTables returns tables of another protocol whose block hashes are
// a table of their own, equal to the game's: as tables that name blocks
// otherwise are, which no protocol does yet.
func copiedTables(br world.BlockRegistry) *wireTables {
	h := hashesOf(br)
	w := &wireTables{proto: otherProtocol, br: br, chunkBR: br}
	w.slot.Store(slotOf(otherProtocol))
	w.blocks.Store(&blockHashes{hash: slices.Clone(h.hash), rid: maps.Clone(h.rid)})
	return w
}

// TestBlockUpdatesOfAnotherTable checks a session whose blocks are named by
// another table than the session that encoded the changes of a column
// encodes its own, and leaves the shared ones to the sessions of theirs.
func TestBlockUpdatesOfAnotherTable(t *testing.T) {
	t.Parallel()
	a, b := testSession("a"), testSession("b")
	a.br, b.br = world.DefaultBlockRegistry, world.DefaultBlockRegistry
	b.wire = copiedTables(b.br)
	u := &world.BlockUpdates{Updates: []world.BlockUpdate{{Pos: cube.Pos{1, 70, 1}, Block: block.Stone{}}}}
	a.ViewBlockUpdates(u)
	b.ViewBlockUpdates(u)
	shared := u.Shared.(*sharedBlockUpdates)
	pa, pb := a.testPacket(), b.testPacket()
	if pa != proto.Packet(shared.packets[0]) || pb == pa || shared.blocks != a.tables().hashes() {
		t.Fatalf("the sessions of two tables share %v and %v", pa, pb)
	}
}

// TestColumnCachesKeepEachClass checks the payloads of the tables of a
// protocol other than the default are kept in the cache of a column for the
// key of the protocol, and those of the default protocol's tables in the
// column's own: each encodes a sub-chunk once, neither takes the other's.
func TestColumnCachesKeepEachClass(t *testing.T) {
	stone, _ := chunk.StateToRuntimeID("minecraft:stone", nil)
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
	c.SetBlock(0, 0, 0, 0, stone)
	col := &world.Column{Chunk: c}
	other, model := copiedTables(world.DefaultBlockRegistry), tablesOf(nil, world.DefaultBlockRegistry)
	key := slotOf(otherProtocol)
	if model.columnCache(col) != col.Cache(nil) || other.columnCache(col) != col.Cache(key) || slotOf(otherProtocol) != key {
		t.Fatal("the tables do not keep their payloads in the cache of their class")
	}
	got := other.fullChunkPayload(col)
	if _, ok := col.CachedNetworkSubChunkPayload(0); ok {
		t.Fatal("another table filled the column's own cache of a sub-chunk")
	}
	if _, ok := col.CachedFullChunkPayload(); ok {
		t.Fatal("another table filled the column's own cache of the chunk")
	}
	if cached, ok := col.Cache(slotOf(otherProtocol)).CachedFullChunkPayload(); !ok || &cached[0] != &got[0] {
		t.Fatal("another table did not fill the cache of its class")
	}
	want := model.fullChunkPayload(col)
	if cached, ok := col.CachedFullChunkPayload(); !ok || &cached[0] != &want[0] {
		t.Fatal("the game's tables did not fill the cache of the chunk")
	}
	if &got[0] == &want[0] || string(got) != string(want) {
		t.Fatal("the two classes share a payload, or name the same blocks otherwise")
	}
	if again := other.fullChunkPayload(col); &again[0] != &got[0] {
		t.Fatal("another table encoded the chunk again")
	}
}

// readAs decodes the fields of a packet a client of a protocol sent as
// netconn reads it, as the packet of the model it carries: as the model if
// the layout of its ID is the model's, else with the codecs of the
// protocol, then converted.
func readAs(t *testing.T, p *versions.Protocol, id uint32, fields []byte) proto.Packet {
	t.Helper()
	codec := p
	if p.SameLayout(id) {
		codec = versions.Model()
	}
	pk := codec.New(id)
	r := wire.NewReader(fields)
	pk.Decode(r)
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("packet %d does not decode: %v, %d bytes left", id, r.Err(), r.Len())
	}
	if codec == p && p.Converts() {
		pks := p.In(pk)
		if len(pks) != 1 {
			t.Fatalf("packet %d of protocol %d is %d packets of the model", id, p.Number, len(pks))
		}
		pk = pks[0]
	}
	return pk
}
