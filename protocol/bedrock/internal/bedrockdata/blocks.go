package bedrockdata

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"
)

// GameBlocks is the protocol whose block states the game holds: those of
// server/world's block_states.nbt, of 1.26.60. Its clients name every
// block as the game does; those of 2193 name the blocks 1.26.60 changed
// through the step of 2225 back.
const GameBlocks = 2225

// BlockStep turns the block states of the protocol before a protocol into
// its own, as a schema of pmmp's BedrockBlockUpgradeSchema does: the
// properties it adds to blocks, with their values, and the states of the
// protocol before that the protocol lacks, for the step back.
type BlockStep struct {
	// Added holds the properties the step adds to blocks, with the values
	// states of the protocol before take, by block name.
	Added map[string]map[string]any
	// Fallbacks holds, by block name, the state of the protocol before that
	// stands in for each state of a block the protocol before lacks: one
	// that looks alike, chosen by hand.
	Fallbacks map[string]BlockState
}

// BlockState is a block state: a name and its properties.
type BlockState struct {
	Name   string         `json:"name"`
	States map[string]any `json:"states"`
}

// Up returns a state of the protocol before as a state of the step's
// protocol.
func (s *BlockStep) Up(name string, props map[string]any) (string, map[string]any) {
	add, ok := s.Added[name]
	if !ok {
		return name, props
	}
	out := maps.Clone(props)
	if out == nil {
		out = make(map[string]any, len(add))
	}
	for k, v := range add {
		if _, set := out[k]; !set {
			out[k] = v
		}
	}
	return name, out
}

// Down returns a state of the step's protocol as a state of the protocol
// before: without the properties the step added, or the fallback of a
// block the protocol before lacks.
func (s *BlockStep) Down(name string, props map[string]any) (string, map[string]any) {
	if f, ok := s.Fallbacks[name]; ok {
		return f.Name, maps.Clone(f.States)
	}
	add, ok := s.Added[name]
	if !ok {
		return name, props
	}
	out := maps.Clone(props)
	for k := range add {
		delete(out, k)
	}
	return name, out
}

// schemaFile is what blocks.json and fallbacks.json of a protocol hold:
// a schema of pmmp's in the format of BedrockBlockUpgradeSchema, of which
// the adapter reads the added properties alone, and the fallbacks.
type schemaFile struct {
	MaxVersionMajor    int                                  `json:"maxVersionMajor"`
	MaxVersionMinor    int                                  `json:"maxVersionMinor"`
	MaxVersionPatch    int                                  `json:"maxVersionPatch"`
	MaxVersionRevision int                                  `json:"maxVersionRevision"`
	AddedProperties    map[string]map[string]map[string]any `json:"addedProperties"`
}

var (
	stepsMu sync.Mutex
	steps   = map[int32]*BlockStep{}
)

// BlockStepOf returns the step of block states from the protocol before p to
// p, read the first time; false for the first protocol, or one without
// data. A schema with more than added properties, which the adapter does
// not apply, panics: its states would be wrong.
func BlockStepOf(p int32) (*BlockStep, bool) {
	stepsMu.Lock()
	defer stepsMu.Unlock()
	if s, ok := steps[p]; ok {
		return s, s != nil
	}
	s := readStep(p)
	steps[p] = s
	return s, s != nil
}

func readStep(p int32) *BlockStep {
	b, err := files.ReadFile(fmt.Sprintf("v%d/blocks.json", p))
	if err != nil {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		panic(fmt.Sprintf("bedrockdata: blocks.json of %d: %v", p, err))
	}
	for k := range raw {
		switch k {
		case "maxVersionMajor", "maxVersionMinor", "maxVersionPatch", "maxVersionRevision", "addedProperties":
		default:
			panic(fmt.Sprintf("bedrockdata: blocks.json of %d holds %s, which the adapter does not apply", p, k))
		}
	}
	var schema schemaFile
	if err := json.Unmarshal(b, &schema); err != nil {
		panic(fmt.Sprintf("bedrockdata: blocks.json of %d: %v", p, err))
	}
	s := &BlockStep{Added: make(map[string]map[string]any, len(schema.AddedProperties)), Fallbacks: map[string]BlockState{}}
	for name, props := range schema.AddedProperties {
		s.Added[name] = make(map[string]any, len(props))
		for prop, tagged := range props {
			s.Added[name][prop] = schemaValue(p, tagged)
		}
	}
	if b, err := files.ReadFile(fmt.Sprintf("v%d/fallbacks.json", p)); err == nil {
		if err := json.Unmarshal(b, &s.Fallbacks); err != nil {
			panic(fmt.Sprintf("bedrockdata: fallbacks.json of %d: %v", p, err))
		}
		for _, f := range s.Fallbacks {
			for k, v := range f.States {
				// JSON numbers of states are bytes, as the properties of
				// fallbacks are.
				if n, ok := v.(float64); ok {
					f.States[k] = uint8(n)
				}
			}
		}
	}
	return s
}

