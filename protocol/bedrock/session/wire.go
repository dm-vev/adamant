package session

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// wireTables are the tables a session encodes the content of the game
// with for the clients of one protocol and the blocks of one registry: the
// network hashes of the blocks, the runtime IDs of the items, the biomes
// and entities the clients know, and the packets made of them that are the
// same for each of those clients, the creative inventory and the recipes.
// The tables of a protocol and a registry are built once, by tablesOf, and
// shared by their sessions.
//
// Every protocol the adapter speaks names the content of the game as the
// game does, today: the tables of each are those of the game. The tables of
// a protocol that names some of it otherwise map the game's content into
// that protocol's here, and the rest of the session encodes through them.
type wireTables struct {
	proto *versions.Protocol
	br    world.BlockRegistry
	// vanilla is the vanilla content of BDS of the protocol.
	vanilla *bedrockdata.Vanilla
	// chunkBR is br as the registry of a chunk, to tell the chunks of br.
	chunkBR chunk.BlockRegistry
	// blocks are the network hashes of the runtime IDs of br, built by
	// hashes once br is finalized. game reports, before blocks is set, that
	// they are the hashes of the game's own states, hashesOf(br), which the
	// caches of columns hold.
	blocks atomic.Pointer[blockHashes]
	game   atomic.Bool

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

// tablesOf returns the tables of the clients of a protocol, the model if p
// is nil, and the blocks of a finalized registry, built the first time. The
// registry is a key, as it is of hashesOf.
func tablesOf(p *versions.Protocol, br world.BlockRegistry) *wireTables {
	if p == nil {
		p = versions.Model()
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
// takes that of the model.
func vanillaOf(p *versions.Protocol) *bedrockdata.Vanilla {
	if v, ok := bedrockdata.VanillaOf(p.Number); ok {
		return v
	}
	if _, known := versions.ByNumber(p.Number); known {
		panic(fmt.Sprintf("session: no vanilla data of protocol %d", p.Number))
	}
	v, _ := bedrockdata.VanillaOf(versions.Model().Number)
	return v
}

// hashes returns the network hashes of the runtime IDs of the registry of
// the tables, built the first time.
func (w *wireTables) hashes() *blockHashes {
	if h := w.blocks.Load(); h != nil {
		return h
	}
	h := blockTable(w.proto, w.br)
	w.game.Store(h == hashesOf(w.br))
	w.blocks.CompareAndSwap(nil, h)
	return w.blocks.Load()
}

// blockTable returns the network hashes the clients of a protocol know the
// runtime IDs of a finalized registry by. Every protocol names the blocks
// of the game as the game does, so these are the hashes of the registry
// itself.
func blockTable(_ *versions.Protocol, br chunk.BlockRegistry) *blockHashes {
	return hashesOf(br)
}

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
func (w *wireTables) biomes() ([]v2193.BiomeDefinition, []string) {
	return biomeDefinitions()
}

// entities returns the identifiers of the entity types of a list that the
// clients of the tables know, in the order of the list.
func (w *wireTables) entities(ids []string) []string {
	return ids
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

// protocolOf returns the protocol a connection speaks, the model for a
// connection that does not tell.
func protocolOf(conn Conn) *versions.Protocol {
	if c, ok := conn.(interface{ Protocol() *versions.Protocol }); ok {
		if p := c.Protocol(); p != nil {
			return p
		}
	}
	return versions.Model()
}
