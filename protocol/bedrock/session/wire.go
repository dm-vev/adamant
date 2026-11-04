package session

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// wireTables are the tables a session encodes the content of the game
// with for the clients of one protocol and the blocks of one registry: the
// network hashes of the blocks, the runtime IDs of the items, the biomes
// and entities the clients know, and the packets made of them that are the
// same for each of those clients, the creative inventory and the recipes.
// The tables of a protocol and a registry are built once, by tablesOf, and
// shared by their sessions.
//
// The game holds the blocks of 2225, whose clients name the content of the
// game as the game does: their tables are those of the game. The tables of
// a protocol that names some of it otherwise, as 2193 names the blocks
// 1.26.60 changed, map the game's content into that protocol's here, and
// the rest of the session encodes through them.
type wireTables struct {
	proto *versions.Protocol
	br    world.BlockRegistry
	// vanilla is the vanilla content of BDS of the protocol.
	vanilla *bedrockdata.Vanilla
	// chunkBR is br as the registry of a chunk, to tell the chunks of br.
	chunkBR chunk.BlockRegistry
	// blocks are the network hashes of the runtime IDs of br, built by
	// hashes once br is finalized. slot is set, before blocks, to the key
	// the caches of columns keep the payloads of the tables under: nil for
	// the default protocol, whose payloads a column keeps in its own cache,
	// else that of the protocol.
	blocks atomic.Pointer[blockHashes]
	slot   atomic.Pointer[world.SlotKey]

	creativeMu sync.Mutex
	creative   *creativeData
	recipesMu  sync.Mutex
	recipes    *recipeData
}

// tablesKey names the tables of a protocol and a registry.
type tablesKey struct {
	proto *versions.Protocol
	br    world.BlockRegistry
}

// allTables holds the wireTables tablesOf built, by tablesKey.
var allTables sync.Map

// tablesOf returns the tables of the clients of a protocol, the default of
// the build if p is nil, and the blocks of a finalized registry, built the
// first time. The registry is a key, as it is of hashesOf.
func tablesOf(p *versions.Protocol, br world.BlockRegistry) *wireTables {
	if p == nil {
		p = versions.Default()
	}
	key := tablesKey{proto: p, br: br}
	if t, ok := allTables.Load(key); ok {
		return t.(*wireTables)
	}
	t, _ := allTables.LoadOrStore(key, newTables(p, br))
	return t.(*wireTables)
}

func newTables(p *versions.Protocol, br world.BlockRegistry) *wireTables {
	return &wireTables{proto: p, br: br, chunkBR: br, vanilla: vanillaOf(p)}
}

// vanillaOf returns the vanilla content of a protocol. Every protocol the
// adapter was built with has its own; one it was not, as a test makes,
// takes that of the default of the build.
func vanillaOf(p *versions.Protocol) *bedrockdata.Vanilla {
	if v, ok := bedrockdata.VanillaOf(p.Number); ok {
		return v
	}
	if _, known := versions.ByNumber(p.Number); known {
		panic(fmt.Sprintf("session: no vanilla data of protocol %d", p.Number))
	}
	v, _ := bedrockdata.VanillaOf(versions.Default().Number)
	return v
}

// hashes returns the network hashes of the runtime IDs of the registry of
// the tables, built the first time.
func (w *wireTables) hashes() *blockHashes {
	if h := w.blocks.Load(); h != nil {
		return h
	}
	h := blockTable(w.proto, w.br)
	// The tables of the default protocol keep their payloads in a column's
	// own cache, those of every other protocol in the cache of its key, so
	// that a server of the default config keeps one cache a column, as it
	// did before it served more protocols.
	if w.proto != versions.Default() {
		w.slot.Store(slotOf(w.proto))
	}
	w.blocks.CompareAndSwap(nil, h)
	return w.blocks.Load()
}

// slots holds the keys of the caches of columns of the protocols other
// than the default, by protocol.
var slots sync.Map

// slotOf returns the key of the caches of columns of the tables of a
// protocol other than the default: one for each protocol, named
// "bedrock/blocks/" and its number.
func slotOf(p *versions.Protocol) *world.SlotKey {
	if k, ok := slots.Load(p); ok {
		return k.(*world.SlotKey)
	}
	k, _ := slots.LoadOrStore(p, world.NewSlotKey(fmt.Sprintf("bedrock/blocks/%d", p.Number)))
	return k.(*world.SlotKey)
}

// blockTable returns the network hashes the clients of a protocol know the
// runtime IDs of a finalized registry by: the hashes of the registry
// itself for a protocol that names blocks as the game does, else those of
// each state of the game as the protocol names it (bedrockdata.MapState).
func blockTable(p *versions.Protocol, br chunk.BlockRegistry) *blockHashes {
	if p == nil || bedrockdata.NamesBlocksAsTheGame(p.Number) {
		return hashesOf(br)
	}
	key := protocolTableKey{p.Number, br}
	if h, ok := protocolTables.Load(key); ok {
		return h.(*blockHashes)
	}
	h, _ := protocolTables.LoadOrStore(key, buildProtocolHashes(p.Number, br))
	return h.(*blockHashes)
}

