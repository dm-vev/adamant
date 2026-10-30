package player

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestSculkCalibratedFilteringMatchesBDS(t *testing.T) {
	testSculkNativeEnvironment(t, "bedrock_1_26_51_sculk_filters.json", 24)
}

func TestSculkVibrationObstructionMatchesBDS(t *testing.T) {
	testSculkNativeEnvironment(t, "bedrock_1_26_51_sculk_occlusion.json", 48)
}

func TestSculkButtonVibrationsMatchBDS(t *testing.T) {
	testSculkNativeEnvironment(t, "bedrock_1_26_51_sculk_buttons.json", 8)
}

func TestSculkAdjacentSwitchFilteringMatchesBDS(t *testing.T) {
	testSculkNativeEnvironment(t, "bedrock_1_26_51_sculk_adjacent.json", 15)
}

func testSculkNativeEnvironment(t *testing.T, name string, count int) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sensor, Facing, Barrier, Source string
				Filter                          *int
				FilterFront                     bool      `json:"filter_front"`
				RequestPhase                    int       `json:"request_phase"`
				SensorPos                       *cube.Pos `json:"sensor_pos"`
			}
			Input   *cube.Pos
			Wires   []cube.Pos
			Changes []struct {
				Tick         int64
				BeforeSample bool `json:"before_sample"`
			}
			Results []json.RawMessage
			Samples []struct {
				Tick                    int64
				Source, Sensor, Barrier sculkNativeBlock
				Input                   *sculkNativeBlock
				Wires                   []struct {
					Pos   cube.Pos
					Block sculkNativeBlock
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native sculk environment capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		filter := "none"
		if c.Filter != nil {
			filter = strconv.Itoa(*c.Filter)
		}
		t.Run(fmt.Sprintf("%s/%s/source%s/barrier%s/filter%s/front%t", c.Sensor, c.Facing, c.Source, c.Barrier, filter, c.FilterFront), func(t *testing.T) {
			if len(trace.Changes) != 2 || len(trace.Results) != 2 || len(trace.Samples) < 220 {
				t.Fatal("missing accepted native player uses")
			}
			facing := cube.North
			for _, d := range cube.Directions() {
				if d.String() == c.Facing {
					facing = d
				}
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			settings.Time, settings.TimeCycle, settings.WeatherCycle = 6001, false, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			defer w.Close()
			var handle *world.EntityHandle
			applied := 0
			const y = 65
			sensor, source := cube.Pos{4, y, 0}, cube.Pos{0, y, 0}
			if c.SensorPos != nil {
				sensor = *c.SensorPos
			}
			<-w.Exec(func(tx *world.Tx) {
				for x := -32; x <= 48; x += 16 {
					for z := -32; z <= 32; z += 16 {
						tx.Block(cube.Pos{x, 64, z})
					}
				}
				for x := -4; x <= 10; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				var inputBlock world.Block = block.Lever{Facing: cube.FaceUp, Direction: cube.North}
				if c.Source != "" {
					typeOfButton := block.WoodButton()
					if c.Source == "stone_button" {
						typeOfButton = block.StoneButton()
					}
					inputBlock = block.Button{Type: typeOfButton, Facing: cube.FaceUp}
				}
				tx.SetBlock(source, inputBlock, nil)
				for _, pos := range trace.Wires {
					tx.SetBlock(pos, block.RedstoneWire{}, nil)
				}
				if trace.Input != nil {
					if c.Filter == nil {
						t.Fatal("missing native filter strength")
					}
					tx.SetBlock(*trace.Input, block.DaylightDetector{Signal: *c.Filter}, nil)
				}
				if c.Barrier != "" && c.Barrier != "air" {
					tx.SetBlock(cube.Pos{2, y, 0}, sculkNativeBarrier(t, c.Barrier), nil)
				}
				var b world.Block = block.NewSculkSensor()
				if c.Sensor == "calibrated_sculk_sensor" {
					s := block.NewCalibratedSculkSensor()
					s.Facing = facing
					b = s
				}
				tx.SetBlock(sensor, b, nil)
				pos := mgl64.Vec3{.5, 65, 2.5}
				handle = tx.AddEntity((world.EntitySpawnOpts{Position: pos}).New(Type, Config{Position: pos, GameMode: world.GameModeCreative})).H()
				// Native player use precedes this tick's scheduled block pass.
				// Replaying after AdvanceTick instead advances the queue clock
				// before the click and postpones button expiry by one game tick.
				tx.AddEntity((world.EntitySpawnOpts{Position: pos}).New(sculkInputType{}, sculkInputTick(func(tx *world.Tx, current int64) {
					for _, change := range trace.Changes {
						if change.Tick == current && change.BeforeSample {
							p, ok := handle.Entity(tx)
							if !ok {
								t.Fatal("missing replay player")
							}
							p.(*Player).UseItemOnBlock(source, cube.FaceUp, mgl64.Vec3{.5, .05, .5})
							applied++
						}
					}
				})))
			})
			for _, s := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					use := func(before bool) {
						for _, ch := range trace.Changes {
							if ch.Tick == s.Tick && ch.BeforeSample == before {
								p, ok := handle.Entity(tx)
								if !ok {
									t.Fatal("missing replay player")
								}
								p.(*Player).UseItemOnBlock(source, cube.FaceUp, mgl64.Vec3{.5, .05, .5})
								applied++
							}
						}
					}
					if tx.CurrentTick() != s.Tick {
						t.Fatalf("tick %d, BDS %d", tx.CurrentTick(), s.Tick)
					}
					if c.Source == "" {
						if got := tx.Block(source).(block.Lever).Powered; got != s.Source.States.Powered {
							t.Errorf("tick %d lever=%t, BDS %t", s.Tick, got, s.Source.States.Powered)
						}
					} else if got := tx.Block(source).(block.Button).Pressed; got != s.Source.States.Pressed {
						t.Errorf("tick %d button=%t, BDS %t", s.Tick, got, s.Source.States.Pressed)
					}
					b := tx.Block(sensor)
					_, props := b.EncodeBlock()
					if got := int(props["sculk_sensor_phase"].(int32)); got != s.Sensor.States.Phase {
						t.Errorf("tick %d sensor phase=%d, BDS %d", s.Tick, got, s.Sensor.States.Phase)
					}
					if s.Sensor.Power == nil {
						t.Fatal("missing native sensor strength")
					}
					if got := b.(interface{ OutputPower() int }).OutputPower(); got != *s.Sensor.Power {
						t.Errorf("tick %d sensor strength=%d, BDS %d", s.Tick, got, *s.Sensor.Power)
					}
					if trace.Input != nil {
						if s.Input == nil {
							t.Fatal("missing native input block")
						}
						if got := tx.Block(*trace.Input).(block.DaylightDetector).Signal; got != s.Input.States.Signal {
							t.Errorf("tick %d filter=%d, BDS %d", s.Tick, got, s.Input.States.Signal)
						}
					}
					id, _ := tx.Block(cube.Pos{2, y, 0}).EncodeBlock()
					if id != s.Barrier.ID {
						t.Errorf("tick %d barrier=%s, BDS %s", s.Tick, id, s.Barrier.ID)
					}
					for _, v := range s.Wires {
						if got := tx.Block(v.Pos).(block.RedstoneWire).Power; got != v.Block.States.Signal {
							t.Errorf("tick %d wire %v=%d, BDS %d", s.Tick, v.Pos, got, v.Block.States.Signal)
						}
					}
					use(false)
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 2 {
				t.Fatal("native player inputs not replayed")
			}
		})
	}
}

// This controller supplies the recorded native input during the entity pass;
// it neither synthesizes a game event nor alters a scheduled callback.
type sculkInputTick func(*world.Tx, int64)

func (f sculkInputTick) Apply(data *world.EntityData) { data.Data = f }

type sculkInputType struct{}

func (sculkInputType) Open(_ *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	return &sculkInputEntity{h, data}
}
func (sculkInputType) EncodeEntity() string                        { return "adamant:audit_input" }
func (sculkInputType) BBox(world.Entity) cube.BBox                 { return cube.BBox{} }
func (sculkInputType) DecodeNBT(map[string]any, *world.EntityData) {}
func (sculkInputType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type sculkInputEntity struct {
	handle *world.EntityHandle
	data   *world.EntityData
}

func (e *sculkInputEntity) H() *world.EntityHandle        { return e.handle }
func (e *sculkInputEntity) Position() mgl64.Vec3          { return e.data.Pos }
func (e *sculkInputEntity) Rotation() cube.Rotation       { return e.data.Rot }
func (*sculkInputEntity) Close() error                    { return nil }
func (e *sculkInputEntity) Tick(tx *world.Tx, tick int64) { e.data.Data.(sculkInputTick)(tx, tick) }

func sculkNativeBarrier(t *testing.T, name string) world.Block {
	t.Helper()
	for _, colour := range item.Colours() {
		if name == colour.String()+"_wool" {
			return block.Wool{Colour: colour}
		}
		if name == colour.String()+"_carpet" {
			return block.Carpet{Colour: colour}
		}
	}
	switch name {
	case "stone":
		return block.Stone{}
	case "glass":
		return block.Glass{}
	case "moss_carpet":
		return block.MossCarpet{}
	case "snow_layer":
		return block.SnowLayer{Layers: 1}
	case "oak_leaves":
		return block.Leaves{Type: block.OakLeaves()}
	default:
		t.Fatalf("unhandled native barrier %q", name)
		return nil
	}
}
