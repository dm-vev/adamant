package chunk

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"slices"

	"github.com/df-mc/worldupgrader/blockupgrader"
)

// schema0361 is pmmp's BedrockBlockUpgradeSchema
// nbt_upgrade_schema/0361_1.26.50_to_1.26.60.json (CC0 1.0), commit
// 755ff72f27dd7849caaff1c3be06a93e86525f29 of 2026-10-28, sha256
// 3fa17b08bf9cd8c170b7d665d49c9bb0c5bd057e8f51e7834b35191c021efcdc: the
// connection states 1.26.60 added to redstone wire, fire and chorus
// plants, with the values of states saved before them, none and 0. It is
// the same file as protocol/bedrock/internal/bedrockdata/v2225/blocks.json.
// df-mc/worldupgrader, whose newest release, v1.0.22, predates it, does not
// hold it yet.
//
//go:embed schemas/0361_1.26.50_to_1.26.60.json
var schema0361 []byte

// derivedProperties are the properties the upgrade adds that blocks derive
// from their neighbours, by block name: the connections schema 0361 adds to
// redstone wire, fire and chorus plants, with the value of a block connected
// to nothing. A block whose saved state lacked them is recorded as missing
// them, so that its MissingStateReconstructor derives them once its
// neighbours are loaded. BDS 1.26.61.1 does the same with a world saved by
// 1.26.52.3: it saves the blocks of such a world connected to their
// neighbours once it loaded them, untouched, and keeps the connections of a
// world saved by itself, unconnected wire included, as they are.
var derivedProperties = map[string][]string{}

// init registers the schemas of the block states of the game newer than
// those of df-mc/worldupgrader, after its own, so that states saved by
// 1.26.50 load as states of 1.26.60.
func init() {
	if err := blockupgrader.RegisterSchema(bytes.NewReader(schema0361)); err != nil {
		panic("chunk: schema 0361 of the block states: " + err.Error())
	}
	var schema struct {
		AddedProperties map[string]map[string]json.RawMessage `json:"addedProperties"`
	}
	if err := json.Unmarshal(schema0361, &schema); err != nil {
		panic("chunk: schema 0361 of the block states: " + err.Error())
	}
	for name, props := range schema.AddedProperties {
		for prop := range props {
			derivedProperties[name] = append(derivedProperties[name], prop)
		}
		slices.Sort(derivedProperties[name])
	}
}

// lackedDerivedProperties returns the derived properties a saved state of a
// block lacks, before the upgrade fills them in.
func lackedDerivedProperties(name string, state map[string]any) []string {
	var lacked []string
	for _, prop := range derivedProperties[name] {
		if _, saved := state[prop]; !saved {
			lacked = append(lacked, prop)
		}
	}
	return lacked
}
