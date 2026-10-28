package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPoweredRailStraightMatchesBDS(t *testing.T) {
	testPoweredRailNative(t, "bedrock_1_26_51_powered_rail_straight.json", 32)
}

func TestPoweredRailInputsMatchBDS(t *testing.T) {
	testPoweredRailNative(t, "bedrock_1_26_51_powered_rail_input.json", 32)
}

func TestPoweredRailTopologyMatchesBDS(t *testing.T) {
	testPoweredRailNative(t, "bedrock_1_26_51_powered_rail_topology.json", 32)
}

func TestRailInputFacesMatchBDS(t *testing.T) {
	testPoweredRailNative(t, "bedrock_1_26_51_rail_input_faces.json", 36)
}

func TestRailSlopeFacesMatchBDS(t *testing.T) {
	testPoweredRailNative(t, "bedrock_1_26_51_rail_slope_faces.json", 24)
}

func TestMixedRailPowerMatchesBDS(t *testing.T) {
	testPoweredRailNative(t, "bedrock_1_26_51_mixed_rail.json", 20)
}

func testPoweredRailNative(t *testing.T, name string, count int) {
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Axis, Input, Topology string
				Length, Phase                int
				Face                         int
				MiddlePowered                bool `json:"middle_powered"`
			}
			SetupTick      int64 `json:"setup_tick"`
			Source         cube.Pos
			SourceOff      string `json:"source_off"`
			Input          *cube.Pos
			RailPositions  []cube.Pos      `json:"rail_positions"`
			RailDirections []RailDirection `json:"rail_directions"`
			RailBlocks     []string        `json:"rail_blocks"`
			Supports       []cube.Pos
			ExtraRails     []struct {
				Position  cube.Pos
				Direction RailDirection
			} `json:"extra_rails"`
			Changes []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick   int64
				Source nativeTripwireBlock
				Input  *nativeTripwireBlock
				Rails  []nativeTripwireBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native rail capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/length%d/phase%d/%s/%s/face%d", c.Block, c.Axis, c.Length, c.Phase, c.Input, c.Topology, c.Face), func(t *testing.T) {
			if len(trace.RailPositions) != c.Length || len(trace.Samples) != 80 || len(trace.Changes) != 2 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native rail trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			<-w.Exec(func(tx *world.Tx) {
				base := trace.RailPositions[0]
				loader.Move(tx, base.Vec3Centre())
				loader.Load(tx, 25)
				for x := base[0] - 2; x <= base[0]+18; x++ {
					for z := base[2] - 2; z <= base[2]+18; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				dir := RailNorthSouth
				if c.Axis == "x" {
					dir = RailEastWest
				}
				for _, p := range trace.Supports {
					tx.SetBlock(p, Stone{}, nil)
				}
				if trace.Input != nil {
					switch c.Input {
					case "wire":
						tx.SetBlock(*trace.Input, RedstoneWire{}, nil)
					case "repeater":
						facing := cube.North
						if c.Axis == "x" {
							facing = cube.West
						}
						tx.SetBlock(*trace.Input, RedstoneRepeater{Facing: facing}, nil)
					default:
						t.Fatal("unexpected rail input", c.Input)
					}
				}
				placeRail := func(p cube.Pos, dir RailDirection, kind string) {
					switch kind {
					case "golden_rail":
						tx.SetBlock(p, PoweredRail{Direction: dir}, nil)
					case "activator_rail":
						tx.SetBlock(p, ActivatorRail{Direction: dir}, nil)
					case "rail":
						tx.SetBlock(p, Rail{Direction: dir}, nil)
					case "detector_rail":
						tx.SetBlock(p, DetectorRail{Direction: dir, Powered: c.MiddlePowered}, nil)
					default:
						t.Fatal("unexpected rail kind", c.Block)
					}
				}
				for _, extra := range trace.ExtraRails {
					placeRail(extra.Position, extra.Direction, c.Block)
				}
				if len(trace.RailDirections) != 0 && len(trace.RailDirections) != len(trace.RailPositions) {
					t.Fatal("invalid native rail directions")
				}
				for i, p := range trace.RailPositions {
					shape := dir
					if len(trace.RailDirections) != 0 {
						shape = trace.RailDirections[i]
					}
					kind := c.Block
					if len(trace.RailBlocks) != 0 {
						if len(trace.RailBlocks) != len(trace.RailPositions) {
							t.Fatal("invalid rail family metadata")
						}
						kind = trace.RailBlocks[i]
					}
					placeRail(p, shape, kind)
				}
				// The native script applies this same-ID shape edit after placing
				// the crossing. Its already configured scene axis is retained.
				if c.Topology == "wrong_axis" {
					placeRail(trace.RailPositions[4], trace.RailDirections[4], c.Block)
				}
				if trace.SourceOff == "minecraft:stone" {
					tx.SetBlock(trace.Source, Stone{}, nil)
				}
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick || len(frame.Rails) != len(trace.RailPositions) {
						t.Fatal("rail replay clock/cell mismatch")
					}
					compareSculkContactNativeBlock(t, tx, frame.Tick, trace.Source, frame.Source)
					if trace.Input != nil {
						if frame.Input == nil {
							t.Fatal("missing native input sample")
						}
						compareSculkContactNativeBlock(t, tx, frame.Tick, *trace.Input, *frame.Input)
					}
					for i, p := range trace.RailPositions {
						compareSculkContactNativeBlock(t, tx, frame.Tick, p, frame.Rails[i])
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block = Air{}
							if trace.SourceOff == "minecraft:stone" {
								source = Stone{}
							}
							if change.Powered {
								source = RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, source, nil)
						}
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
