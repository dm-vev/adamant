package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestTorchRotationMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_torch_rotation.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Method      string
				WestPowered bool
				Phase       int
				NorthLamp   bool
			}
			Samples []struct {
				Tick                     int64
				Torch, Lamp, West, North torchNetworkBlock
				NorthSupport             torchNetworkBlock `json:"north_support"`
			}
			Changes []struct {
				Tick                int64
				Kind, Block, Facing string
				Pos                 cube.Pos
				Powered             bool
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 24 {
		t.Fatal("incomplete native torch rotation capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/west%v/phase%d/northLamp%v", c.Method, c.WestPowered, c.Phase, c.NorthLamp), func(t *testing.T) {
			if len(trace.Samples) != 64 || len(trace.Changes) < 7 || trace.Changes[0].Kind != "setup" {
				t.Fatal("incomplete native rotation trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(cube.Pos{-1, 65, 0}, Stone{}, nil)
				var northSupport world.Block = Stone{}
				if c.NorthLamp {
					northSupport = RedstoneLamp{}
				}
				tx.SetBlock(cube.Pos{0, 65, -1}, northSupport, nil)
				tx.SetBlock(cube.Pos{0, 66, 0}, RedstoneLamp{}, nil)
				tx.SetBlock(cube.Pos{-2, 65, 0}, Lever{Facing: cube.FaceWest, Powered: c.WestPowered}, nil)
				if !c.NorthLamp {
					tx.SetBlock(cube.Pos{0, 65, -2}, Lever{Facing: cube.FaceNorth, Powered: !c.WestPowered}, nil)
				}
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneTorch{Facing: cube.FaceWest, Lit: true}, nil)
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Errorf("world tick %d, native %d", tx.CurrentTick(), sample.Tick)
					}
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{0, 65, 0}, sample.Torch)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{0, 66, 0}, sample.Lamp)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{-2, 65, 0}, sample.West)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{0, 65, -2}, sample.North)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{0, 65, -1}, sample.NorthSupport)
					if sample.Torch.States.Facing != "" {
						_, states := tx.Block(cube.Pos{0, 65, 0}).EncodeBlock()
						if states["torch_facing_direction"] != sample.Torch.States.Facing {
							t.Errorf("tick %d facing=%v, native=%s", sample.Tick, states["torch_facing_direction"], sample.Torch.States.Facing)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick || change.Kind == "setup" {
							continue
						}
						facing := cube.FaceNorth
						if change.Facing == "west" {
							facing = cube.FaceWest
						}
						var b world.Block
						switch change.Kind {
						case "lever":
							b = Lever{Facing: facing, Powered: change.Powered}
						case "torch":
							if change.Block == "air" {
								b = Air{}
							} else if change.Block == "redstone_torch" || change.Block == "unlit_redstone_torch" {
								b = RedstoneTorch{Facing: facing, Lit: change.Block == "redstone_torch"}
							} else {
								t.Errorf("unknown native torch identifier %q", change.Block)
								continue
							}
						default:
							t.Errorf("unknown native change kind %q", change.Kind)
							continue
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
