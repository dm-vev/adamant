package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type sculkContactNativeBlock struct {
	nativeTripwireBlock
	Power *int
}

func TestSculkPressurePlateVibrationsMatchBDS(t *testing.T) {
	testSculkNativeContacts(t, "bedrock_1_26_51_sculk_plates.json", 68)
}

func TestSculkTripwireVibrationsMatchBDS(t *testing.T) {
	testSculkNativeContacts(t, "bedrock_1_26_51_sculk_tripwire.json", 20)
}

func TestPressurePlateContextMatchesBDS(t *testing.T) {
	testSculkNativeContacts(t, "bedrock_1_26_51_plate_context.json", 17)
}

func testSculkNativeContacts(t *testing.T, name string, count int) {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	type vector struct{ X, Y, Z float64 }
	vec := func(v vector) mgl64.Vec3 { return mgl64.Vec3{v.X, v.Y, v.Z} }
	var native struct {
		Complete bool
		Sounds   []struct {
			SoundType, EntityType string
			ExtraData             int32
			Position              mgl64.Vec3
		}
		Scenarios []struct {
			Scenario struct {
				Block, Entity string
				Label         string
				Mode          string
				Powered       bool
				Filter, Phase int
				CaptureFrom   int `json:"capture_from"`
			}
			X, Z                                int
			Source, Sensor, Input, Output, Wire cube.Pos
			NearSensor                          *cube.Pos `json:"near_sensor"`
			NearOutput                          *cube.Pos `json:"near_output"`
			Comparator                          *cube.Pos
			LeftSupport                         *cube.Pos `json:"left_support"`
			RightSupport                        *cube.Pos `json:"right_support"`
			RightWire                           *cube.Pos `json:"right_wire"`
			DirectWire                          *cube.Pos `json:"direct_wire"`
			Barriers                            []struct {
				Pos   cube.Pos
				Block nativeTripwireBlock
			}
			Changes []struct {
				Tick    int64
				Kind    string
				Count   int
				Actions []struct {
					Pos       cube.Pos
					Block     string
					Direction int
					Attached  *bool
					Powered   *bool
				}
			}
			Samples []struct {
				Tick                                int64
				Source, Sensor, Input, Output, Wire sculkContactNativeBlock
				Left, Right                         *sculkContactNativeBlock
				NearSensor                          *sculkContactNativeBlock `json:"near_sensor"`
				NearOutput                          *sculkContactNativeBlock `json:"near_output"`
				Comparator                          *sculkContactNativeBlock
				LeftSupport                         *sculkContactNativeBlock `json:"left_support"`
				RightSupport                        *sculkContactNativeBlock `json:"right_support"`
				RightWire                           *sculkContactNativeBlock `json:"right_wire"`
				DirectWire                          *sculkContactNativeBlock `json:"direct_wire"`
				Entities                            []struct {
					ID       string
					EntityID string `json:"entity_id"`
					Position vector
					AABB     struct{ Center, Extent vector }
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native sensor/contact capture")
	}
	contextCapture := name == "bedrock_1_26_51_plate_context.json"
	if contextCapture && len(native.Sounds) != 2*count {
		t.Fatal("missing native plate sound packets")
	}
	if len(native.Sounds) != 0 && len(native.Sounds) != 2*count {
		t.Fatal("incomplete native source sound capture")
	}
	for traceIndex, trace := range native.Scenarios {
		c := trace.Scenario
		testName := fmt.Sprintf("%s/%s/%s/powered%t/filter%d/phase%d", c.Block, c.Entity, c.Mode, c.Powered, c.Filter, c.Phase)
		if c.Label != "" {
			testName += "/" + c.Label
		}
		t.Run(testName, func(t *testing.T) {
			if contextCapture && (trace.NearSensor == nil || trace.NearOutput == nil || trace.Comparator == nil || c.CaptureFrom != 60) {
				t.Fatal("missing adjacent plate/frequency context")
			}
			if name == "bedrock_1_26_51_hook_permutations.json" && trace.DirectWire == nil {
				t.Fatal("missing direct hook output probe")
			}
			expectedSamples := 210
			if c.Mode != "" {
				expectedSamples = 240
			}
			if c.CaptureFrom > 0 {
				expectedSamples = 211 - c.CaptureFrom
			}
			if len(trace.Samples) != expectedSamples || len(trace.Changes) != 3 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("missing native contact inputs or parity")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			settings.Time, settings.TimeCycle, settings.WeatherCycle = 6001, false, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			defer w.Close()
			if c.Mode != "" {
				if err := w.SetGameRule("block_drops", false); err != nil {
					t.Fatal(err)
				}
			}
			var soundObserver *nativeSwitchSoundObserver
			if len(native.Sounds) > 0 {
				soundObserver = &nativeSwitchSoundObserver{}
				w.Handle(soundObserver)
			}
			tripwire := c.Block == "tripwire_hook" || c.Mode != ""
			left, right := cube.Pos{trace.X, 65, trace.Z}, cube.Pos{trace.X + 2, 65, trace.Z}
			<-w.Exec(func(tx *world.Tx) {
				for x := (trace.X >> 4) - 2; x <= (trace.X>>4)+2; x++ {
					for z := (trace.Z >> 4) - 2; z <= (trace.Z>>4)+2; z++ {
						tx.Block(cube.Pos{x << 4, 64, z << 4})
					}
				}
				for x := trace.X - 2; x <= trace.X+9; x++ {
					for z := trace.Z - 2; z <= trace.Z+3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
					}
				}
				if trace.Barriers != nil {
					for x := trace.X - 6; x <= trace.X+6; x++ {
						for z := trace.Z - 6; z <= trace.Z+6; z++ {
							tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
						}
					}
					for _, barrier := range trace.Barriers {
						b, ok := w.BlockRegistry().BlockByName(barrier.Block.ID, nil)
						if !ok {
							t.Fatalf("unmapped native obstruction %s", barrier.Block.ID)
						}
						tx.SetBlock(barrier.Pos, b, nil)
						compareSculkContactNativeBlock(t, tx, trace.Samples[0].Tick, barrier.Pos, barrier.Block)
					}
				}
				if tripwire {
					tx.SetBlock(left.Side(cube.FaceWest), Stone{}, nil)
					tx.SetBlock(right.Side(cube.FaceEast), Stone{}, nil)
					tx.SetBlock(left, TripwireHook{Facing: cube.East}, nil)
					if c.Mode != "add_right" {
						tx.SetBlock(right, TripwireHook{Facing: cube.West}, nil)
					}
					if c.Mode != "complete_string" {
						tx.SetBlock(trace.Source, Tripwire{}, nil)
					}
				} else {
					found := false
					for _, kind := range PressurePlateTypes() {
						if kind.blockName() == c.Block {
							tx.SetBlock(trace.Source, PressurePlate{Type: kind}, nil)
							found = true
						}
					}
					if !found {
						t.Fatal("unmapped native pressure plate")
					}
				}
				tx.SetBlock(trace.Wire, RedstoneWire{}, nil)
				if trace.RightWire != nil {
					tx.SetBlock(*trace.RightWire, RedstoneWire{}, nil)
				}
				if trace.DirectWire != nil {
					tx.SetBlock(left.Side(cube.FaceSouth), Stone{}, nil)
					tx.SetBlock(*trace.DirectWire, RedstoneWire{}, nil)
				}
				tx.SetBlock(trace.Output, RedstoneWire{}, nil)
				tx.SetBlock(trace.Input, DaylightDetector{Signal: c.Filter}, nil)
				if trace.Comparator != nil {
					tx.SetBlock(*trace.Comparator, RedstoneComparator{Facing: cube.South}, nil)
				}
				sensor := NewCalibratedSculkSensor()
				sensor.Facing = cube.North
				tx.SetBlock(trace.Sensor, sensor, nil)
				if trace.NearSensor != nil {
					if trace.NearOutput == nil {
						t.Fatal("missing adjacent output")
					}
					tx.SetBlock(*trace.NearOutput, RedstoneWire{}, nil)
					near := NewCalibratedSculkSensor()
					near.Facing = cube.East
					tx.SetBlock(*trace.NearSensor, near, nil)
				}
			})
			type replayBody struct {
				handle *world.EntityHandle
				bounds *cube.BBox
			}
			bodies := map[string]replayBody{}
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Fatalf("tick %d, BDS %d", tx.CurrentTick(), sample.Tick)
					}
					for _, b := range []struct {
						pos    cube.Pos
						native sculkContactNativeBlock
					}{
						{trace.Source, sample.Source}, {trace.Wire, sample.Wire}, {trace.Sensor, sample.Sensor}, {trace.Input, sample.Input}, {trace.Output, sample.Output},
					} {
						compareSculkContactNativeBlock(t, tx, sample.Tick, b.pos, b.native.nativeTripwireBlock)
					}
					if sample.Sensor.Power == nil {
						t.Fatal("missing native sensor strength")
					}
					if got := tx.Block(trace.Sensor).(CalibratedSculkSensor).OutputPower(); got != *sample.Sensor.Power {
						t.Errorf("tick %d sensor strength=%d, BDS %d", sample.Tick, got, *sample.Sensor.Power)
					}
					listener := tx.Block(trace.Sensor).(CalibratedSculkSensor).VibrationData()
					var selected []world.VibrationInfo
					if listener.Selector.HasCandidate {
						selected = append(selected, listener.Selector.Candidate)
					}
					if listener.HasCurrent {
						selected = append(selected, listener.Current)
					}
					for _, info := range selected {
						wantSource := !tripwire && info.Event == world.GameEventBlockActivate
						if (info.SourceID != uuid.Nil) != wantSource {
							t.Errorf("tick %d event %s source=%s, native source present=%t", sample.Tick, info.Event, info.SourceID, wantSource)
						}
					}
					if tripwire {
						if sample.Left == nil || sample.Right == nil {
							t.Fatal("missing native hooks")
						}
						compareSculkContactNativeBlock(t, tx, sample.Tick, left, sample.Left.nativeTripwireBlock)
						compareSculkContactNativeBlock(t, tx, sample.Tick, right, sample.Right.nativeTripwireBlock)
					}
					if c.Mode != "" {
						if trace.LeftSupport == nil || trace.RightSupport == nil || trace.RightWire == nil || sample.LeftSupport == nil || sample.RightSupport == nil || sample.RightWire == nil {
							t.Fatal("missing lifecycle observations")
						}
						for _, part := range []struct {
							pos   cube.Pos
							state sculkContactNativeBlock
						}{
							{*trace.LeftSupport, *sample.LeftSupport}, {*trace.RightSupport, *sample.RightSupport}, {*trace.RightWire, *sample.RightWire},
						} {
							compareSculkContactNativeBlock(t, tx, sample.Tick, part.pos, part.state.nativeTripwireBlock)
						}
						if trace.DirectWire != nil {
							if sample.DirectWire == nil {
								t.Fatal("missing direct hook output observation")
							}
							compareSculkContactNativeBlock(t, tx, sample.Tick, *trace.DirectWire, sample.DirectWire.nativeTripwireBlock)
						}
						for _, change := range trace.Changes {
							if change.Tick != sample.Tick {
								continue
							}
							if len(change.Actions) == 0 {
								t.Fatal("missing native edit commands")
							}
							for _, action := range change.Actions {
								var b world.Block
								switch action.Block {
								case "air":
								case "stone":
									b = Stone{}
								case "trip_wire":
									b = Tripwire{}
								case "tripwire_hook":
									facing, ok := map[int]cube.Direction{0: cube.South, 1: cube.West, 2: cube.North, 3: cube.East}[action.Direction]
									if !ok {
										t.Fatal("unmapped hook direction")
									}
									b = TripwireHook{Facing: facing, Attached: action.Attached != nil && *action.Attached, Powered: action.Powered != nil && *action.Powered}
								default:
									t.Fatal("unmapped native lifecycle edit")
								}
								tx.SetBlock(action.Pos, b, nil)
							}
						}
					}
					if trace.NearSensor != nil {
						if sample.NearSensor == nil || sample.NearOutput == nil || sample.NearSensor.Power == nil || sample.Comparator == nil || sample.Comparator.Power == nil {
							t.Fatal("missing adjacent sensor/frequency observation")
						}
						compareSculkContactNativeBlock(t, tx, sample.Tick, *trace.NearSensor, sample.NearSensor.nativeTripwireBlock)
						compareSculkContactNativeBlock(t, tx, sample.Tick, *trace.NearOutput, sample.NearOutput.nativeTripwireBlock)
						near := tx.Block(*trace.NearSensor).(CalibratedSculkSensor)
						if near.OutputPower() != *sample.NearSensor.Power {
							t.Errorf("tick %d adjacent strength=%d, BDS %d", sample.Tick, near.OutputPower(), *sample.NearSensor.Power)
						}
						// The comparator diagnoses heard frequency; its own actor-order
						// dependent output transients are covered by separate captures.
						if frequency := *sample.Comparator.Power; frequency > 0 && near.LastVibrationFrequency() != frequency {
							t.Errorf("tick %d adjacent frequency=%d, native comparator %d", sample.Tick, near.LastVibrationFrequency(), frequency)
						}
					}
					if i+1 == len(trace.Samples) {
						return
					}
					// Preserve each native actor's identity across frames while replaying
					// its actual next-phase AABB. Motion/AI are not synthesized here.
					present := map[string]bool{}
					for _, body := range trace.Samples[i+1].Entities {
						if body.EntityID == "" {
							t.Fatal("missing native source identity")
						}
						present[body.EntityID] = true
						location := vec(body.Position)
						center, extent := vec(body.AABB.Center).Sub(location), vec(body.AABB.Extent)
						bounds := cube.Box(center[0]-extent[0], center[1]-extent[1], center[2]-extent[2], center[0]+extent[0], center[1]+extent[1], center[2]+extent[2])
						b, exists := bodies[body.EntityID]
						if !exists {
							b.bounds = &bounds
							typ := sculkContactBodyType{plateNativeBodyType: plateNativeBodyType{buttonNativeProjectileType: buttonNativeProjectileType{id: body.ID}, source: trace.Source}, bounds: b.bounds}
							b.handle = tx.AddEntity((world.EntitySpawnOpts{Position: location}).New(typ, buttonNativeProjectileConfig{})).H()
							bodies[body.EntityID] = b
						} else {
							*b.bounds = bounds
							e, ok := b.handle.Entity(tx)
							if !ok {
								t.Fatal("missing replay contact actor")
							}
							switch e := e.(type) {
							case plateNativeBody:
								e.d.Pos = location
							case plateNativeMob:
								e.d.Pos = location
							default:
								t.Fatal("unexpected replay actor view")
							}
						}
					}
					for id, b := range bodies {
						if !present[id] {
							if e, ok := b.handle.Entity(tx); ok {
								_ = tx.RemoveEntity(e).Close()
							}
							delete(bodies, id)
						}
					}
				})
				if t.Failed() {
					break
				}
			}
			if soundObserver != nil && !t.Failed() {
				var sounds []sound.NativeEvent
				var positions []mgl64.Vec3
				wantPoint := trace.Source.Vec3().Add(mgl64.Vec3{.5, .1, .5})
				for axis := range 3 {
					wantPoint[axis] = float64(float32(wantPoint[axis]))
				}
				for i, s := range soundObserver.sounds {
					point := soundObserver.positions[i]
					for axis := range 3 {
						point[axis] = float64(float32(point[axis]))
					}
					if point == wantPoint {
						if event, ok := s.(sound.NativeEvent); ok {
							sounds = append(sounds, event)
							positions = append(positions, point)
						}
					}
				}
				if len(sounds) != 2 {
					t.Fatalf("source sounds=%d, native 2", len(sounds))
				}
				for i, s := range sounds {
					want := native.Sounds[2*traceIndex+i]
					for axis := range 3 {
						want.Position[axis] = float64(float32(want.Position[axis]))
					}
					if s.SoundType != want.SoundType || s.ExtraData != want.ExtraData || s.EntityType != want.EntityType || positions[i] != want.Position {
						t.Errorf("sound=%+v at %v, native=%+v", s, positions[i], want)
					}
				}
			}
		})
	}
}

