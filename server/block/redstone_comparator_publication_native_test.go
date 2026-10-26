package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestComparatorPublicationEditsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_comparator_publication.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Method        string
				Rising        bool
				Phase, Offset int
			}
			Samples []struct {
				Tick                                    int64
				First, Wire, Second, Output, Side, Rear torchNetworkBlock
				SideComparator                          torchNetworkBlock `json:"side_comparator"`
			}
			Changes []struct {
				Tick        int64
				Kind, Block string
				Powered     bool
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 48 {
		t.Fatal("incomplete native comparator publication capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/rising%v/phase%d/offset%d", c.Method, c.Rising, c.Phase, c.Offset), func(t *testing.T) {
			if len(trace.Samples) < 36 || len(trace.Changes) == 0 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native publication trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos := cube.Pos{0, 65, 0}
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 4; x++ {
					for z := -2; z <= 3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{0, 65, -1}, Cake{Bites: 3}, nil)
				if !c.Rising {
					tx.SetBlock(cube.Pos{-1, 65, 0}, RedstoneBlock{}, nil)
				}
				tx.SetBlock(pos, RedstoneComparator{Facing: cube.West}, nil)
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
						{pos, sample.First}, {cube.Pos{1, 65, 0}, sample.Wire}, {cube.Pos{2, 65, 0}, sample.Second}, {cube.Pos{3, 65, 0}, sample.Output},
						{cube.Pos{0, 65, 1}, sample.Side}, {cube.Pos{0, 65, 2}, sample.SideComparator}, {cube.Pos{-1, 65, 0}, sample.Rear},
					} {
						compareComparatorNativeBlock(t, tx, sample.Tick, check.Pos, check.Expected)
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick {
							continue
						}
						switch change.Kind {
						case "source":
							var b world.Block = Air{}
							if change.Powered {
								b = RedstoneBlock{}
							}
							tx.SetBlock(cube.Pos{-1, 65, 0}, b, nil)
						case "mode", "rotation":
							b, ok := tx.Block(pos).(RedstoneComparator)
							if !ok {
								t.Error("missing comparator for property edit")
								continue
							}
							if change.Kind == "mode" {
								b.Subtract = true
							} else {
								b.Facing = cube.North
							}
							tx.SetBlock(pos, b, nil)
						case "comparator":
							var b world.Block = Air{}
							if change.Block != "air" {
								if change.Block != "powered_comparator" && change.Block != "unpowered_comparator" {
									t.Errorf("unknown comparator %q", change.Block)
								}
								var output uint8
								if old, ok := tx.Block(pos).(RedstoneComparator); ok {
									output = old.Output
								}
								b = RedstoneComparator{Facing: cube.West, Powered: change.Block == "powered_comparator", Output: output}
							}
							tx.SetBlock(pos, b, nil)
						default:
							t.Errorf("unknown native change %q", change.Kind)
						}
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
