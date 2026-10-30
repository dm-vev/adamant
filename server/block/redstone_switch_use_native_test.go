package block

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestSwitchPlayerUseMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_switch_use.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Pattern, Direction string
				Facing                    int
			}
			Samples []struct {
				Tick         int64
				GameTick     int64 `json:"game_tick"`
				Source, Wire torchNetworkBlock
			}
			Changes []struct {
				Tick         int64
				GameTick     int64 `json:"game_tick"`
				BeforeSample bool  `json:"before_sample"`
				Before       torchNetworkBlock
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 66 {
		t.Fatal("incomplete native player-use capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/facing%d/%s", c.Block, c.Pattern, c.Facing, c.Direction), func(t *testing.T) {
			expected := 1
			if c.Pattern == "repeat" {
				expected = 2
			}
			if len(trace.Samples) < 65 || len(trace.Changes) != expected || trace.Samples[0].GameTick == 0 {
				t.Fatal("invalid native player-use trace")
			}
			var source world.Block
			if c.Block == "lever" {
				d := cube.North
				if strings.HasSuffix(c.Direction, "east_west") {
					d = cube.West
				}
				source = Lever{Facing: cube.Face(c.Facing), Direction: d}
			} else {
				for _, kind := range ButtonTypes() {
					if kind.blockName() == c.Block {
						source = Button{Facing: cube.Face(c.Facing), Type: kind}
						break
					}
				}
				if source == nil {
					t.Fatal("unknown native button")
				}
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos, wirePos := cube.Pos{0, 65, 0}, cube.Pos{1, 65, 0}
			if c.Facing == 4 {
				wirePos[0] = -1
			}
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 3; x++ {
					for z := -3; z <= 3; z++ {
						tx.SetBlock(cube.Pos{x, 63, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(pos.Side(cube.Face(c.Facing).Opposite()), Stone{}, nil)
				tx.SetBlock(wirePos.Side(cube.FaceDown), Stone{}, nil)
				tx.SetBlock(pos, source, nil)
				tx.SetBlock(wirePos, RedstoneWire{}, nil)
			})
			applied := 0
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					compareTorchNetworkBlock(t, tx, sample.Tick, pos, sample.Source)
					compareTorchNetworkBlock(t, tx, sample.Tick, wirePos, sample.Wire)
					if b, ok := tx.Block(pos).(Button); ok {
						if b.Pressed != sample.Source.States.Pressed {
							t.Errorf("tick %d pressed=%v native=%v", sample.Tick, b.Pressed, sample.Source.States.Pressed)
						}
					}
					for _, change := range trace.Changes {
						// Network input uses the game clock before the next
						// interval snapshot. SDK event delivery has its own tick
						// label, so use the independently captured game time.
						if sample.GameTick != change.GameTick {
							continue
						}
						switch before := tx.Block(pos).(type) {
						case Button:
							if before.Pressed != change.Before.States.Pressed {
								t.Errorf("native click precondition differs at tick %d", change.Tick)
							}
						case Lever:
							if before.Powered != change.Before.States.Powered {
								t.Errorf("native lever precondition differs at tick %d", change.Tick)
							}
						}
						b := tx.Block(pos).(interface {
							Activate(cube.Pos, cube.Face, *world.Tx, item.User, *item.UseContext) bool
						})
						b.Activate(pos, cube.Face(c.Facing), tx, nil, nil)
						applied++
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != expected {
				t.Errorf("applied %d native inputs, expected %d", applied, expected)
			}
		})
	}
}