type sculkContactBodyType struct {
	plateNativeBodyType
	bounds *cube.BBox
}

func (t sculkContactBodyType) BBox(world.Entity) cube.BBox { return *t.bounds }

func compareSculkContactNativeBlock(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, expected nativeTripwireBlock) {
	t.Helper()
	registry := tx.World().BlockRegistry()
	id, states, ok := registry.RuntimeIDToState(registry.BlockRuntimeID(tx.Block(pos)))
	if !ok || id != expected.ID {
		t.Errorf("tick %d at %v id=%s, BDS %s", tick, pos, id, expected.ID)
		return
	}
	for name, want := range expected.States {
		got := states[name]
		if name == "facing_direction" && got == nil {
			switch chest := tx.Block(pos).(type) {
			case Chest:
				got = int32(chest.Facing.Face())
			case EnderChest:
				got = int32(chest.Facing.Face())
			}
		}
		// These captures retain the legacy inactive aliases next to their
		// modern properties. Validate only the observed inactive projections;
		// other phases require independent alias evidence.
		if name == "powered_bit" && got == nil {
			if sensor, ok := tx.Block(pos).(SculkSensor); ok && sensor.Phase == SculkSensorInactive {
				got = false
			}
		}
		if name == "active" && got == nil {
			if heart, ok := tx.Block(pos).(CreakingHeart); ok && heart.State == CreakingHeartUprooted {
				got = false
			}
		}
		// Script API exposes the sensor's legacy direction alias alongside
		// cardinal_direction. The native palette projection retains the latter;
		// check the alias against the same actual facing instead of dropping it.
		if name == "direction" && got == nil {
			switch b := tx.Block(pos).(type) {
			case CalibratedSculkSensor:
				got = int32(horizontalDirection(b.Facing))
			case RedstoneRepeater:
				got = int32(horizontalDirection(b.Facing))
			case RedstoneComparator:
				got = int32(horizontalDirection(b.Facing))
			}
		}
		if name == "stone_type" && got == nil && id == "minecraft:stone" {
			if _, ok := tx.Block(pos).(Stone); ok {
				got = "stone"
			}
		}
		if name == "color" && got == nil {
			switch b := tx.Block(pos).(type) {
			case Wool:
				got = b.Colour.String()
			case Carpet:
				got = b.Colour.String()
			}
		}
		switch v := want.(type) {
		case bool:
			if n, ok := got.(uint8); ok {
				got = n != 0
			}
		case float64:
			if n, ok := got.(int32); ok {
				got = float64(n)
			}
		case string:
		default:
			t.Fatalf("unsupported native state %s=%v", name, v)
		}
		if got != want {
			t.Errorf("tick %d at %v %s=%v, BDS %v", tick, pos, name, got, want)
		}
	}
}
