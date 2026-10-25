package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func compareComparatorNativeBlock(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, expected torchNetworkBlock) {
	t.Helper()
	compareTorchNetworkBlock(t, tx, tick, pos, expected)
	if c, ok := tx.Block(pos).(RedstoneComparator); ok {
		if c.Lit != expected.States.Lit || c.Subtract != expected.States.Subtract || c.Facing.String() != expected.States.Cardinal {
			t.Errorf("tick %d at %v: lit=%v subtract=%v facing=%s, native lit=%v subtract=%v facing=%s", tick, pos, c.Lit, c.Subtract, c.Facing, expected.States.Lit, expected.States.Subtract, expected.States.Cardinal)
		}
	}
}

func TestComparatorModeMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_comparator_mode.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Method       string
				Subtract     bool
				Bites, Phase int
			}
			Samples []struct {
				Tick                                    int64
				First, Wire, Second, Output, Side, Rear torchNetworkBlock
				SideComparator                          torchNetworkBlock `json:"side_comparator"`
			}
			Changes []struct {
				Tick        int64
				Kind, Block string
				Pos         cube.Pos
				Subtract    bool
				Bites       int
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 32 {
		t.Fatal("incomplete native comparator mode capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/subtract%v/bites%d/phase%d", c.Method, c.Subtract, c.Bites, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 80 || len(trace.Changes) < 9 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native comparator mode trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 4; x++ {
					for z := -1; z <= 3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{-1, 65, 0}, Cake{Bites: c.Bites}, nil)
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West, Subtract: c.Subtract}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneWire{}, nil)
				tx.SetBlock(cube.Pos{2, 65, 0}, RedstoneComparator{Facing: cube.West}, nil)
				tx.SetBlock(cube.Pos{3, 65, 0}, RedstoneWire{}, nil)
				tx.SetBlock(cube.Pos{0, 65, 1}, RedstoneWire{}, nil)
				tx.SetBlock(cube.Pos{0, 65, 2}, RedstoneComparator{Facing: cube.South}, nil)
				tx.SetBlock(cube.Pos{0, 65, 3}, Cake{Bites: 3}, nil)
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Errorf("world tick %d, native %d", tx.CurrentTick(), sample.Tick)
					}
					for _, check := range []struct {
						Pos      cube.Pos
						Expected torchNetworkBlock
					}{
						{cube.Pos{0, 65, 0}, sample.First}, {cube.Pos{1, 65, 0}, sample.Wire}, {cube.Pos{2, 65, 0}, sample.Second}, {cube.Pos{3, 65, 0}, sample.Output},
						{cube.Pos{0, 65, 1}, sample.Side}, {cube.Pos{0, 65, 2}, sample.SideComparator}, {cube.Pos{-1, 65, 0}, sample.Rear},
					} {
						compareComparatorNativeBlock(t, tx, sample.Tick, check.Pos, check.Expected)
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick || change.Kind == "setup" {
							continue
						}
						var b world.Block = Air{}
						switch change.Kind {
						case "rear":
							if change.Block == "cake" {
								b = Cake{Bites: change.Bites}
							} else if change.Block != "air" {
								t.Errorf("unknown rear %q", change.Block)
							}
						case "comparator":
							if change.Block != "air" {
								if change.Block != "unpowered_comparator" && change.Block != "powered_comparator" {
									t.Errorf("unknown comparator %q", change.Block)
								}
								// Preserve the actor's existing output across an ordinary
								// permutation change; remove/replacement has a new actor.
								var output uint8
								var lit bool
								if old, ok := tx.Block(change.Pos).(RedstoneComparator); ok {
									output = old.Output
									if c.Method == "state" && (sample.Tick-trace.Changes[0].Tick == 16 || sample.Tick-trace.Changes[0].Tick == 48) {
										lit = old.Lit
									}
								}
								b = RedstoneComparator{Facing: cube.West, Subtract: change.Subtract, Powered: change.Block == "powered_comparator", Lit: lit, Output: output}
							}
						default:
							t.Errorf("unknown native change %q", change.Kind)
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
