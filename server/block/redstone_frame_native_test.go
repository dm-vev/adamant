package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestFrameSignalsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_frame_signal.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block                          string
				Through                        bool
				Facing, Rotation, Phase, Query int
			}
			Samples []struct {
				Tick                     int64
				Source, Comparator, Wire torchNetworkBlock
			}
			Changes []struct{ Tick int64 }
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 792 {
		t.Fatal("incomplete native frame signal capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/query%d/through%v/facing%d/rotation%d", c.Block, c.Query, c.Through, c.Facing, c.Rotation), func(t *testing.T) {
			if len(trace.Samples) != 16 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native frame signal trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			comparatorPos := cube.Pos{0, 65, 0}
			inputFace := cube.Face(c.Query)
			pos := comparatorPos.Side(inputFace)
			wirePos := comparatorPos.Side(inputFace.Opposite())
			if c.Through {
				pos = pos.Side(inputFace)
			}
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 3; x++ {
					for z := -3; z <= 3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				frame := ItemFrame{Facing: cube.Face(c.Facing).Opposite(), Glowing: c.Block == "glow_frame", DropChance: 1, Rotations: max(c.Rotation, 0)}
				if c.Rotation >= 0 {
					frame.Item = item.NewStack(Stone{}, 1)
				}
				tx.SetBlock(pos.Side(frame.Facing), Stone{}, nil)
				if c.Through {
					tx.SetBlock(comparatorPos.Side(inputFace), Stone{}, nil)
				}
				tx.SetBlock(comparatorPos, RedstoneComparator{Facing: inputFace.Direction()}, nil)
				tx.SetBlock(wirePos, RedstoneWire{}, nil)
				tx.SetBlock(pos, frame, nil)
			})
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				if i < 8 {
					continue
				}
				<-w.Exec(func(tx *world.Tx) {
					compareComparatorNativeBlock(t, tx, sample.Tick, comparatorPos, sample.Comparator)
					compareTorchNetworkBlock(t, tx, sample.Tick, wirePos, sample.Wire)
					compareTorchNetworkBlock(t, tx, sample.Tick, pos, sample.Source)
					frame, ok := tx.Block(pos).(ItemFrame)
					if !ok || int(frame.Facing.Opposite()) != c.Facing {
						t.Error("frame lost its native-facing support")
					}
					got := tx.Block(comparatorPos).(RedstoneComparator).Output
					if int(got) != sample.Wire.States.Signal {
						t.Errorf("output=%d native=%d", got, sample.Wire.States.Signal)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