// protocolTableKey names the hashes of a protocol and a registry.
type protocolTableKey struct {
	protocol int32
	br       chunk.BlockRegistry
}

// protocolTables holds the hashes blockTable built for protocols that name
// blocks otherwise than the game, by protocolTableKey.
var protocolTables sync.Map

// buildProtocolHashes hashes every runtime ID of a finalized registry as
// the clients of a protocol name its state: the hashes of the registry,
// but for the states the protocol names otherwise, as 2193 names redstone
// wire, fire and chorus plants without their connections and disc presses
// as stonecutters. A hash the clients send is read back as the state of the
// game the protocol's state is (bedrockdata.GameState): the wire of 2193 as
// wire connected to no side, a stonecutter as a stonecutter. A state of the
// game the protocol names otherwise has no runtime ID by its own hash, as a
// hash no client should send has none.
func buildProtocolHashes(p int32, br chunk.BlockRegistry) *blockHashes {
	game := hashesOf(br)
	h := &blockHashes{hash: slices.Clone(game.hash), rid: maps.Clone(game.rid)}
	scratch := make([]byte, 0, 0xff)
	var named map[uint32]uint32
	for i := range br.BlockCount() {
		rid := uint32(i)
		name, properties, _ := br.RuntimeIDToState(rid)
		mapped, props := bedrockdata.MapState(p, name, properties)
		if mapped == name && maps.EqualFunc(props, properties, sameValue) {
			continue
		}
		var netHash uint32
		netHash, scratch = networkBlockHash(mapped, props, scratch)
		if h.rid[game.hash[rid]] == rid {
			delete(h.rid, game.hash[rid])
		}
		h.hash[rid] = netHash
		back, backProps := bedrockdata.GameState(p, mapped, props)
		if back != name || !maps.EqualFunc(backProps, properties, sameValue) {
			continue
		}
		if other, ok := h.rid[netHash]; ok && other != rid {
			panic(fmt.Sprintf("network block hash collision of protocol %d for runtime IDs %d and %d", p, rid, other))
		}
		if named == nil {
			named = make(map[uint32]uint32)
		}
		named[netHash] = rid
	}
	maps.Copy(h.rid, named)
	return h
}

// sameValue reports whether two values of block properties are the same.
func sameValue(a, b any) bool { return a == b }

// blocksOf returns the network hashes the clients of the tables know the
// blocks of a registry by, which may be another than that of the tables, as
// that of a chunk of another world.
func (w *wireTables) blocksOf(br chunk.BlockRegistry) *blockHashes {
	if br == w.chunkBR {
		return w.hashes()
	}
	return blockTable(w.proto, br)
}

// blockID returns the network hash the clients of the tables know a block of
// their registry by, 0 if it has none.
func (w *wireTables) blockID(b world.Block) uint32 {
	return w.hashes().blockID(w.br, b)
}

// itemRuntimeID returns the runtime ID and metadata the clients of the
// tables know an item by, and false for an item they do not know.
func (w *wireTables) itemRuntimeID(i world.Item) (rid int32, meta int16, ok bool) {
	return w.vanilla.ItemRuntimeID(i)
}

// itemByRuntimeID returns the item the clients of the tables name by a
// runtime ID and metadata.
func (w *wireTables) itemByRuntimeID(rid int32, meta int16) (world.Item, bool) {
	return w.vanilla.ItemByRuntimeID(rid, meta)
}

// biomes returns the definitions of the biomes registered the clients of
// the tables know, and the strings their names and tags index.
func (w *wireTables) biomes() ([]mc.BiomeDefinition, []string) {
	return biomeDefinitions(w.knows(w.vanilla.Biomes))
}

// entities returns the identifiers of the entity types of a list that the
// clients of the tables know, in the order of the list: every one but the
// vanilla types BDS of their protocol does not list.
func (w *wireTables) entities(ids []string) []string {
	known := w.knows(w.vanilla.Entities)
	return slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return !known(id) })
}

// knows returns whether the clients of the tables know a vanilla
// identifier, one of the namespace minecraft, by the list of those BDS of
// their protocol knows. Every other identifier, of content of the server's
// own, they are told of.
func (w *wireTables) knows(vanilla []string) func(id string) bool {
	return func(id string) bool {
		return !strings.HasPrefix(id, "minecraft:") || slices.Contains(vanilla, id)
	}
}

// tables returns the tables of the session, those of its protocol and
// registry for a session New did not make.
func (s *Session) tables() *wireTables {
	if s.wire != nil {
		return s.wire
	}
	br := s.br
	if br == nil {
		br = world.DefaultBlockRegistry
	}
	return tablesOf(s.conf.Protocol, br)
}

// protocolOf returns the protocol a connection speaks, the default of the
// build for a connection that does not tell.
func protocolOf(conn Conn) *versions.Protocol {
	if c, ok := conn.(interface{ Protocol() *versions.Protocol }); ok {
		if p := c.Protocol(); p != nil {
			return p
		}
	}
	return versions.Default()
}
