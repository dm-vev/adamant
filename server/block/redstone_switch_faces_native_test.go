package block

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSwitchOutputFacesMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_switch_faces.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Direction      string
				Facing, Output, Phase int
				Powered, Conducted    bool
			}
			Samples []struct {
				Tick                      int64
				Source, Output, Conductor torchNetworkBlock
			}
			Changes []struct{ Tick int64 }
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 480 {
		t.Fatal("incomplete native switch face capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/facing%d/out%d/conducted%v/powered%v", c.Block, c.Direction, c.Facing, c.Output, c.Conducted, c.Powered), func(t *testing.T) {
			if len(trace.Samples) != 24 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native switch face trace")
			}
			var source world.Block
			if c.Block == "lever" {
				d := cube.North
				if strings.HasSuffix(c.Direction, "east_west") {
					d = cube.West
				}
				source = Lever{Facing: cube.Face(c.Facing), Direction: d, Powered: c.Powered}
			} else {
				for _, kind := range ButtonTypes() {
					if kind.blockName() == c.Block {
						source = Button{Type: kind, Facing: cube.Face(c.Facing), Pressed: c.Powered}
						break
					}
				}
				if source == nil {
					t.Fatalf("unknown native button %s", c.Block)
				}
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos := cube.Pos{0, 65, 0}
			conductor := pos.Side(cube.Face(c.Output))
			output := conductor
			if c.Conducted {
				output = output.Side(cube.Face(c.Output))
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(cube.Face(c.Facing).Opposite()), Stone{}, nil)
				if c.Conducted {
					tx.SetBlock(conductor, Stone{}, nil)
				}
				tx.SetBlock(output, RedstoneLamp{}, nil)
				tx.SetBlock(pos, source, nil)
			})
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				if i < 16 {
					continue
				}
				<-w.Exec(func(tx *world.Tx) {
					compareTorchNetworkBlock(t, tx, sample.Tick, pos, sample.Source)
					compareTorchNetworkBlock(t, tx, sample.Tick, output, sample.Output)
					compareTorchNetworkBlock(t, tx, sample.Tick, conductor, sample.Conductor)
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
