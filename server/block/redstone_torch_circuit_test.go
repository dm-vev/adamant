package block

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestTorchCacheMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_torch_cache.json")
	if err != nil {
		t.Fatal(err)
	}
	type source struct {
		Group, Type     uint64
		Power, Distance int
		Pos             cube.Pos
		Direct          *bool
		Sources         []json.RawMessage
	}
	var decodeSources func([]json.RawMessage) []torchCircuitSource
	decodeSources = func(raw []json.RawMessage) []torchCircuitSource {
		out := make([]torchCircuitSource, len(raw))
		for i, encoded := range raw {
			var s source
			if err := json.Unmarshal(encoded, &s); err != nil {
				t.Fatal(err)
			}
			out[i] = torchCircuitSource{group: s.Group, componentType: s.Type, power: s.Power, distance: s.Distance, pos: s.Pos,
				direct: s.Direct == nil || *s.Direct, sources: decodeSources(s.Sources)}
		}
		return out
	}
	var reference struct {
		Position cube.Pos `json:"source_position"`
		Cases    []struct {
			Initial, Cached, Result [5]int
			Sources                 []json.RawMessage
			Chain                   []int
			ChainResult             []int `json:"chain_result"`
			Changed                 bool
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.Cases) != 2240 {
		t.Fatal("incomplete native torch cache capture")
	}
	for i, example := range reference.Cases {
		state := torchCircuit{selfPowerCount: example.Initial[0], on: example.Initial[1] != 0, halfFrame: example.Initial[2] != 0,
			changed: example.Initial[3] != 0, canReignite: example.Initial[4] != 0}
		chain := []*torchCircuit{&state}
		for _, count := range example.Chain {
			next := &torchCircuit{selfPowerCount: count}
			chain[len(chain)-1].nextOrder = next
			chain = append(chain, next)
		}
		if len(chain) > 1 {
			chain[len(chain)-1].nextOrder = &state
		}
		state.cache(reference.Position, decodeSources(example.Sources))
		cached := [5]int{state.selfPowerCount, int(boolByte(state.nextOn)), int(boolByte(state.nextHalfFrame)), int(boolByte(state.nextChanged)), int(boolByte(state.canReignite))}
		if cached != example.Cached {
			t.Fatalf("case %d cache=%v, native=%v, input=%+v", i, cached, example.Cached, example)
		}
		changed := state.evaluate()
		result := [5]int{state.selfPowerCount, int(boolByte(state.on)), int(boolByte(state.halfFrame)), int(boolByte(state.changed)), int(boolByte(state.canReignite))}
		if result != example.Result || changed != example.Changed {
			t.Fatalf("case %d evaluate=%v changed=%v, native=%v changed=%v", i, result, changed, example.Result, example.Changed)
		}
		counts := make([]int, len(chain)-1)
		for i, next := range chain[1:] {
			counts[i] = next.selfPowerCount
		}
		if !slices.Equal(counts, example.ChainResult) {
			t.Fatalf("case %d following counters=%v, native=%v", i, counts, example.ChainResult)
		}
	}
}

func TestTorchEvaluatorMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_redstone_evaluators.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct{ Torches struct{ Cases [][9]int } }
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.Torches.Cases) != 1536 {
		t.Fatal("incomplete native torch evaluator capture")
	}
	for _, example := range reference.Torches.Cases {
		flags := example[0]
		state := torchCircuit{selfPowerCount: example[1], on: flags&1 != 0, halfFrame: flags&2 != 0, changed: flags&4 != 0,
			nextOn: flags&8 != 0, nextHalfFrame: flags&16 != 0, nextChanged: flags&32 != 0, canReignite: example[3] != 0}
		if example[2] != 0 {
			state.nextOrder = &torchCircuit{}
		}
		changed := state.evaluate()
		result := [5]int{int(boolByte(state.on)), int(boolByte(state.halfFrame)), int(boolByte(state.changed)), state.selfPowerCount, int(boolByte(changed))}
		if result != [5]int(example[4:]) {
			t.Fatalf("input=%v result=%v, native=%v", example[:4], result, example[4:])
		}
	}
}
