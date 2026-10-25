package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type torchNetworkBlock struct {
	ID     string
	States struct {
		Signal   int    `json:"redstone_signal"`
		Powered  bool   `json:"open_bit"`
		Facing   string `json:"torch_facing_direction"`
		Lit      bool   `json:"output_lit_bit"`
		Subtract bool   `json:"output_subtract_bit"`
		Cardinal string `json:"minecraft:cardinal_direction"`
	}
}

func compareTorchNetworkBlock(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, expected torchNetworkBlock) {
	t.Helper()
	b := tx.Block(pos)
	id, _ := b.EncodeBlock()
	if id != expected.ID {
		t.Errorf("tick %d at %v: %s, native %s", tick, pos, id, expected.ID)
	}
	if wire, ok := b.(RedstoneWire); ok && wire.Power != expected.States.Signal {
		t.Errorf("tick %d at %v: power %d, native %d", tick, pos, wire.Power, expected.States.Signal)
	}
	if lever, ok := b.(Lever); ok && lever.Powered != expected.States.Powered {
		t.Errorf("tick %d at %v: lever=%v, native %v", tick, pos, lever.Powered, expected.States.Powered)
	}
}

func TestTorchNetworksMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_torch_network.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Length, Phase   int
				Closed, Reverse bool
			}
			Layout  struct{ Torches, Supports, Wires []cube.Pos }
			Samples []struct {
				Tick           int64
				Torches, Wires []torchNetworkBlock
				Input          torchNetworkBlock
			}
			Changes []struct {
				Tick  int64
				Block string
				Pos   cube.Pos
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 36 {
		t.Fatal("incomplete native torch network capture")
	}
	for _, tr := range reference.Scenarios {
		c := tr.Scenario
		t.Run(fmt.Sprintf("length%d/closed%v/reverse%v/phase%d", c.Length, c.Closed, c.Reverse, c.Phase), func(t *testing.T) {
			if len(tr.Samples) != 112 || len(tr.Layout.Torches) != c.Length || len(tr.Changes) != 3 {
				t.Fatal("invalid native network trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = tr.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for _, p := range tr.Layout.Supports {
					tx.SetBlock(p, Stone{}, nil)
				}
				for _, p := range tr.Layout.Wires {
					tx.SetBlock(p, RedstoneWire{}, nil)
				}
				tx.SetBlock(cube.Pos{-1, 64, 0}, RedstoneRepeater{Facing: cube.West}, nil)
				for index := range c.Length {
					i := index
					if c.Reverse {
						i = c.Length - 1 - index
					}
					facing := cube.FaceWest
					if i == 0 {
						facing = cube.FaceDown
					}
					tx.SetBlock(tr.Layout.Torches[i], RedstoneTorch{Facing: facing, Lit: true}, nil)
				}
			})
			for _, s := range tr.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != s.Tick {
						t.Errorf("world tick %d, native %d", tx.CurrentTick(), s.Tick)
					}
					if len(s.Torches) != c.Length || len(s.Wires) != len(tr.Layout.Wires) {
						t.Fatal("incomplete native sample")
					}
					for i, expected := range s.Torches {
						compareTorchNetworkBlock(t, tx, s.Tick, tr.Layout.Torches[i], expected)
					}
					for i, expected := range s.Wires {
						compareTorchNetworkBlock(t, tx, s.Tick, tr.Layout.Wires[i], expected)
					}
					compareTorchNetworkBlock(t, tx, s.Tick, cube.Pos{-1, 64, 0}, s.Input)
					for _, change := range tr.Changes {
						if change.Tick != s.Tick || change.Block == "" {
							continue
						}
						var b world.Block = Air{}
						if change.Block == "redstone_block" {
							b = RedstoneBlock{}
						} else if change.Block != "air" {
							t.Errorf("unknown source %s", change.Block)
						}
						tx.SetBlock(change.Pos, b, nil)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

// Cache traversal depends on the native chunk maps and component vectors.
// Cycle shape/order is checked independently from exact startup/release phase.
func TestTorchClusterCyclesMatchNativeCapture(t *testing.T) {
	t.Run("subsets", func(t *testing.T) { testTorchClusters(t, "bedrock_1_26_51_torch_cluster.json", 16, 112, false) })
	t.Run("translations", func(t *testing.T) {
		testTorchClusters(t, "bedrock_1_26_51_torch_cluster_translation.json", 20, 80, false)
	})
}

func TestTorchClusterExactPhaseMatchesNativeCapture(t *testing.T) {
	testTorchClusters(t, "bedrock_1_26_51_torch_cluster.json", 16, 112, true)
	testTorchClusters(t, "bedrock_1_26_51_torch_cluster_translation.json", 20, 80, true)
}

func testTorchClusters(t *testing.T, filename string, scenarios, frames int, exact bool) {

	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sides   []string
				Center  cube.Pos
				Phase   int
				Reverse bool
			}
			Positions []cube.Pos
			Samples   []struct {
				Tick        int64
				Torches     []torchNetworkBlock
				Wire, Input torchNetworkBlock
			}
			Changes []struct {
				Tick    int64
				Powered bool
				Pos     cube.Pos
				Kind    string
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != scenarios {
		t.Fatal("incomplete native torch cluster capture")
	}
	facings := map[string]cube.Face{"east": cube.FaceWest, "west": cube.FaceEast, "north": cube.FaceSouth, "south": cube.FaceNorth}
	if len(reference.Scenarios[0].Samples) == 0 {
		t.Fatal("missing first native frame")
	}
	firstTick := reference.Scenarios[0].Samples[0].Tick
	settings := (world.NopProvider{}).Settings()
	settings.CurrentTick = firstTick - 2
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}, Entities: redstoneBreakDropTestEntityRegistry()}.New()
	t.Cleanup(func() { _ = w.Close() })
	current := firstTick - 1
	for _, tr := range reference.Scenarios {
		c := tr.Scenario
		center := c.Center
		if center == (cube.Pos{}) {
			center = cube.Pos{0, 64, 0}
		}
		if !t.Run(fmt.Sprintf("%v/%v/reverse%v/phase%d", center, c.Sides, c.Reverse, c.Phase), func(t *testing.T) {
			if len(tr.Samples) != frames || len(tr.Positions) != len(c.Sides) || len(tr.Changes) != 3 {
				t.Fatal("invalid native cluster trace")
			}
			setup := tr.Changes[0].Tick
			if current > setup || setup != tr.Samples[0].Tick-1 {
				t.Fatal("native setup is out of order")
			}
			for current < setup {
				w.AdvanceTick()
				current++
			}
			<-w.Exec(func(tx *world.Tx) {
				// Native scenarios reuse the same world. Clear only this setup's
				// fill region, preserving old chunks and other circuits.
				for x := center[0] - 3; x <= center[0]+3; x++ {
					for y := center[1] - 3; y <= center[1]+4; y++ {
						for z := center[2] - 3; z <= center[2]+3; z++ {
							p := cube.Pos{x, y, z}
							if _, air := tx.Block(p).(Air); !air {
								tx.SetBlock(p, Air{}, nil)
							}
						}
					}
				}
				tx.SetBlock(center, Stone{}, nil)
				for _, p := range tr.Positions {
					tx.SetBlock(p.Side(cube.FaceUp), Stone{}, nil)
				}
				tx.SetBlock(center.Side(cube.FaceUp), RedstoneWire{}, nil)
				tx.SetBlock(center.Side(cube.FaceDown), Lever{Facing: cube.FaceDown, Direction: cube.North}, nil)
				for index := range len(c.Sides) {
					i := index
					if c.Reverse {
						i = len(c.Sides) - 1 - index
					}
					facing, ok := facings[c.Sides[i]]
					if !ok {
						t.Fatal("unknown facing")
					}
					tx.SetBlock(tr.Positions[i], RedstoneTorch{Facing: facing, Lit: true}, nil)
				}
			})
			nativeNext := make(map[int]int)
			previous := -1
			for _, sample := range tr.Samples {
				count, on := 0, -1
				for i, b := range sample.Torches {
					if b.ID == "minecraft:redstone_torch" {
						count++
						on = i
					}
				}
				if count != 1 {
					previous = -1
					continue
				}
				if previous >= 0 && previous != on {
					if next, ok := nativeNext[previous]; ok && next != on {
						t.Fatal("native cluster has inconsistent rotation")
					}
					nativeNext[previous] = on
				}
				previous = on
			}
			if len(nativeNext) != len(c.Sides) {
				t.Fatal("native trace did not exercise every ring edge")
			}
			previous = -1
			for _, s := range tr.Samples {
				if s.Tick != current+1 {
					t.Fatal("nonconsecutive native frame")
				}
				w.AdvanceTick()
				current++
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != s.Tick {
						t.Errorf("world tick %d, native %d", tx.CurrentTick(), s.Tick)
					}
					if len(s.Torches) != len(c.Sides) {
						t.Fatal("incomplete native sample")
					}
					nativeCount, actualCount, on := 0, 0, -1
					for i, expected := range s.Torches {
						if expected.ID == "minecraft:redstone_torch" {
							nativeCount++
						}
						live, ok := tx.Block(tr.Positions[i]).(RedstoneTorch)
						if !ok {
							t.Errorf("tick %d missing torch at %v", s.Tick, tr.Positions[i])
						}
						if live.Lit {
							actualCount++
							on = i
						}
						if exact {
							compareTorchNetworkBlock(t, tx, s.Tick, tr.Positions[i], expected)
						}
					}
					if actualCount != nativeCount {
						t.Errorf("tick %d powered torch count=%d, native=%d", s.Tick, actualCount, nativeCount)
					}
					if actualCount == 1 {
						if previous >= 0 && previous != on && nativeNext[previous] != on {
							t.Errorf("tick %d rotation %d->%d, native next=%d", s.Tick, previous, on, nativeNext[previous])
						}
						previous = on
					} else {
						previous = -1
					}
					compareTorchNetworkBlock(t, tx, s.Tick, center.Side(cube.FaceUp), s.Wire)
					compareTorchNetworkBlock(t, tx, s.Tick, center.Side(cube.FaceDown), s.Input)
					for _, change := range tr.Changes {
						if change.Tick == s.Tick && change.Kind != "setup" {
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
