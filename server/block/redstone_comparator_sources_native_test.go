package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Check source acceptance at steady state. The fixture retains every native
// frame, but this matrix deliberately asserts only the settled window;
// TestComparatorTimingMatchesNativeCapture covers pulse and chain timing.
func TestComparatorSideSourcesMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_comparator_sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Source   string
				Subtract bool
				Side     int
				Phase    int
			}
			Samples []struct {
				Tick       int64
				Comparator struct {
					ID     string
					States struct {
						Lit      bool `json:"output_lit_bit"`
						Subtract bool `json:"output_subtract_bit"`
					}
				}
				Output, Side, Rear torchNetworkBlock
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 48 {
		t.Fatal("incomplete native comparator source capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/subtract%v/side%d", c.Source, c.Subtract, c.Side), func(t *testing.T) {
			if len(trace.Samples) != 24 || c.Side != -1 && c.Side != 1 {
				t.Fatal("incomplete native side-source trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 3; x++ {
					for z := -3; z <= 3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{-1, 65, 0}, Cake{Bites: 3}, nil)
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West, Subtract: c.Subtract}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneWire{}, nil)
				p := cube.Pos{0, 65, c.Side}
				direction := cube.North
				if c.Side > 0 {
					direction = cube.South
				}
				var side world.Block = Air{}
				switch c.Source {
				case "none":
				case "redstone_block":
					side = RedstoneBlock{}
				case "torch":
					side = RedstoneTorch{Facing: cube.FaceDown, Lit: true}
				case "lever":
					side = Lever{Facing: cube.FaceUp, Direction: cube.North, Powered: true}
				case "wire":
					side = RedstoneWire{}
				case "repeater":
					side = RedstoneRepeater{Facing: direction}
				case "comparator":
					side = RedstoneComparator{Facing: direction}
				case "parallel_repeater":
					side = RedstoneRepeater{Facing: cube.West}
				case "cake":
					side = Cake{}
				case "cauldron":
					side = Cauldron{FillLevel: 6}
				case "composter":
					side = Composter{Level: 8}
				case "powered_stone":
					side = Stone{}
				default:
					t.Errorf("unknown native side source %q", c.Source)
				}
				if c.Source != "none" {
					tx.SetBlock(p, side, nil)
				}
				if c.Source == "wire" || c.Source == "repeater" || c.Source == "comparator" {
					tx.SetBlock(cube.Pos{0, 65, c.Side * 2}, RedstoneBlock{}, nil)
				}
				if c.Source == "parallel_repeater" {
					tx.SetBlock(cube.Pos{-1, 65, c.Side}, RedstoneBlock{}, nil)
				}
				if c.Source == "powered_stone" {
					tx.SetBlock(cube.Pos{0, 65, c.Side * 2}, Lever{Facing: direction.Face(), Powered: true}, nil)
				}
			})
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				if i < 16 {
					continue
				}
				<-w.Exec(func(tx *world.Tx) {
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{1, 65, 0}, sample.Output)
					comparator := tx.Block(cube.Pos{0, 65, 0}).(RedstoneComparator)
					name, _ := comparator.EncodeBlock()
					if name != sample.Comparator.ID || int(comparator.Output) != sample.Output.States.Signal || comparator.Lit != sample.Comparator.States.Lit || comparator.Subtract != sample.Comparator.States.Subtract {
						t.Errorf("tick %d comparator=%s output=%d, native=%s output=%d", sample.Tick, name, comparator.Output, sample.Comparator.ID, sample.Output.States.Signal)
					}
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{-1, 65, 0}, sample.Rear)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{0, 65, c.Side}, sample.Side)
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
