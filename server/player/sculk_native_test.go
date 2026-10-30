package player

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/go-gl/mathgl/mgl64"
)

type sculkNativeBlock struct {
	ID     string
	States struct {
		Phase   int  `json:"sculk_sensor_phase"`
		Signal  int  `json:"redstone_signal"`
		Powered bool `json:"open_bit"`
		Pressed bool `json:"button_pressed_bit"`
		Lit     bool `json:"output_lit_bit"`
	}
	Power *int
}
type sculkNativeScenario struct {
	Sensor       string
	Distance     int
	RequestPhase int `json:"request_phase"`
	Toggle       bool
}
type sculkNativeSample struct {
	Tick                                             int64
	Source, Sensor, North, South, Comparator, Output sculkNativeBlock
}
type sculkNativeTrace struct {
	Scenario sculkNativeScenario
	Changes  []struct {
		Tick         int64
		BeforeSample bool `json:"before_sample"`
	}
	Results []json.RawMessage
	Samples []sculkNativeSample
}
type sculkNativeCapture struct {
	Complete    bool
	Scenarios   []sculkNativeTrace
	Checkpoints []struct {
		Scenario   sculkNativeScenario
		Sample     sculkNativeSample
		Components struct{ Comparator struct{ Analog int } }
	}
}

// The black-box capture does not record BDS's random stream. Replay both actor
// orders on each arrival, requiring one coherent replay to match EVERY frame,
// including comparator permutation and downstream dust. This permits only the
// native shuffle's randomness, not arbitrary output tolerances. The independent
// heap capture below also records which order's analog read actually occurred,
// so its replay has no choices to search.
func TestSculkPlayerLeverVibrationsMatchBDS(t *testing.T) {
	native := readSculkNativeCapture(t, "bedrock_1_26_51_sculk_use.json", 36)
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/d%d/phase%d/toggle%t", c.Sensor, c.Distance, c.RequestPhase, c.Toggle), func(t *testing.T) {
			var failures []error
			for variant := 0; variant < 1<<len(trace.Changes); variant++ {
				choices := make(map[int64]bool)
				for i, change := range trace.Changes {
					choices[change.Tick+int64(c.Distance)] = variant&(1<<i) != 0
				}
				if err := replaySculkNativeTrace(trace, choices); err == nil {
					return
				} else {
					failures = append(failures, err)
				}
			}
			t.Fatalf("no native actor order matches the full trace: %v", failures)
		})
	}
}

func TestSculkComparatorSamplingMatchesNativeHeap(t *testing.T) {
	native := readSculkNativeCapture(t, "bedrock_1_26_51_sculk_components.json", 4)
	if len(native.Checkpoints) != 44 {
		t.Fatal("missing native comparator checkpoints")
	}
	before, after := false, false
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/phase%d", c.Sensor, c.RequestPhase), func(t *testing.T) {
			choices := make(map[int64]bool)
			arrival := trace.Changes[0].Tick + int64(c.Distance)
			for _, checkpoint := range native.Checkpoints {
				if checkpoint.Scenario == c && checkpoint.Sample.Tick == arrival {
					analog := checkpoint.Components.Comparator.Analog
					if analog != 0 && analog != 10 {
						t.Fatalf("unexpected native rear sample %d", analog)
					}
					choices[arrival] = analog == 10
					before = before || analog == 0
					after = after || analog == 10
				}
			}
			if len(choices) != 1 {
				t.Fatal("missing native arrival checkpoint")
			}
			if err := replaySculkNativeTrace(trace, choices); err != nil {
				t.Fatal(err)
			}
		})
	}
	if !before || !after {
		t.Fatal("native capture must observe both orders of the comparator/sensor actors")
	}
}

func readSculkNativeCapture(t *testing.T, name string, count int) sculkNativeCapture {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var native sculkNativeCapture
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native sculk capture")
	}
	return native
}

// Only two actors share a chunk: comparator first, then sensor. Control the
// raw 32-bit shuffle draw explicitly. Weather and random block ticks are off.
type sculkActorRandom struct{ sensorFirst bool }

func (r *sculkActorRandom) Uint64() uint64 {
	word := uint64(0x8000000000000000)
	if !r.sensorFirst {
		word |= 1 << 32
	}
	return word
}

