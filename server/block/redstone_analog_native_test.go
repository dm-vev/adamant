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

func TestCampfireAnalogDispatchMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_analog_dispatch.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete bool
		Methods  map[string]struct{ Result int }
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Methods) != 2 || reference.Methods["hasComparatorSignal"].Result != 0 || reference.Methods["getComparatorSignal"].Result != 0 {
		t.Fatal("invalid executed-client analog dispatch capture")
	}
	for _, fire := range FireTypes() {
		for _, extinguished := range []bool{false, true} {
			for filled := 0; filled <= 4; filled++ {
				t.Run(fmt.Sprintf("%v/extinguished%v/filled%d", fire, extinguished, filled), func(t *testing.T) {
					w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End}.New()
					t.Cleanup(func() { _ = w.Close() })
					<-w.Exec(func(tx *world.Tx) {
						pos := cube.Pos{0, 65, 0}
						c := Campfire{Type: fire, Extinguished: extinguished}
						for i := 0; i < filled; i++ {
							c.Items[i].Item = item.NewStack(item.Beef{}, 1)
						}
						tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
						tx.SetBlock(pos, c, nil)
						if output, supported := comparatorOverride(tx, pos, cube.FaceWest); supported || output != 0 {
							t.Errorf("analog supported=%v output=%d, executed client supports=false output=0", supported, output)
						}
					})
				})
			}
		}
	}
}

func TestAnalogBlockOutputsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_analog_blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block  string
				States map[string]any
				Phase  int
			}
			Samples []struct {
				Tick                     int64
				Source, Comparator, Wire torchNetworkBlock
			}
			Changes []struct {
				Tick int64
				Kind string
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 77 {
		t.Fatal("incomplete native analog-block capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%v", c.Block, c.States), func(t *testing.T) {
			if len(trace.Samples) != 16 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native analog-block trace")
			}
			source := analogNativeSource(t, c.Block, c.States)
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 2; x++ {
					for z := -1; z <= 1; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{-1, 65, 0}, source, nil)
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneComparator{Facing: cube.West}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneWire{}, nil)
			})
			// This matrix checks scalar outputs in the final eight frames.
			// Pulse sampling and publication are tested by separate full traces.
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				if i < 8 {
					continue
				}
				<-w.Exec(func(tx *world.Tx) {
					compareComparatorNativeBlock(t, tx, sample.Tick, cube.Pos{0, 65, 0}, sample.Comparator)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{1, 65, 0}, sample.Wire)
					compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{-1, 65, 0}, sample.Source)
					if output := tx.Block(cube.Pos{0, 65, 0}).(RedstoneComparator).Output; int(output) != sample.Wire.States.Signal {
						t.Errorf("output=%d, native %d", output, sample.Wire.States.Signal)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

func analogNativeSource(t *testing.T, name string, states map[string]any) world.Block {
	t.Helper()
	number := func(key string) int {
		v, ok := states[key].(float64)
		if !ok {
			t.Fatalf("missing numeric state %s", key)
		}
		return int(v)
	}
	oxidation := UnoxidisedOxidation()
	switch {
	case strings.Contains(name, "exposed_"):
		oxidation = ExposedOxidation()
	case strings.Contains(name, "weathered_"):
		oxidation = WeatheredOxidation()
	case strings.Contains(name, "oxidized_"):
		oxidation = OxidisedOxidation()
	}
	switch {
	case name == "beehive":
		return Beehive{HoneyLevel: number("honey_level")}
	case name == "bee_nest":
		return BeeNest{Beehive: Beehive{HoneyLevel: number("honey_level")}}
	case strings.HasSuffix(name, "copper_bulb"):
		lit, ok := states["lit"].(bool)
		if !ok {
			t.Fatal("missing bulb lit state")
		}
		return CopperBulb{Oxidation: oxidation, Waxed: strings.HasPrefix(name, "waxed_"), Lit: lit}
	case name == "cauldron":
		liquid := CauldronWater
		switch states["cauldron_liquid"] {
		case "water":
		case "lava":
			liquid = CauldronLava
		case "powder_snow":
			liquid = CauldronPowderSnow
		default:
			t.Fatal("unknown cauldron liquid")
		}
		return Cauldron{Liquid: liquid, FillLevel: number("fill_level")}
	case name == "cake":
		return Cake{Bites: number("bite_counter")}
	case name == "candle_cake":
		return CandleCake{}
	case name == "composter":
		return Composter{Level: number("composter_fill_level")}
	case name == "respawn_anchor":
		return RespawnAnchor{Charge: number("respawn_anchor_charge")}
	case name == "end_portal_frame":
		eye, ok := states["end_portal_eye_bit"].(bool)
		if !ok {
			t.Fatal("missing portal eye bit")
		}
		return EndPortalFrame{Eye: eye}
	case strings.HasSuffix(name, "copper_golem_statue"):
		return CopperGolemStatue{Waxed: strings.HasPrefix(name, "waxed_"), Oxidation: oxidation}
	default:
		t.Fatalf("unknown native source %q", name)
		return nil
	}
}