// schemaValue returns a value of a property of a schema, tagged with the
// NBT type of the property: {"byte": 0}, {"string": "none"} or
// {"int": 3}.
func schemaValue(p int32, tagged map[string]any) any {
	for kind, v := range tagged {
		switch kind {
		case "byte":
			return uint8(v.(float64))
		case "int":
			return int32(v.(float64))
		case "string":
			return v.(string)
		}
		panic(fmt.Sprintf("bedrockdata: blocks.json of %d: a value of type %s", p, kind))
	}
	panic(fmt.Sprintf("bedrockdata: blocks.json of %d: a value of no type", p))
}

// MapState returns a state of the game as the clients of protocol p name
// it: through the steps of the protocols after GameBlocks up to p, or back
// through those down to p.
func MapState(p int32, name string, props map[string]any) (string, map[string]any) {
	i, j := slices.Index(protocols, GameBlocks), slices.Index(protocols, p)
	for ; i < j; i++ {
		if s, ok := BlockStepOf(protocols[i+1]); ok {
			name, props = s.Up(name, props)
		}
	}
	for ; i > j && j >= 0; i-- {
		if s, ok := BlockStepOf(protocols[i]); ok {
			name, props = s.Down(name, props)
		}
	}
	return name, props
}

// GameState returns a state of protocol p as the game names it: the
// inverse of MapState, through the steps of the protocols after p up to
// GameBlocks, or back through those down to it. Where several states of the
// game are one state of p, as wire of every connection is wire of 2193, it
// is the one of them whose properties p lacks take the values of the
// steps: that of wire connected to no side. A state of p that stands in for
// a block p lacks is that state of the game.
func GameState(p int32, name string, props map[string]any) (string, map[string]any) {
	i, j := slices.Index(protocols, p), slices.Index(protocols, GameBlocks)
	for ; i < j && i >= 0; i++ {
		if s, ok := BlockStepOf(protocols[i+1]); ok {
			name, props = s.Up(name, props)
		}
	}
	for ; i > j; i-- {
		if s, ok := BlockStepOf(protocols[i]); ok {
			name, props = s.Down(name, props)
		}
	}
	return name, props
}

// KnowsBlock reports whether the clients of protocol p know the block of
// the game of a name: false for a block the steps between GameBlocks and p
// replace by a fallback, as 2193 does the disc press of 1.26.60.
func KnowsBlock(p int32, name string) bool {
	_, lacks := BlockFallback(p, name)
	return !lacks
}

// BlockFallback returns the name of the block the clients of protocol p are
// shown in place of a block of the game they lack, as 2193's clients are
// shown a stonecutter for the disc press; false for a block they know.
func BlockFallback(p int32, name string) (string, bool) {
	lacks := false
	i, j := slices.Index(protocols, GameBlocks), slices.Index(protocols, p)
	for ; i > j && j >= 0; i-- {
		if s, ok := BlockStepOf(protocols[i]); ok {
			if f, ok := s.Fallbacks[name]; ok {
				name, lacks = f.Name, true
			}
		}
	}
	return name, lacks
}

// NamesBlocksAsTheGame reports whether the clients of protocol p name every
// state of the game as the game does: whether no step lies between
// GameBlocks and p. A protocol without data, which MapState leaves the
// states of as they are, does.
func NamesBlocksAsTheGame(p int32) bool {
	i, j := slices.Index(protocols, GameBlocks), slices.Index(protocols, p)
	if j < 0 {
		return true
	}
	for lo, hi := min(i, j), max(i, j); lo < hi; lo++ {
		if _, ok := BlockStepOf(protocols[lo+1]); ok {
			return false
		}
	}
	return true
}
