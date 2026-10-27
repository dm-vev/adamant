package player

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestSculkDiagonalSignalsMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_sculk_distance.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sensor string
				Pos    cube.Pos
			}
			Wire    cube.Pos
			Changes []struct {
				Tick         int64
				BeforeSample bool `json:"before_sample"`
			}
			Results []json.RawMessage
			Samples []struct {
				Tick                 int64
				Source, Sensor, Wire sculkNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 24 {
		t.Fatal("incomplete native diagonal sensor capture")
	}
	for _, trace := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/%v", trace.Scenario.Sensor, trace.Scenario.Pos), func(t *testing.T) {
			if len(trace.Changes) != 1 || len(trace.Results) != 1 || len(trace.Samples) < 130 {
				t.Fatal("missing accepted native player input")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			settings.WeatherCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			defer w.Close()
			var handle *world.EntityHandle
			applied := 0
			source, sensor := cube.Pos{0, 65, 0}, trace.Scenario.Pos
			<-w.Exec(func(tx *world.Tx) {
				for x := -32; x <= 48; x += 16 {
					for z := -32; z <= 48; z += 16 {
						tx.Block(cube.Pos{x, 64, z})
					}
				}
				for x := -4; x <= 20; x++ {
					for z := -4; z <= 20; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(source, block.Lever{Facing: cube.FaceUp, Direction: cube.North}, nil)
				tx.SetBlock(trace.Wire, block.RedstoneWire{}, nil)
				var b world.Block = block.NewSculkSensor()
				if trace.Scenario.Sensor == "calibrated_sculk_sensor" {
					s := block.NewCalibratedSculkSensor()
					s.Facing = cube.North
					b = s
				}
				tx.SetBlock(sensor, b, nil)
				pos := mgl64.Vec3{.5, 65, 2.5}
				handle = tx.AddEntity((world.EntitySpawnOpts{Position: pos}).New(Type, Config{Position: pos, GameMode: world.GameModeCreative})).H()
				tx.AddEntity((world.EntitySpawnOpts{Position: pos}).New(sculkInputType{}, sculkInputTick(func(tx *world.Tx, tick int64) {
					if ch := trace.Changes[0]; ch.Tick == tick && ch.BeforeSample {
						p, ok := handle.Entity(tx)
						if !ok {
							t.Fatal("missing replay player")
						}
						p.(*Player).UseItemOnBlock(source, cube.FaceUp, mgl64.Vec3{.5, .05, .5})
						applied++
					}
				})))
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Fatalf("tick %d, BDS %d", tx.CurrentTick(), sample.Tick)
					}
					if got := tx.Block(source).(block.Lever).Powered; got != sample.Source.States.Powered {
						t.Errorf("tick %d lever=%t, BDS %t", sample.Tick, got, sample.Source.States.Powered)
					}
					b := tx.Block(sensor)
					_, states := b.EncodeBlock()
					if got := int(states["sculk_sensor_phase"].(int32)); got != sample.Sensor.States.Phase {
						t.Errorf("tick %d sensor phase=%d, BDS %d", sample.Tick, got, sample.Sensor.States.Phase)
					}
					if sample.Sensor.Power == nil {
						t.Fatal("missing native sensor strength")
					}
					if got := b.(interface{ OutputPower() int }).OutputPower(); got != *sample.Sensor.Power {
						t.Errorf("tick %d sensor strength=%d, BDS %d", sample.Tick, got, *sample.Sensor.Power)
					}
					if got := tx.Block(trace.Wire).(block.RedstoneWire).Power; got != sample.Wire.States.Signal {
						t.Errorf("tick %d wire=%d, BDS %d", sample.Tick, got, sample.Wire.States.Signal)
					}
					if ch := trace.Changes[0]; ch.Tick == sample.Tick && !ch.BeforeSample {
						p, ok := handle.Entity(tx)
						if !ok {
							t.Fatal("missing replay player")
						}
						p.(*Player).UseItemOnBlock(source, cube.FaceUp, mgl64.Vec3{.5, .05, .5})
						applied++
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 1 {
				t.Fatal("native player input not replayed")
			}
		})
	}
}