func replaySculkNativeTrace(trace sculkNativeTrace, choices map[int64]bool) error {
	c := trace.Scenario
	expected := 1
	if c.Toggle {
		expected = 2
	}
	if len(trace.Changes) != expected || len(trace.Results) != expected || len(trace.Samples) < 160 {
		return fmt.Errorf("missing native player input")
	}
	settings := (world.NopProvider{}).Settings()
	settings.CurrentTick = trace.Samples[0].Tick - 2
	settings.WeatherCycle = false
	random := &sculkActorRandom{}
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, RandSource: random, Provider: world.NopProvider{Set: settings}}.New()
	defer w.Close()
	var handle *world.EntityHandle
	sensor := cube.Pos{c.Distance, 65, 0}
	<-w.Exec(func(tx *world.Tx) {
		for x := -32; x <= 48; x += 16 {
			for z := -32; z <= 32; z += 16 {
				tx.Block(cube.Pos{x, 64, z})
			}
		}
		for x := -4; x <= 22; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{0, 65, 0}, block.Lever{Facing: cube.FaceUp, Direction: cube.North}, nil)
		tx.SetBlock(sensor.Add(cube.Pos{1, 0, 0}), block.RedstoneComparator{Facing: cube.West}, nil)
		for _, p := range []cube.Pos{sensor.Add(cube.Pos{2, 0, 0}), sensor.Add(cube.Pos{0, 0, -1}), sensor.Add(cube.Pos{0, 0, 1})} {
			tx.SetBlock(p, block.RedstoneWire{}, nil)
		}
		var b world.Block = block.NewSculkSensor()
		if c.Sensor == "calibrated_sculk_sensor" {
			b = block.NewCalibratedSculkSensor()
		}
		tx.SetBlock(sensor, b, nil)
		pos := mgl64.Vec3{.5, 65, 2.5}
		handle = tx.AddEntity((world.EntitySpawnOpts{Position: pos}).New(Type, Config{Position: pos, GameMode: world.GameModeCreative})).H()
	})
	applied := 0
	for _, sample := range trace.Samples {
		random.sensorFirst = choices[sample.Tick]
		w.AdvanceTick()
		var mismatch error
		<-w.Exec(func(tx *world.Tx) {
			use := func(before bool) {
				for _, change := range trace.Changes {
					if change.Tick == sample.Tick && change.BeforeSample == before {
						p, ok := handle.Entity(tx)
						if !ok {
							mismatch = fmt.Errorf("missing replay player")
							return
						}
						p.(*Player).UseItemOnBlock(cube.Pos{0, 65, 0}, cube.FaceUp, mgl64.Vec3{.5, .05, .5})
						applied++
					}
				}
			}
			use(true)
			if tx.CurrentTick() != sample.Tick {
				mismatch = fmt.Errorf("tick %d, BDS %d", tx.CurrentTick(), sample.Tick)
				return
			}
			if got := tx.Block(cube.Pos{0, 65, 0}).(block.Lever).Powered; got != sample.Source.States.Powered {
				mismatch = fmt.Errorf("tick %d lever=%v, BDS=%v", sample.Tick, got, sample.Source.States.Powered)
				return
			}
			b := tx.Block(sensor)
			_, props := b.EncodeBlock()
			if phase := int(props["sculk_sensor_phase"].(int32)); phase != sample.Sensor.States.Phase {
				mismatch = fmt.Errorf("tick %d sensor phase=%d, BDS=%d", sample.Tick, phase, sample.Sensor.States.Phase)
				return
			}
			if sample.Sensor.Power != nil {
				if got := b.(interface{ OutputPower() int }).OutputPower(); got != *sample.Sensor.Power {
					mismatch = fmt.Errorf("tick %d sensor power=%d, BDS=%d", sample.Tick, got, *sample.Sensor.Power)
					return
				}
			}
			comparator := tx.Block(sensor.Add(cube.Pos{1, 0, 0})).(block.RedstoneComparator)
			id, _ := comparator.EncodeBlock()
			if id != sample.Comparator.ID || comparator.Lit != sample.Comparator.States.Lit {
				mismatch = fmt.Errorf("tick %d comparator=%s/lit%t, BDS=%s/lit%t", sample.Tick, id, comparator.Lit, sample.Comparator.ID, sample.Comparator.States.Lit)
				return
			}
			for _, v := range []struct {
				Pos   cube.Pos
				Block sculkNativeBlock
			}{{sensor.Add(cube.Pos{0, 0, -1}), sample.North}, {sensor.Add(cube.Pos{0, 0, 1}), sample.South}, {sensor.Add(cube.Pos{2, 0, 0}), sample.Output}} {
				if got := tx.Block(v.Pos).(block.RedstoneWire).Power; got != v.Block.States.Signal {
					mismatch = fmt.Errorf("tick %d wire %v=%d, BDS=%d", sample.Tick, v.Pos, got, v.Block.States.Signal)
					return
				}
			}
			use(false)
		})
		if mismatch != nil {
			return mismatch
		}
	}
	if applied != expected {
		return fmt.Errorf("native player input not replayed")
	}
	return nil
}
