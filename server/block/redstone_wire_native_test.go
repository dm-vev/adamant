package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestWireTimingMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_wire_timing.json")
	if err != nil {
		t.Fatal(err)
	}
	type nativeBlock struct {
		ID     string
		States struct {
			Signal  int  `json:"redstone_signal"`
			Powered bool `json:"powered_bit"`
		}
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Source, Output string
				Width, Phase   int
			}
			Samples []struct {
				Tick           int64
				Source, Output nativeBlock
				Wires          []nativeBlock
			}
			Changes []struct {
				Tick  int64
				Pos   cube.Pos
				Block string
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 48 {
		t.Fatal("incomplete native wire timing capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/width%d/phase%d", c.Source, c.Output, c.Width, c.Phase), func(t *testing.T) {
			if len(trace.Samples) == 0 || len(trace.Changes) != 2 {
				t.Fatal("invalid native trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -4; x <= 6; x++ {
					for z := -2; z <= 2; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				switch c.Source {
				case "observer":
					tx.SetBlock(cube.Pos{-1, 65, 0}, Observer{Facing: cube.FaceWest}, nil)
				case "repeater":
					tx.SetBlock(cube.Pos{-1, 65, 0}, RedstoneRepeater{Facing: cube.West}, nil)
				case "block":
				default:
					t.Errorf("unknown source %s", c.Source)
				}
				for x := 0; x < 4; x++ {
					tx.SetBlock(cube.Pos{x, 65, 0}, RedstoneWire{}, nil)
				}
				var output world.Block = RedstoneLamp{}
				if c.Output == "repeater" {
					output = RedstoneRepeater{Facing: cube.West}
				}
				tx.SetBlock(cube.Pos{4, 65, 0}, output, nil)
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					for i, expected := range sample.Wires {
						r, ok := tx.Block(cube.Pos{i, 65, 0}).(RedstoneWire)
						if !ok || expected.ID != "minecraft:redstone_wire" || r.Power != expected.States.Signal {
							t.Errorf("tick %d wire %d: power=%d, native=%d", sample.Tick, i, r.Power, expected.States.Signal)
						}
					}
					for _, block := range []struct {
						Pos      cube.Pos
						Expected nativeBlock
					}{{cube.Pos{-1, 65, 0}, sample.Source}, {cube.Pos{4, 65, 0}, sample.Output}} {
						got := tx.Block(block.Pos)
						id, _ := got.EncodeBlock()
						if id != block.Expected.ID {
							t.Errorf("tick %d block %v=%s, native=%s", sample.Tick, block.Pos, id, block.Expected.ID)
						}
						if o, ok := got.(Observer); ok && o.Powered != block.Expected.States.Powered {
							t.Errorf("tick %d observer powered=%v, native=%v", sample.Tick, o.Powered, block.Expected.States.Powered)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick != sample.Tick {
							continue
						}
						var block world.Block = Air{}
						switch change.Block {
						case "stone":
							block = Stone{}
						case "redstone_block":
							block = RedstoneBlock{}
						case "air":
						default:
							t.Errorf("unknown block %s", change.Block)
						}
						tx.SetBlock(change.Pos, block, nil)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
