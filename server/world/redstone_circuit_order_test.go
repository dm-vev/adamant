package world

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

type circuitOrderTestBlock struct {
	redstoneCancellationSource
	kind uint64
}

func (b circuitOrderTestBlock) Hash() (uint64, uint64) { return b.kind, 0 }

// Replay registration/removal in the same world history as the owned native
// process. Both the queue before graph update and the resulting chunk vectors
// were independently read from BDS; no expected order comes from this model.
func TestCircuitTraversalMatchesNativeCapture(t *testing.T) {
	t.Run("placement-history", func(t *testing.T) { testCircuitTraversalCapture(t, "bedrock_1_26_51_circuit_order.json", 20) })
	t.Run("table-growth", func(t *testing.T) { testCircuitTraversalCapture(t, "bedrock_1_26_51_circuit_order_growth.json", 6) })
}

func testCircuitTraversalCapture(t *testing.T, filename string, scenarios int) {
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	type entry struct{ Pos cube.Pos }
	var reference struct {
		Complete  bool
		Scenarios []struct {
			WorldTrace struct {
				Scenario struct {
					Center  cube.Pos
					Reverse bool
					Phase   int
				}
				Positions []cube.Pos
				Statics   []cube.Pos `json:"static_positions"`
			} `json:"world_trace"`
			Pending struct {
				Buckets uint64 `json:"bucket_count"`
				Entries []entry
			} `json:"pending_before_flush"`
			Circuit struct {
				Buckets uint64 `json:"bucket_count"`
				Chunks  []struct {
					Pos     cube.Pos
					Entries []entry
				}
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != scenarios {
		t.Fatal("incomplete native traversal capture")
	}
	var traversal circuitTraversal
	var previousStatics []cube.Pos
	pendingGrowth, chunkGrowth := false, false
	for scenarioIndex, capture := range reference.Scenarios {
		tr := capture.WorldTrace
		c := tr.Scenario
		if !t.Run(fmt.Sprintf("%v/reverse%v/phase%d", c.Center, c.Reverse, c.Phase), func(t *testing.T) {
			for _, pos := range previousStatics {
				traversal.replace(pos, nil)
			}
			for pos := range traversal.kinds {
				if pos[0] >= c.Center[0]-3 && pos[0] <= c.Center[0]+3 &&
					pos[1] >= c.Center[1]-3 && pos[1] <= c.Center[1]+4 &&
					pos[2] >= c.Center[2]-3 && pos[2] <= c.Center[2]+3 {
					traversal.replace(pos, nil)
				}
			}
			traversal.replace(c.Center.Side(cube.FaceUp), circuitOrderTestBlock{kind: 1})
			traversal.replace(c.Center.Side(cube.FaceDown), circuitOrderTestBlock{kind: 2})
			for index := range tr.Positions {
				i := index
				if c.Reverse {
					i = len(tr.Positions) - 1 - index
				}
				traversal.replace(tr.Positions[i], circuitOrderTestBlock{kind: 3})
			}
			for _, pos := range tr.Statics {
				traversal.replace(pos, circuitOrderTestBlock{kind: 4})
			}
			previousStatics = tr.Statics
			if scenarioIndex > 0 {
				var want, got []cube.Pos
				for _, e := range capture.Pending.Entries {
					want = append(want, e.Pos)
				}
				for node := traversal.pending.head; node != nil; node = node.next {
					got = append(got, node.pos)
				}
				if !slices.Equal(got, want) || traversal.pending.mask+1 != capture.Pending.Buckets {
					t.Fatalf("pending queue=%v (%d buckets), native=%v (%d buckets)", got, traversal.pending.mask+1, want, capture.Pending.Buckets)
				}
				pendingGrowth = pendingGrowth || capture.Pending.Buckets > 8
			}
			traversal.flush()
			if traversal.chunks.mask+1 != capture.Circuit.Buckets {
				t.Fatalf("chunk buckets=%d, native=%d", traversal.chunks.mask+1, capture.Circuit.Buckets)
			}
			chunkGrowth = chunkGrowth || capture.Circuit.Buckets > 8
			node := traversal.chunks.head
			for _, expected := range capture.Circuit.Chunks {
				if node == nil || node.pos != expected.Pos {
					t.Fatalf("chunk order differs at native %v", expected.Pos)
				}
				var want []cube.Pos
				for _, e := range expected.Entries {
					want = append(want, e.Pos)
				}
				if got := traversal.entries[node.pos]; !slices.Equal(got, want) {
					t.Fatalf("chunk %v entries=%v, native=%v", node.pos, got, want)
				}
				node = node.next
			}
			if node != nil {
				t.Fatal("extra kernel chunk entries")
			}
		}) {
			break // Following scenarios depend on this exact registration history.
		}
	}
	if scenarios == 6 && (!pendingGrowth || !chunkGrowth) {
		t.Fatal("native stress capture did not exercise both hash-table growth paths")
	}
}
