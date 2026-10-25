package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// This isolates the remaining world traversal problem: the actual capacitor
// algorithm, given the independently observed native cache order, must match
// every native frame, including startup and external-input release. It does
// not claim that the world's current coordinate sort constructs that order.
func TestTorchClusterNativeTraversalExplainsPhase(t *testing.T) {
	t.Run("placement-history", func(t *testing.T) { testTorchNativeTraversal(t, "bedrock_1_26_51_circuit_order.json", 20) })
	t.Run("table-growth", func(t *testing.T) { testTorchNativeTraversal(t, "bedrock_1_26_51_circuit_order_growth.json", 6) })
}

func testTorchNativeTraversal(t *testing.T, filename string, scenarios int) {
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
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
				Samples   []struct {
					Tick    int64
					Torches []torchNetworkBlock
					Input   torchNetworkBlock
				}
				Changes []struct {
					Tick    int64
					Kind    string
					Powered bool
				}
			} `json:"world_trace"`
			Circuit struct {
				Chunks []struct {
					Evaluate bool
					Entries  []struct {
						Pos   cube.Pos
						Flags uint8
					}
				}
			}
			Torches []struct {
				Pos  cube.Pos
				Next cube.Pos `json:"next_pos"`
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != scenarios {
		t.Fatal("incomplete native traversal capture")
	}
	for _, captured := range reference.Scenarios {
		tr := captured.WorldTrace
		c := tr.Scenario
		t.Run(fmt.Sprintf("%v/reverse%v/phase%d", c.Center, c.Reverse, c.Phase), func(t *testing.T) {
			if len(tr.Positions) != 4 || len(tr.Samples) != 80 || len(tr.Changes) != 3 || len(captured.Torches) != 4 {
				t.Fatal("incomplete native cluster observation")
			}
			index := make(map[cube.Pos]int, 4)
			for i, p := range tr.Positions {
				index[p] = i
			}
			var order []int
			for _, chunk := range captured.Circuit.Chunks {
				for _, entry := range chunk.Entries {
					if i, ok := index[entry.Pos]; ok {
						if !chunk.Evaluate || entry.Flags&1 == 0 {
							t.Fatal("sampled native torch is excluded from cache traversal")
						}
						order = append(order, i)
					}
				}
			}
			if len(order) != 4 {
				t.Fatal("missing native cache order")
			}
			// Native updateDependencies constructs N -> W -> S -> E -> N;
			// the fixture's positions use E, N, W, S. Only the last counter
			// starts at 16. Validate the graph separately from the cache order.
			ring := [...]int{1, 2, 3, 0}
			states := make([]torchCircuit, 4)
			for i, member := range ring {
				states[member].on = true
				states[member].nextOrder = &states[ring[(i+1)%len(ring)]]
			}
			states[ring[len(ring)-1]].selfPowerCount = 16
			for _, torch := range captured.Torches {
				i, ok := index[torch.Pos]
				if !ok {
					t.Fatal("native dependency contains an unexpected torch")
				}
				next, ok := index[torch.Next]
				if !ok || states[i].nextOrder != &states[next] {
					t.Fatalf("native dependency at %v points to %v", torch.Pos, torch.Next)
				}
			}
			live := [4]bool{true, true, true, true}
			var pending *[4]bool
			powered := false
			for _, sample := range tr.Samples {
				if pending != nil {
					live, pending = *pending, nil
				}
				if len(sample.Torches) != len(states) {
					t.Fatal("incomplete native frame")
				}
				for i, block := range sample.Torches {
					want := block.ID == "minecraft:redstone_torch"
					if live[i] != want {
						t.Fatalf("tick %d at %v: lit=%v, native=%v", sample.Tick, tr.Positions[i], live[i], want)
					}
				}
				if sample.Input.States.Powered != powered {
					t.Fatalf("tick %d input differs from native lever", sample.Tick)
				}
				if sample.Tick&1 == 0 {
					power := 0
					if powered {
						power = 15
					}
					sources := []torchCircuitSource{{group: torchSourcePoweredBlock, sources: []torchCircuitSource{{
						componentType: 0x800001, power: power, direct: true,
					}}}}
					for _, i := range order {
						states[i].cache(tr.Positions[i], sources)
					}
					next := new([4]bool)
					for i := range states {
						states[i].evaluate()
						next[i] = states[i].on
					}
					pending = next
				}
				for _, change := range tr.Changes {
					if change.Tick == sample.Tick && change.Kind != "setup" {
						powered = change.Powered
					}
				}
			}
		})
	}
}

// Unlike replaying each geometry in a fresh world, this keeps native chunk
// registration history and unrelated older circuits between scenarios.
func TestTorchClusterWorldHistoryMatchesNativeCapture(t *testing.T) {
	t.Run("placement-history", func(t *testing.T) { testTorchClusterWorldHistory(t, "bedrock_1_26_51_circuit_order.json", 20) })
	t.Run("table-growth", func(t *testing.T) { testTorchClusterWorldHistory(t, "bedrock_1_26_51_circuit_order_growth.json", 6) })
}

