package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestComparatorTimingMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_comparator_timing.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Source       string
				Subtract     bool
				Width, Phase int
			}
			Samples []struct {
				Tick                                    int64
				First, Wire, Second, Output, Side, Rear torchNetworkBlock
				SideComparator                          torchNetworkBlock `json:"side_comparator"`
			}
			Changes []struct {
				Tick    int64
				Pos     cube.Pos
				Block   string
				Powered bool
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 60 {
		t.Fatal("incomplete native comparator timing capture")
	}
	for _, tr := range reference.Scenarios {
		c := tr.Scenario
		t.Run(fmt.Sprintf("%s/subtract%v/width%d/phase%d", c.Source, c.Subtract, c.Width, c.Phase), func(t *testing.T) {
			if len(tr.Changes) != 2 || len(tr.Samples) < 37 || tr.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native comparator timing trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = tr.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 5; x++ {
					for z := -1; z <= 3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West, Subtract: c.Subtract}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneWire{}, nil)
				tx.SetBlock(cube.Pos{2, 65, 0}, RedstoneComparator{Facing: cube.West}, nil)
				tx.SetBlock(cube.Pos{3, 65, 0}, RedstoneWire{}, nil)
				tx.SetBlock(cube.Pos{0, 65, 1}, RedstoneWire{}, nil)
				tx.SetBlock(cube.Pos{0, 65, 2}, RedstoneComparator{Facing: cube.South}, nil)
				tx.SetBlock(cube.Pos{0, 65, 3}, Cake{Bites: 3}, nil)
				if c.Source == "wire" {
					tx.SetBlock(cube.Pos{-1, 65, 0}, RedstoneWire{}, nil)
				}
			})
			for _, s := range tr.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != s.Tick {
						t.Errorf("world tick %d, native %d", tx.CurrentTick(), s.Tick)
					}
					for _, check := range []struct {
						Pos      cube.Pos
						Expected torchNetworkBlock
					}{
						{cube.Pos{0, 65, 0}, s.First}, {cube.Pos{1, 65, 0}, s.Wire}, {cube.Pos{2, 65, 0}, s.Second}, {cube.Pos{3, 65, 0}, s.Output},
						{cube.Pos{0, 65, 1}, s.Side}, {cube.Pos{0, 65, 2}, s.SideComparator}, {cube.Pos{-1, 65, 0}, s.Rear},
					} {
						compareComparatorNativeBlock(t, tx, s.Tick, check.Pos, check.Expected)
					}
					for _, change := range tr.Changes {
						if change.Tick != s.Tick {
							continue
						}
						var source world.Block = Air{}
						switch change.Block {
						case "redstone_block":
							source = RedstoneBlock{}
						case "cake":
							source = Cake{}
						case "air":
						default:
							t.Errorf("unknown native source %s", change.Block)
						}
						tx.SetBlock(change.Pos, source, nil)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
