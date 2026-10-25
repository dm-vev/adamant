package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestRedstoneWorldMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_redstone_world.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool `json:"complete"`
		Scenarios []struct {
			Kind            string   `json:"kind"`
			Name            string   `json:"name"`
			Power           cube.Pos `json:"power"`
			Extended        bool     `json:"extended"`
			Rear, Side      int
			Lit             bool   `json:"lit"`
			Block           string `json:"block"`
			FacingDirection int    `json:"facing_direction"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 49 {
		t.Fatalf("incomplete native capture: %d scenarios", len(reference.Scenarios))
	}
	for _, scenario := range reference.Scenarios {
		t.Run(scenario.Kind+"/"+scenario.Name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				opts := &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true}
				pos := cube.Pos{0, 65, 0}
				switch scenario.Kind {
				case "piston_power":
					faces := []cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceSouth, cube.FaceNorth, cube.FaceEast, cube.FaceWest}
					piston := Piston{Facing: faces[scenario.FacingDirection]}
					tx.SetBlock(pos, piston, opts)
					tx.SetBlock(scenario.Power, RedstoneBlock{}, opts)
					if got := piston.shouldExtend(pos, tx); got != scenario.Extended {
						t.Errorf("piston accepts power at %v: %v, BDS extends: %v", scenario.Power, got, scenario.Extended)
					}
				case "comparator_subtract_equal":
					if scenario.Rear != 15 || scenario.Side != 15 {
						t.Fatal("unsupported captured input fixture")
					}
					comparator := RedstoneComparator{Facing: cube.West, Subtract: true}
					for x := -2; x <= 2; x++ {
						for z := -2; z <= 3; z++ {
							tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, opts)
						}
					}
					tx.SetBlock(pos, comparator, opts)
					tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, opts)
					tx.SetBlock(pos.Side(cube.FaceSouth), RedstoneRepeater{Facing: cube.South}, opts)
					tx.SetBlock(cube.Pos{0, 65, 2}, RedstoneBlock{}, opts)
				default:
					t.Fatalf("unhandled native scenario %q", scenario.Kind)
				}
			})
			if scenario.Kind == "comparator_subtract_equal" {
				for range 16 {
					w.AdvanceTick()
				}
				<-w.Exec(func(tx *world.Tx) {
					pos := cube.Pos{0, 65, 0}
					got := tx.Block(pos).(RedstoneComparator)
					name, _ := got.EncodeBlock()
					if got.Lit != scenario.Lit || name != "minecraft:"+scenario.Block || got.Output != 0 {
						t.Errorf("subtract comparator = %+v (%s), BDS = %s lit=%v", got, name, scenario.Block, scenario.Lit)
					}
				})
			}
		})
	}
}

func TestComparatorArithmeticMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_redstone_evaluators.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Comparators struct {
			Cases       [][6]int `json:"cases"`
			DirectCases [][4]int `json:"direct_cases"`
		} `json:"comparators"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.Comparators.Cases) != 8192 || len(reference.Comparators.DirectCases) != 512 {
		t.Fatal("incomplete native comparator capture")
	}
	for _, example := range reference.Comparators.Cases {
		mode, rear, left, right, expected := example[0], example[1], example[2], example[3], example[4]
		if got := comparatorTargetOutput(rear, max(left, right), mode != 0); int(got) != expected {
			t.Fatalf("mode=%d rear=%d sides=%d/%d: got %d, native %d", mode, rear, left, right, got, expected)
		}
	}
	for _, example := range reference.Comparators.DirectCases {
		if got := comparatorTargetOutput(example[1], example[2], example[0] != 0); int(got) != example[3] {
			t.Fatalf("direct input %v: got %d, native %d", example[:3], got, example[3])
		}
	}
}

func TestSimpleCircuitsMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_redstone_evaluators.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Simple struct{ Pulses, Transporters [][4]int } `json:"simple_capacitors"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.Simple.Pulses) != 256 || len(reference.Simple.Transporters) != 256 {
		t.Fatal("incomplete native simple capacitor capture")
	}
	for _, example := range reference.Simple.Pulses {
		state := observerCircuit{strength: example[0], next: example[1]}
		next, _ := state.Evaluate()
		got := next.(observerCircuit)
		if got.strength != example[2] || got.next != example[3] {
			t.Fatalf("native pulse %v: strength=%d next=%d", example, got.strength, got.next)
		}
	}
	for _, example := range reference.Simple.Transporters {
		state := wireCircuit{wire: RedstoneWire{Power: example[0]}, next: example[1]}
		next, output := state.Evaluate()
		if got := next.(wireCircuit).wire.Power; got != example[2] || (output != nil) != (example[3] != 0) {
			t.Fatalf("native transporter %v: power=%d changed=%v", example, got, output != nil)
		}
	}
}

func TestRepeaterSideLockMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_repeater_lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool `json:"complete"`
		Scenarios []struct {
			SideSource string `json:"side_source"`
			Powered    bool   `json:"powered"`
			Rear       int    `json:"rear"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 6 {
		t.Fatal("incomplete native repeater lock capture")
	}
	for _, scenario := range reference.Scenarios {
		t.Run(scenario.SideSource, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				if scenario.Rear != 15 {
					t.Fatal("unsupported captured rear input")
				}
				pos := cube.Pos{0, 65, 0}
				opts := &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true}
				repeater := RedstoneRepeater{Facing: cube.West}
				tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, opts)
				tx.SetBlock(pos.Side(cube.FaceSouth).Side(cube.FaceDown), Stone{}, opts)
				tx.SetBlock(pos, repeater, opts)
				tx.SetBlock(pos.Side(cube.FaceWest), RedstoneBlock{}, opts)
				var side world.Block
				switch scenario.SideSource {
				case "redstone_block":
					side = RedstoneBlock{}
				case "wire":
					side = RedstoneWire{Power: 15}
				case "torch":
					side = RedstoneTorch{Facing: cube.FaceDown, Lit: true}
				case "repeater":
					side = RedstoneRepeater{Facing: cube.South, Powered: true}
				case "comparator":
					side = RedstoneComparator{Facing: cube.South, Powered: true, Output: 15}
				case "parallel_repeater":
					side = RedstoneRepeater{Facing: cube.West, Powered: true}
				default:
					t.Fatalf("unknown captured side source %q", scenario.SideSource)
				}
				tx.SetBlock(pos.Side(cube.FaceSouth), side, opts)
				if locked := repeater.isLocked(pos, tx); locked == scenario.Powered {
					t.Errorf("side source %s: locked=%v, BDS powered=%v with rear input 15", scenario.SideSource, locked, scenario.Powered)
				}
				state := repeater.RedstoneCircuit().Cache(pos, tx)
				_, output := state.Evaluate()
				got := repeater.Powered
				if output != nil {
					got = output.(RedstoneRepeater).Powered
				}
				if got != scenario.Powered {
					t.Errorf("side source %s: powered=%v, BDS=%v", scenario.SideSource, got, scenario.Powered)
				}
			})
		})
	}
}

func TestRepeaterPipelineMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_redstone_evaluators.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Repeaters []struct {
			Delay         int `json:"delay"`
			Input, Output []int
		} `json:"repeaters"`
		Transitions struct {
			Traces []struct {
				InitialPowered bool     `json:"initial_powered"`
				Steps          [][4]int `json:"steps"`
			} `json:"traces"`
		} `json:"repeater_transitions"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if len(reference.Repeaters) != 16 {
		t.Fatal("incomplete native repeater capture")
	}
	for n, trace := range reference.Repeaters {
		s := newRepeaterCircuit(false)
		if len(trace.Input) != len(trace.Output) {
			t.Fatal("invalid native trace length")
		}
		for i, input := range trace.Input {
			if got := s.step(input != 0, trace.Delay, false); got != (trace.Output[i] != 0) {
				t.Fatalf("trace %d delay %d step %d: powered=%v, native=%d", n, trace.Delay, i, got, trace.Output[i])
			}
		}
	}
	if len(reference.Transitions.Traces) != 128 {
		t.Fatal("incomplete native transition capture")
	}
	for n, trace := range reference.Transitions.Traces {
		s := newRepeaterCircuit(trace.InitialPowered)
		if len(trace.Steps) != 96 {
			t.Fatal("incomplete native transition trace")
		}
		for i, step := range trace.Steps {
			if got := s.step(step[1] != 0, step[0], step[2] != 0); got != (step[3] != 0) {
				t.Fatalf("transition trace %d step %d delay=%d input=%d locked=%d: got %v, native %d", n, i, step[0], step[1], step[2], got, step[3])
			}
		}
	}
}

func TestRepeaterTimingMatchesNativeCapture(t *testing.T) {
	testRepeaterNativeTiming(t, "testdata/bedrock_1_26_51_repeater_timing.json", 48)
}

func TestRepeaterStateMatchesNativeCapture(t *testing.T) {
	testRepeaterNativeTiming(t, "testdata/bedrock_1_26_51_repeater_state.json", 40)
}

func TestRepeaterDelayMatchesNativeCapture(t *testing.T) {
	testRepeaterNativeTiming(t, "testdata/bedrock_1_26_51_repeater_delay.json", 8)
}

func testRepeaterNativeTiming(t *testing.T, fixture string, count int) {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	type nativeBlock struct {
		ID string `json:"id"`
	}
	var reference struct {
		Complete  bool `json:"complete"`
		Scenarios []struct {
			Scenario struct {
				Name                string
				Delay, Width, Phase int
			} `json:"scenario"`
			Samples []struct {
				Tick                int64 `json:"tick"`
				First, Second, Side nativeBlock
			} `json:"samples"`
			Changes []struct {
				Tick    int64
				Powered bool
				Kind    string
				Value   int
			} `json:"changes"`
		} `json:"scenarios"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != count {
		t.Fatal("incomplete native timing capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/delay%d/width%d/phase%d", c.Name, c.Delay, c.Width, c.Phase), func(t *testing.T) {
			if len(trace.Samples) == 0 || len(trace.Changes) < 2 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native trace")
			}
			settings := (world.NopProvider{}).Settings()
			// Config.New performs an initial tick before returning the world.
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, Dim: world.End, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 6; x++ {
					for z := -2; z <= 2; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{0, 65, 0}, RedstoneRepeater{Facing: cube.West, Delay: c.Delay}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, RedstoneRepeater{Facing: cube.West}, nil)
				if trace.Samples[0].Side.ID != "" {
					tx.SetBlock(cube.Pos{0, 65, 1}, RedstoneRepeater{Facing: cube.South}, nil)
				}
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Errorf("replay clock = %d, native tick = %d", tx.CurrentTick(), sample.Tick)
					}
					positions := []cube.Pos{{0, 65, 0}, {1, 65, 0}, {0, 65, 1}}
					for i, expected := range []nativeBlock{sample.First, sample.Second, sample.Side} {
						if expected.ID == "" {
							continue
						}
						got, ok := tx.Block(positions[i]).(RedstoneRepeater)
						if !ok || got.Powered != (expected.ID == "minecraft:powered_repeater") {
							t.Errorf("native tick %d repeater %d: powered=%v, native=%s", sample.Tick, i, got.Powered, expected.ID)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == sample.Tick {
							if change.Kind == "delay" {
								r := tx.Block(cube.Pos{0, 65, 0}).(RedstoneRepeater)
								r.Delay = change.Value
								tx.SetBlock(cube.Pos{0, 65, 0}, r, nil)
								continue
							}
							var source world.Block = Air{}
							if change.Powered {
								source = RedstoneBlock{}
							}
							pos := cube.Pos{-1, 65, 0}
							if change.Kind == "side" {
								pos = cube.Pos{0, 65, 2}
							}
							tx.SetBlock(pos, source, nil)
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
