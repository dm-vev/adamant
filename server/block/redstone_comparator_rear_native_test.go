package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestComparatorRearPriorityMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_comparator_rear.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Material string
				Bites    int
				Powered  bool
			}
			Samples []struct {
				Tick                                 int64
				Comparator, Output, Near, Far, Input torchNetworkBlock
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 30 {
		t.Fatal("incomplete native rear-priority capture")
	}
	for _, tr := range reference.Scenarios {
		c := tr.Scenario
		t.Run(fmt.Sprintf("%s/bites%d/powered%v", c.Material, c.Bites, c.Powered), func(t *testing.T) {
			if len(tr.Samples) != 24 {
				t.Fatal("incomplete native rear-priority trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = tr.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 3; x++ {
					for z := -2; z <= 2; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{-2, 65, 0}, Cake{Bites: c.Bites}, nil)
				var near world.Block
				switch c.Material {
				case "stone":
					near = Stone{}
				case "redstone_block":
					near = RedstoneBlock{}
				case "glass":
					near = Glass{}
				case "air":
					near = Air{}
				case "glowstone":
					near = Glowstone{}
				default:
					t.Errorf("unknown native intervening block %s", c.Material)
					return
				}
				tx.SetBlock(cube.Pos{-1, 65, 0}, near, nil)
				tx.SetBlock(cube.Pos{-1, 65, -1}, RedstoneRepeater{Facing: cube.North}, nil)
				if c.Powered {
					tx.SetBlock(cube.Pos{-1, 65, -2}, RedstoneBlock{}, nil)
				}
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneWire{}, nil)
			})
			for i, s := range tr.Samples {
				w.AdvanceTick()
				if i < 16 {
					continue
				}
				<-w.Exec(func(tx *world.Tx) {
					compareTorchNetworkBlock(t, tx, s.Tick, cube.Pos{0, 65, 0}, s.Comparator)
					compareTorchNetworkBlock(t, tx, s.Tick, cube.Pos{1, 65, 0}, s.Output)
					compareTorchNetworkBlock(t, tx, s.Tick, cube.Pos{-1, 65, 0}, s.Near)
					compareTorchNetworkBlock(t, tx, s.Tick, cube.Pos{-2, 65, 0}, s.Far)
					compareTorchNetworkBlock(t, tx, s.Tick, cube.Pos{-1, 65, -1}, s.Input)
					if output := tx.Block(cube.Pos{0, 65, 0}).(RedstoneComparator).Output; int(output) != s.Output.States.Signal {
						t.Errorf("comparator output=%d, native=%d", output, s.Output.States.Signal)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
