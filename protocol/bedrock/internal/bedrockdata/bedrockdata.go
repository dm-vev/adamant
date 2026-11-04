// Package bedrockdata holds data of Bedrock Edition the Bedrock adapter reads,
// for each protocol it speaks: the vanilla item registry, by which the
// adapter defines the items to the client and gives items their runtime
// IDs, the vanilla blocks BDS defines to the client in StartGame, and the
// entity types and biomes BDS lists.
//
// The data of protocol N is in the directory vN, as BDS of that protocol
// sends it, with a PROVENANCE file that records which BDS it was taken from
// and how. A protocol after the first may hold blocks.json, the schema of
// pmmp's BedrockBlockUpgradeSchema that turns the block states of the
// protocol before it into its own.
package bedrockdata

import (
	"embed"
	"fmt"
	"strings"
	"sync"

	"github.com/dm-vev/adamant/net/nbt"
)

//go:embed v*/items.nbt v*/blocks.nbt v*/entities.txt v*/biomes.txt
var files embed.FS

// protocols are the protocols with data, oldest first.
var protocols = []int32{2193, 2225}

// Vanilla is the vanilla content BDS of one protocol defines to its clients.
type Vanilla struct {
	// Protocol is the number of the protocol.
	Protocol int32
	// ItemsNBT is the item registry of BDS, as NBT: the runtime ID, whether
	// the item is component based, its version and its data, by item name.
	ItemsNBT []byte
	// BlocksNBT is the definitions of the vanilla blocks BDS sends in
	// StartGame, as NBT: their properties by name.
	BlocksNBT []byte
	// Entities are the identifiers of the entity types BDS lists in
	// AvailableActorIdentifiers, in its order.
	Entities []string
	// Biomes are the identifiers of the biomes BDS defines in
	// BiomeDefinitionList, in its order.
	Biomes []string

	itemsOnce  sync.Once
	items      map[string]ItemEntry
	blocksOnce sync.Once
	blocks     map[string]map[string]any

	// ids holds the runtime IDs of the items, custom items included.
	ids itemIDs
}

// ItemEntry is an entry of the item registry of BDS.
type ItemEntry struct {
	RuntimeID      int32          `nbt:"runtime_id"`
	ComponentBased bool           `nbt:"component_based"`
	Version        int32          `nbt:"version"`
	Data           map[string]any `nbt:"data,omitempty"`
}

var (
	vanillaMu sync.Mutex
	vanilla   = map[int32]*Vanilla{}
)

// VanillaOf returns the vanilla content of a protocol, read the first
// time, and false for a protocol of which the adapter has no data.
func VanillaOf(protocol int32) (*Vanilla, bool) {
	vanillaMu.Lock()
	defer vanillaMu.Unlock()
	if v, ok := vanilla[protocol]; ok {
		return v, true
	}
	dir := fmt.Sprintf("v%d/", protocol)
	items, err := files.ReadFile(dir + "items.nbt")
	if err != nil {
		return nil, false
	}
	blocks, err := files.ReadFile(dir + "blocks.nbt")
	if err != nil {
		panic(fmt.Sprintf("bedrockdata: protocol %d has items but no blocks: %v", protocol, err))
	}
	v := &Vanilla{Protocol: protocol, ItemsNBT: items, BlocksNBT: blocks, Entities: lines(dir + "entities.txt"), Biomes: lines(dir + "biomes.txt")}
	v.ids.v = v
	vanilla[protocol] = v
	return v, true
}

// lines returns the lines of a file of the data, which every protocol
// has.
func lines(path string) []string {
	b, err := files.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("bedrockdata: %v", err))
	}
	return strings.Fields(string(b))
}

// Protocols returns the numbers of the protocols the adapter has data of,
// oldest first.
func Protocols() []int32 { return append([]int32(nil), protocols...) }

// Items returns the entries of the item registry, by item name. The map
// must not be changed.
func (v *Vanilla) Items() map[string]ItemEntry {
	v.itemsOnce.Do(func() {
		if err := nbt.Unmarshal(v.ItemsNBT, &v.items); err != nil {
			panic(fmt.Sprintf("bedrockdata: items of protocol %d: %v", v.Protocol, err))
		}
	})
	return v.items
}

// Blocks returns the properties of the vanilla blocks BDS defines in
// StartGame, by name. The map must not be changed.
func (v *Vanilla) Blocks() map[string]map[string]any {
	v.blocksOnce.Do(func() {
		if err := nbt.Unmarshal(v.BlocksNBT, &v.blocks); err != nil {
			panic(fmt.Sprintf("bedrockdata: blocks of protocol %d: %v", v.Protocol, err))
		}
	})
	return v.blocks
}

// v2193 is the data of protocol 2193.
var v2193 = func() *Vanilla {
	v, ok := VanillaOf(2193)
	if !ok {
		panic("bedrockdata: no data of protocol 2193")
	}
	return v
}()