func testTorchClusterWorldHistory(t *testing.T, filename string, scenarios int) {
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Trace struct {
				Scenario struct {
					Center  cube.Pos
					Reverse bool
					Phase   int
				}
				Positions []cube.Pos
				Statics   []cube.Pos `json:"static_positions"`
				Samples   []struct {
					Tick        int64
					Torches     []torchNetworkBlock
					Wire, Input torchNetworkBlock
				}
				Changes []struct {
					Tick    int64
					Kind    string
					Pos     cube.Pos
					Powered bool
				}
			} `json:"world_trace"`
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != scenarios || len(reference.Scenarios[0].Trace.Samples) == 0 {
		t.Fatal("incomplete native cluster history capture")
	}
	firstTick := reference.Scenarios[0].Trace.Samples[0].Tick
	settings := (world.NopProvider{}).Settings()
	settings.CurrentTick = firstTick - 2
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}, Entities: redstoneBreakDropTestEntityRegistry()}.New()
	t.Cleanup(func() { _ = w.Close() })
	current := firstTick - 1
	var previousStatics []cube.Pos
	for _, captured := range reference.Scenarios {
		tr := captured.Trace
		c := tr.Scenario
		if !t.Run(fmt.Sprintf("%v/reverse%v/phase%d/static%d", c.Center, c.Reverse, c.Phase, len(tr.Statics)), func(t *testing.T) {
			if len(tr.Positions) != 4 || len(tr.Samples) != 80 || len(tr.Changes) != 3 {
				t.Fatal("incomplete native world trace")
			}
			setupTick := tr.Changes[0].Tick
			if current > setupTick || setupTick != tr.Samples[0].Tick-1 {
				t.Fatal("native setup is out of order")
			}
			for current < setupTick {
				w.AdvanceTick()
				current++
			}
			<-w.Exec(func(tx *world.Tx) {
				for _, p := range previousStatics {
					tx.SetBlock(p, Air{}, nil)
					tx.SetBlock(p.Side(cube.FaceDown), Air{}, nil)
				}
				for x := c.Center[0] - 3; x <= c.Center[0]+3; x++ {
					for y := c.Center[1] - 3; y <= c.Center[1]+4; y++ {
						for z := c.Center[2] - 3; z <= c.Center[2]+3; z++ {
							p := cube.Pos{x, y, z}
							if _, air := tx.Block(p).(Air); !air {
								tx.SetBlock(p, Air{}, nil)
							}
						}
					}
				}
				tx.SetBlock(c.Center, Stone{}, nil)
				for _, p := range tr.Positions {
					tx.SetBlock(p.Side(cube.FaceUp), Stone{}, nil)
				}
				tx.SetBlock(c.Center.Side(cube.FaceUp), RedstoneWire{}, nil)
				tx.SetBlock(c.Center.Side(cube.FaceDown), Lever{Facing: cube.FaceDown, Direction: cube.North}, nil)
				facings := [...]cube.Face{cube.FaceWest, cube.FaceSouth, cube.FaceEast, cube.FaceNorth}
				for index := range tr.Positions {
					i := index
					if c.Reverse {
						i = len(tr.Positions) - 1 - index
					}
					tx.SetBlock(tr.Positions[i], RedstoneTorch{Facing: facings[i], Lit: true}, nil)
				}
				for _, p := range tr.Statics {
					tx.SetBlock(p.Side(cube.FaceDown), Stone{}, nil)
					tx.SetBlock(p, Lever{Facing: cube.FaceUp, Direction: cube.North}, nil)
				}
			})
			previousStatics = tr.Statics
			for _, sample := range tr.Samples {
				if len(sample.Torches) != 4 || sample.Tick != current+1 {
					t.Fatal("incomplete/nonconsecutive native sample")
				}
				w.AdvanceTick()
				current++
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Errorf("world tick %d, native tick %d", tx.CurrentTick(), sample.Tick)
					}
					for i, expected := range sample.Torches {
						compareTorchNetworkBlock(t, tx, sample.Tick, tr.Positions[i], expected)
					}
					compareTorchNetworkBlock(t, tx, sample.Tick, c.Center.Side(cube.FaceUp), sample.Wire)
					compareTorchNetworkBlock(t, tx, sample.Tick, c.Center.Side(cube.FaceDown), sample.Input)
					for _, change := range tr.Changes {
						if change.Tick == sample.Tick && change.Kind != "setup" {
							tx.SetBlock(change.Pos, Lever{Facing: cube.FaceDown, Direction: cube.North, Powered: change.Powered}, nil)
						}
					}
				})
				if t.Failed() {
					break
				}
			}
		}) {
			break
		}
	}
}
