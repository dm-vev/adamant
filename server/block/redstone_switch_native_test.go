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

func TestSwitchStatesMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_switch_states.json")
	if err != nil {
		t.Fatal(err)
	}
	type producer struct {
		Power     int
		Strong    string
		Connected []string
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Direction string
				Facing, Phase    int
				Powered          bool
			}
			Initial producer `json:"initial_producer"`
			Final   producer `json:"final_producer"`
			Samples []struct {
				Tick   int64
				Source struct {
					ID     string
					States struct {
						Pressed bool `json:"button_pressed_bit"`
						Powered bool `json:"open_bit"`
					}
				}
				Wire torchNetworkBlock
			}
			Changes []struct{ Tick int64 }
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 196 {
		t.Fatal("incomplete native switch capture")
	}
	faces := map[string]cube.Face{"Down": cube.FaceDown, "Up": cube.FaceUp, "North": cube.FaceNorth, "South": cube.FaceSouth, "West": cube.FaceWest, "East": cube.FaceEast}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/facing%d/%s/powered%v", c.Block, c.Facing, c.Direction, c.Powered), func(t *testing.T) {
			if len(trace.Samples) != 80 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native switch trace")
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
			pos, wirePos := cube.Pos{0, 65, 0}, cube.Pos{1, 65, 0}
			if c.Facing == 4 {
				wirePos[0] = -1
			}
			checkProducer := func(tx *world.Tx, native producer) {
				t.Helper()
				b := tx.Block(pos)
				weak, strong := b.(world.RedstonePowerSource), b.(world.RedstoneStrongPowerSource)
				if len(native.Connected) != 6 {
					t.Error("native switch connection list incomplete")
					return
				}
				strongFace, ok := faces[native.Strong]
				if !ok {
					t.Errorf("unknown native strong face %s", native.Strong)
					return
				}
				for _, name := range native.Connected {
					face, ok := faces[name]
					if !ok {
						t.Errorf("unknown native connected face %s", name)
						continue
					}
					if got := weak.RedstonePower(pos, tx, face); got != native.Power {
						t.Errorf("weak face %s=%d native=%d", name, got, native.Power)
					}
					want := 0
					if face == strongFace {
						want = native.Power
					}
					if got := strong.RedstoneStrongPower(pos, tx, face); got != want {
						t.Errorf("strong face %s=%d native=%d", name, got, want)
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 2; x++ {
					for z := -1; z <= 1; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(pos.Side(cube.Face(c.Facing).Opposite()), Stone{}, nil)
				tx.SetBlock(pos, source, nil)
				tx.SetBlock(wirePos, RedstoneWire{}, nil)
				checkProducer(tx, trace.Initial)
			})
			if t.Failed() {
				return
			}
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					b := tx.Block(pos)
					id, _ := b.EncodeBlock()
					if id != sample.Source.ID {
						t.Errorf("tick %d source=%s native=%s", sample.Tick, id, sample.Source.ID)
						return
					}
					switch b := b.(type) {
					case Button:
						if b.Pressed != sample.Source.States.Pressed {
							t.Errorf("tick %d pressed=%v native=%v", sample.Tick, b.Pressed, sample.Source.States.Pressed)
						}
					case Lever:
						if b.Powered != sample.Source.States.Powered {
							t.Errorf("tick %d powered=%v native=%v", sample.Tick, b.Powered, sample.Source.States.Powered)
						}
					}
					compareTorchNetworkBlock(t, tx, sample.Tick, wirePos, sample.Wire)
					if i == len(trace.Samples)-1 {
						checkProducer(tx, trace.Final)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
