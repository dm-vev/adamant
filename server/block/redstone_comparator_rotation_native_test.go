package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestComparatorRotationMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_comparator_rotation.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Method, Facing        string
				Subtract              bool
				Phase                 int
				WestBites, NorthBites int
			}
			Samples []struct {
				Tick                    int64
				Comparator, West, North torchNetworkBlock
				East, South             []torchNetworkBlock
			}
			Changes []struct {
				Tick                int64
				Kind, Block, Facing string
				Pos                 cube.Pos
				Bites               int
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 40 {
		t.Fatal("incomplete native comparator rotation capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/subtract%v/phase%d/rear%d-side%d", c.Method, c.Facing, c.Subtract, c.Phase, c.WestBites, c.NorthBites), func(t *testing.T) {
			if len(trace.Samples) != 80 || len(trace.Changes) < 9 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native comparator rotation trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 4; x++ {
					for z := -2; z <= 4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{-1, 65, 0}, Cake{Bites: c.WestBites}, nil)
				tx.SetBlock(cube.Pos{0, 65, -1}, Cake{Bites: c.NorthBites}, nil)
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West, Subtract: c.Subtract}, nil)
				for d := 1; d <= 3; d++ {
					tx.SetBlock(cube.Pos{d, 65, 0}, RedstoneWire{}, nil)
					tx.SetBlock(cube.Pos{0, 65, d}, RedstoneWire{}, nil)
				}
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Errorf("world tick %d, native %d", tx.CurrentTick(), sample.Tick)
					}
					if len(sample.East) != 3 || len(sample.South) != 3 {
						t.Fatal("incomplete native wire sample")
					}
					compareComparatorNativeBlock(t, tx, sample.Tick, cube.Pos{0, 65, 0}, sample.Comparator)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{-1, 65, 0}, sample.West)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{0, 65, -1}, sample.North)
					for i := range 3 {
						compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{i + 1, 65, 0}, sample.East[i])
						compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{0, 65, i + 1}, sample.South[i])
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick || change.Kind == "setup" {
							continue
						}
						var b world.Block = Air{}
						switch change.Kind {
						case "cake":
							if change.Block == "cake" {
								b = Cake{Bites: change.Bites}
							} else if change.Block != "air" {
								t.Errorf("unknown native cake %q", change.Block)
							}
						case "comparator", "state":
							if change.Block != "air" {
								if change.Block != "powered_comparator" && change.Block != "unpowered_comparator" {
									t.Errorf("unknown native comparator %q", change.Block)
								}
								facing := cube.West
								switch change.Facing {
								case "west":
								case "east":
									facing = cube.East
								case "north":
									facing = cube.North
								default:
									t.Errorf("unknown native facing %q", change.Facing)
								}
								var output uint8
								var lit bool
								if old, ok := tx.Block(change.Pos).(RedstoneComparator); ok {
									output = old.Output
									lit = change.Kind == "state" && old.Lit
								}
								b = RedstoneComparator{Facing: facing, Subtract: c.Subtract, Powered: change.Block == "powered_comparator", Lit: lit, Output: output}
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
