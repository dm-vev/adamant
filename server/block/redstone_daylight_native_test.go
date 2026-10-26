package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Roofs are generated before light initialization to test initial filling
// independently of live mutations, covered by the roof replay. SDK sky values
// are already darkened and are never injected as raw skylight.
type nativeDaylightTerrain map[cube.Pos]world.Block

func (n nativeDaylightTerrain) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{0, 65, 0} }
func (n nativeDaylightTerrain) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	for p, b := range n {
		if p[0]>>4 == int(pos[0]) && p[2]>>4 == int(pos[1]) {
			c.SetBlock(uint8(p[0]), int16(p[1]), uint8(p[2]), 0, world.BlockRuntimeID(b))
		}
	}
	return world.GenerationResult{}
}

func TestDaylightDetectorsMatchNativeCapture(t *testing.T) {
	testDaylightDetectorCapture(t, "bedrock_1_26_51_daylight.json", 16)
}

func TestDaylightInitialProducersMatchNativeCapture(t *testing.T) {
	testDaylightDetectorCapture(t, "bedrock_1_26_51_daylight_initial.json", 1)
}

func testDaylightDetectorCapture(t *testing.T, filename string, count int) {
	data, err := os.ReadFile("testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Time, Phase int
				Advance     bool
			}
			Changes []struct {
				Tick int64
				Kind string
			}
			WarmupTick int64 `json:"warmup_tick"`
			Cells      []struct {
				Dimension, Cover string
				X, Z, Initial    int
				Inverted         bool
			}
			Samples []struct {
				Tick     int64
				GameTick int64 `json:"game_tick"`
				Time     int
				Values   []struct {
					Source, Wire torchNetworkBlock
					Sky          int
				}
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != count {
		t.Fatal("incomplete native daylight capture")
	}
	for _, trace := range reference.Scenarios {
		for _, dimension := range []struct {
			name string
			dim  world.Dimension
		}{
			{"overworld", world.Overworld}, {"nether", world.Nether}, {"the_end", world.End},
		} {
			t.Run(fmt.Sprintf("time%d/advance%v/%s", trace.Scenario.Time, trace.Scenario.Advance, dimension.name), func(t *testing.T) {
				if count == 1 && (trace.WarmupTick <= 0 || trace.WarmupTick > trace.Samples[0].Tick) {
					t.Fatal("missing native chunk-ticking controls")
				}
				if len(trace.Samples) != 60 || len(trace.Cells) != 48 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(trace.Scenario.Phase) {
					t.Fatal("invalid native daylight trace")
				}
				settings := (world.NopProvider{}).Settings()
				settings.CurrentTick = trace.Samples[0].Tick - 2
				terrain := nativeDaylightTerrain{}
				for x := 0; x < 32; x++ {
					for z := 0; z < 32; z++ {
						terrain[cube.Pos{x, 64, z}] = Stone{}
					}
				}
				for _, c := range trace.Cells {
					if c.Dimension != dimension.name {
						continue
					}
					var roof world.Block
					switch c.Cover {
					case "stone":
						roof = Stone{}
					case "glass":
						roof = Glass{}
					case "leaves":
						roof = Leaves{Persistent: true}
					}
					if roof != nil {
						for x := c.X - 2; x <= c.X+2; x++ {
							for z := c.Z - 2; z <= c.Z+2; z++ {
								terrain[cube.Pos{x, 68, z}] = roof
							}
						}
					}
				}
				w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: dimension.dim, Generator: terrain, Provider: world.NopProvider{Set: settings}}.New()
				t.Cleanup(func() { _ = w.Close() })
				w.SetTimeCycle(trace.Scenario.Advance)
				w.SetTime(trace.Scenario.Time)
				<-w.Exec(func(tx *world.Tx) {
					for _, c := range trace.Cells {
						if c.Dimension != dimension.name {
							continue
						}
						tx.SetBlock(cube.Pos{c.X, 65, c.Z}, DaylightDetector{Signal: c.Initial, Inverted: c.Inverted}, nil)
						tx.SetBlock(cube.Pos{c.X + 1, 65, c.Z}, RedstoneWire{}, nil)
					}
				})
				for _, sample := range trace.Samples {
					w.AdvanceTick()
					<-w.Exec(func(tx *world.Tx) {
						if tx.CurrentTick() != sample.Tick || w.Time() != sample.Time {
							t.Fatalf("clock=(%d,%d) native=(%d,%d)", tx.CurrentTick(), w.Time(), sample.Tick, sample.Time)
						}
						if len(sample.Values) != len(trace.Cells) {
							t.Fatal("missing daylight cells")
						}
						for i, c := range trace.Cells {
							if c.Dimension != dimension.name {
								continue
							}
							v := sample.Values[i]
							compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{c.X, 65, c.Z}, v.Source)
							if d := tx.Block(cube.Pos{c.X, 65, c.Z}).(DaylightDetector); d.Signal != v.Source.States.Signal || d.Inverted != c.Inverted {
								t.Errorf("tick %d at (%d,65,%d): detector=%+v native signal=%d", sample.Tick, c.X, c.Z, d, v.Source.States.Signal)
							}
							compareTorchNetworkBlock(t, tx, sample.Tick, cube.Pos{c.X + 1, 65, c.Z}, v.Wire)
						}
					})
					if t.Failed() {
						break
					}
				}
			})
		}
	}
}

func TestDaylightFullDayWeatherMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_daylight_cycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Weather      string
				Start, Phase int
			}
			Changes []struct{ Tick int64 }
			Cells   []struct {
				X, Z     int
				Inverted bool
				Cover    string
			}
			Samples []struct {
				Tick   int64
				Time   int
				Values []struct {
					Source torchNetworkBlock
					Sky    int
				}
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 18 {
		t.Fatal("incomplete full-day weather capture")
	}
	for _, trace := range reference.Scenarios {
		t.Run(fmt.Sprintf("%s/start%d", trace.Scenario.Weather, trace.Scenario.Start), func(t *testing.T) {
			if len(trace.Samples) != 200 || len(trace.Cells) != 8 || len(trace.Changes) != 1 || trace.Changes[0].Tick&1 != int64(trace.Scenario.Phase) {
				t.Fatal("invalid full-day trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.Raining = trace.Scenario.Weather != "clear"
			settings.Thundering = trace.Scenario.Weather == "thunder"
			settings.WeatherCycle = false
			settings.TimeCycle = false
			terrain := nativeDaylightTerrain{}
			for _, c := range trace.Cells {
				var roof world.Block
				switch c.Cover {
				case "stone":
					roof = Stone{}
				case "glass":
					roof = Glass{}
				case "leaves":
					roof = Leaves{Persistent: true}
				}
				terrain[cube.Pos{c.X, 64, c.Z}] = Stone{}
				if roof != nil {
					for x := c.X - 2; x <= c.X+2; x++ {
						for z := c.Z - 2; z <= c.Z+2; z++ {
							terrain[cube.Pos{x, 68, z}] = roof
						}
					}
				}
			}
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}, Generator: terrain}.New()
			t.Cleanup(func() { _ = w.Close() })
			for i, sample := range trace.Samples {
				if sample.Time != trace.Scenario.Start+i*20 || len(sample.Values) != 8 {
					t.Fatal("missing full-day sample")
				}
				w.SetTime(sample.Time)
				<-w.Exec(func(tx *world.Tx) {
					for n, c := range trace.Cells {
						pos := cube.Pos{c.X, 65, c.Z}
						v := sample.Values[n]
						d := DaylightDetector{Inverted: c.Inverted}
						id, _ := d.EncodeBlock()
						if id != v.Source.ID {
							t.Fatal("native detector variant changed")
						}
						if got := d.daylightSignal(pos, tx); got != v.Source.States.Signal {
							t.Errorf("time%d cover%s inverted%v: signal=%d native=%d", sample.Time, c.Cover, c.Inverted, got, v.Source.States.Signal)
						}
						// With this matrix's raw light >=12, inversion is unclamped:
						// it independently exposes the native SDK's darkened sky.
						gotSky := 15 - (DaylightDetector{Inverted: true}).daylightSignal(pos, tx)
						if gotSky != v.Sky {
							t.Errorf("time%d cover%s: sky=%d native=%d", sample.Time, c.Cover, gotSky, v.Sky)
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

func TestDaylightBodyMatchesInspectedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_daylight_methods.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Complete bool
		Bounds   []float64
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if !ref.Complete || len(ref.Bounds) != 6 {
		t.Fatal("incomplete native body evidence")
	}
	for _, b := range allDaylightDetectors() {
		boxes := b.Model().BBox(cube.Pos{}, nil)
		if len(boxes) != 1 {
			t.Fatalf("detector has %d collision boxes", len(boxes))
		}
		lo, hi := boxes[0].Min(), boxes[0].Max()
		for i := 0; i < 3; i++ {
			if lo[i] != ref.Bounds[i] || hi[i] != ref.Bounds[i+3] {
				t.Fatalf("body=%v native=%v", boxes[0], ref.Bounds)
			}
		}
	}
}

func TestDaylightPlayerInversionMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_daylight_use.json")
	if err != nil {
		t.Fatal(err)
	}
	var ref struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Inverted     bool
				Initial      int
				RequestPhase int `json:"request_phase"`
			}
			Samples []struct {
				Tick         int64
				GameTick     int64 `json:"game_tick"`
				Source, Wire torchNetworkBlock
			}
			Changes []struct {
				Tick         int64
				BeforeSample bool `json:"before_sample"`
				Before       torchNetworkBlock
			}
			Results []struct {
				Tick   int64
				Source torchNetworkBlock
			}
		}
	}
	if err := json.Unmarshal(data, &ref); err != nil {
		t.Fatal(err)
	}
	if !ref.Complete || len(ref.Scenarios) != 12 {
		t.Fatal("incomplete native inversion capture")
	}
	for _, trace := range ref.Scenarios {
		t.Run(fmt.Sprintf("inverted%v/initial%d/phase%d", trace.Scenario.Inverted, trace.Scenario.Initial, trace.Scenario.RequestPhase), func(t *testing.T) {
			if len(trace.Samples) < 24 || len(trace.Changes) != 1 || len(trace.Results) != 1 {
				t.Fatal("invalid inversion events")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			settings.Time = 6001
			settings.TimeCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos := cube.Pos{0, 65, 0}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(cube.FaceDown), Stone{}, nil)
				tx.SetBlock(pos.Side(cube.FaceEast).Side(cube.FaceDown), Stone{}, nil)
				tx.SetBlock(pos, DaylightDetector{Inverted: trace.Scenario.Inverted, Signal: trace.Scenario.Initial}, nil)
				tx.SetBlock(pos.Side(cube.FaceEast), RedstoneWire{}, nil)
			})
			check := func(tx *world.Tx, native torchNetworkBlock) {
				t.Helper()
				d := tx.Block(pos).(DaylightDetector)
				id, _ := d.EncodeBlock()
				if id != native.ID || d.Signal != native.States.Signal {
					t.Errorf("tick%d detector=%s/%d native=%s/%d", tx.CurrentTick(), id, d.Signal, native.ID, native.States.Signal)
				}
			}
			applied := 0
			apply := func(tx *world.Tx) {
				check(tx, trace.Changes[0].Before)
				tx.Block(pos).(DaylightDetector).Activate(pos, cube.FaceUp, tx, nil, nil)
				check(tx, trace.Results[0].Source)
				applied++
			}
			for _, sample := range trace.Samples {
				change := trace.Changes[0]
				if change.BeforeSample && change.Tick == sample.Tick {
					<-w.Exec(apply)
				}
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick || sample.GameTick != 6001 {
						t.Fatal("inversion clock drift")
					}
					check(tx, sample.Source)
					compareTorchNetworkBlock(t, tx, sample.Tick, pos.Side(cube.FaceEast), sample.Wire)
					if !change.BeforeSample && change.Tick == sample.Tick {
						apply(tx)
					}
				})
				if t.Failed() {
					break
				}
			}
			if applied != 1 && !t.Failed() {
				t.Fatalf("applied %d inputs", applied)
			}
		})
	}
}
