package block

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPistonTimingMatchesBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_timing.json", 432, 100, 2)
}

func TestPistonRetriggerMatchesBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_retrigger.json", 576, 140, 4)
}

func TestPistonStrongInputMatchesBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_strong.json", 144, 100, 2)
}

func TestPistonBlocksMatchBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_blocks.json", 96, 100, 2)
}

func TestPistonMaterialsMatchBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_materials.json", 480, 100, 2)
}

func TestPistonPushOnlyAdhesionMatchesBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_push_only.json", 288, 100, 2)
}

func TestPistonPayloadMatchesBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_payload.json", 96, 100, 2)
}

func TestPistonPoppedBlocksMatchBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_popped.json", 112, 100, 2)
}

func TestPistonPoppedDropsMatchBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_popped_drops.json", 112, 100, 2)
}

func TestPistonAdhesionMatchesBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_adhesion.json", 240, 100, 2)
}

func TestPistonExtendedAdhesionMatchesBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_adhesion_extended.json", 288, 100, 2)
}

func TestPistonVibrationsMatchBDS(t *testing.T) {
	testPistonNativeTrace(t, "testdata/bedrock_1_26_51_piston_vibration.json", 96, 180, 2)
}

type nativePistonStack struct {
	Slot  int
	ID    string
	Count int
}
type nativePistonBlock struct {
	nativeTripwireBlock
	Inventory []nativePistonStack
}

type nativePistonActor struct {
	State    string
	Moving   bool
	Attached []struct{ X, Y, Z int }
}

type nativePistonDrop struct {
	ID    string
	Count int
}

func TestPistonSortMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_piston_sort.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Face          cube.Face
			Before, After []cube.Pos
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 334 {
		t.Fatal("incomplete executed-client piston sort fixture")
	}
	for i, c := range native.Scenarios {
		sortPistonBlocks(c.Before, c.Face)
		if !slices.Equal(c.Before, c.After) {
			t.Errorf("case %d face %d sort=%v, native %v", i, c.Face, c.Before, c.After)
		}
	}
}

func testPistonNativeTrace(t *testing.T, fixture string, count, frames, changes int) {
	t.Helper()
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Kind                        string
				PayloadKind                 string `json:"payload_kind"`
				Length                      int
				Adhesion, Arm               string
				Sticky, Conducted, Payload  bool
				Facing, Input, Phase, Width int
				Gap                         int
				SensorFilter                *int `json:"sensor_filter"`
				TileDrops                   bool `json:"tile_drops"`
			}
			Base, Source cube.Pos
			Conductor    *cube.Pos
			Front        []cube.Pos
			Sensor       *cube.Pos
			SensorInput  *cube.Pos `json:"sensor_input"`
			SensorOutput *cube.Pos `json:"sensor_output"`
			Extras       []struct {
				Pos   cube.Pos
				Block string
			}
			ExtraProbes  []cube.Pos `json:"extra_probes"`
			TargetProbes []cube.Pos `json:"target_probes"`
			SetupTick    int64      `json:"setup_tick"`
			Changes      []struct {
				Tick    int64
				Powered bool
			}
			Injections []struct {
				Tick  int64
				Pos   cube.Pos
				Block nativeTripwireBlock
			}
			Samples []struct {
				Tick         int64
				Base, Source nativeTripwireBlock
				Conductor    *nativeTripwireBlock
				Front        []nativePistonBlock
				Extra        []nativePistonBlock
				Sensor       *sculkContactNativeBlock
				SensorInput  *sculkContactNativeBlock `json:"sensor_input"`
				SensorOutput *sculkContactNativeBlock `json:"sensor_output"`
				Actor        nativePistonActor
				TargetActors []*nativePistonActor `json:"target_actors"`
				Drops        []nativePistonDrop
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native piston capture")
	}
	byName := make(map[string][]world.Block)
	for _, b := range world.DefaultBlockRegistry.Blocks() {
		name, _ := b.EncodeBlock()
		byName[name] = append(byName[name], b)
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("sticky%t/face%d/input%d/conducted%t/payload%t/%s%d/%s/%s/width%d/gap%d/phase%d", c.Sticky, c.Facing, c.Input, c.Conducted, c.Payload, c.PayloadKind, c.Length, c.Adhesion, c.Arm, c.Width, c.Gap, c.Phase), func(t *testing.T) {
			frontCount := 3
			if c.PayloadKind != "" {
				frontCount = 14
			}
			if len(trace.Samples) != frames || len(trace.Changes) != changes || len(trace.Front) != frontCount || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native timing trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			if c.SensorFilter != nil {
				settings.Time, settings.TimeCycle, settings.WeatherCycle = 6001, false, false
			}
			config := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}
			dropCounts := make(map[string]int)
			if c.TileDrops {
				// Record actual drop factory calls. The inert body intentionally
				// does not compare item physics, pickup, merging or despawn.
				config.Entities = world.EntityRegistryConfig{Item: func(opts world.EntitySpawnOpts, value any) *world.EntityHandle {
					stack := value.(item.Stack)
					id, _ := stack.Item().EncodeItem()
					dropCounts[id] += stack.Count()
					return opts.New(redstoneTNTTestEntityType{}, redstoneTNTTestEntityConfig{})
				}}.New([]world.EntityType{redstoneTNTTestEntityType{}})
			}
			if c.SensorFilter != nil {
				config.Dim = world.Overworld
			}
			w := config.New()
			t.Cleanup(func() { _ = w.Close() })
			if err := w.SetGameRule("block_drops", c.TileDrops); err != nil {
				t.Fatal(err)
			}
			<-w.Exec(func(tx *world.Tx) {
				if trace.Conductor != nil {
					tx.SetBlock(*trace.Conductor, Stone{}, nil)
				}
				tx.SetBlock(trace.Base, Piston{Facing: cube.Face(c.Facing), Sticky: c.Sticky}, nil)
				if c.Payload {
					tx.SetBlock(trace.Front[0], Stone{}, nil)
				}
				for n := 0; n < c.Length; n++ {
					var b world.Block
					switch c.PayloadKind {
					case "stone":
						b = Stone{}
					case "obsidian":
						b = Obsidian{}
					case "slime":
						b = Slime{}
					case "honey_block":
						b = HoneyBlock{}
					case "chest":
						chest := NewChest()
						if err := chest.OwnInventory().SetItem(0, item.NewStack(item.Diamond{}, 7)); err != nil {
							t.Fatal(err)
						}
						b = chest
					default:
						b = pistonNativeBlock(byName, trace.Samples[0].Front[n].nativeTripwireBlock)
						if b == nil {
							t.Fatalf("unmapped piston payload %s", c.PayloadKind)
						}
					}
					tx.SetBlock(trace.Front[n], b, nil)
				}
				for _, extra := range trace.Extras {
					b, ok := w.BlockRegistry().BlockByName("minecraft:"+extra.Block, nil)
					if !ok {
						for i, pos := range trace.ExtraProbes {
							if pos == extra.Pos {
								b = pistonNativeBlock(byName, trace.Samples[0].Extra[i].nativeTripwireBlock)
								break
							}
						}
					}
					if b == nil {
						t.Fatalf("unmapped adhesion block %s", extra.Block)
					}
					tx.SetBlock(extra.Pos, b, nil)
				}
				if c.SensorFilter != nil {
					if trace.Sensor == nil || trace.SensorInput == nil || trace.SensorOutput == nil {
						t.Fatal("missing native piston listener layout")
					}
					// BDS's ticking area supplies the listener's surrounding
					// columns. The isolated replay must load the same 3x3 area.
					for dx := -1; dx <= 1; dx++ {
						for dz := -1; dz <= 1; dz++ {
							tx.Block(cube.Pos{(((*trace.Sensor)[0] >> 4) + dx) << 4, 65, (((*trace.Sensor)[2] >> 4) + dz) << 4})
						}
					}
					for x := trace.Base[0] + 4; x <= trace.Base[0]+7; x++ {
						for z := trace.Base[2] + 4; z <= trace.Base[2]+7; z++ {
							tx.SetBlock(cube.Pos{x, 64, z}, Stone{}, nil)
						}
					}
					tx.SetBlock(*trace.SensorInput, DaylightDetector{Signal: *c.SensorFilter}, nil)
					tx.SetBlock(*trace.SensorOutput, RedstoneWire{}, nil)
					tx.SetBlock(*trace.Sensor, NewCalibratedSculkSensor(), nil)
				}
				if c.Kind == "lever" {
					tx.SetBlock(trace.Source, Lever{Facing: cube.Face(c.Input), Direction: cube.North}, nil)
				}
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Fatalf("clock %d, native %d", tx.CurrentTick(), sample.Tick)
					}
					if c.TileDrops {
						ids := make([]string, 0, len(dropCounts))
						for id := range dropCounts {
							ids = append(ids, id)
						}
						slices.Sort(ids)
						got := make([]nativePistonDrop, 0, len(ids))
						for _, id := range ids {
							got = append(got, nativePistonDrop{id, dropCounts[id]})
						}
						if !slices.Equal(got, sample.Drops) {
							t.Errorf("tick %d cumulative drops=%v, native=%v", sample.Tick, got, sample.Drops)
						}
					}
					comparePistonNativeActor(t, tx, sample.Tick, trace.Base, &sample.Actor)
					if len(trace.TargetProbes) != len(sample.TargetActors) {
						t.Fatal("missing native payload actor observations")
					}
					for i, pos := range trace.TargetProbes {
						comparePistonNativeActor(t, tx, sample.Tick, pos, sample.TargetActors[i])
					}
					compareSculkContactNativeBlock(t, tx, sample.Tick, trace.Base, sample.Base)
					compareSculkContactNativeBlock(t, tx, sample.Tick, trace.Source, sample.Source)
					if trace.Conductor != nil {
						if sample.Conductor == nil {
							t.Fatal("missing conductor observation")
						}
						compareSculkContactNativeBlock(t, tx, sample.Tick, *trace.Conductor, *sample.Conductor)
					}
					if len(sample.Front) != len(trace.Front) {
						t.Fatal("missing piston front observations")
					}
					for i, pos := range trace.Front {
						compareSculkContactNativeBlock(t, tx, sample.Tick, pos, sample.Front[i].nativeTripwireBlock)
						if want := sample.Front[i].Inventory; want != nil {
							got := make([]nativePistonStack, 0)
							var inv *inventory.Inventory
							switch b := tx.Block(pos).(type) {
							case Chest:
								inv = b.OwnInventory()
							case Container:
								inv = b.Inventory(tx, pos)
							default:
								// Beacon/ender-chest Script API components expose
								// an empty inventory without block-local storage.
								if len(want) != 0 || c.PayloadKind == "chest" {
									t.Errorf("tick %d missing native inventory at %v", sample.Tick, pos)
								}
							}
							if inv != nil {
								for slot, stack := range inv.Slots() {
									if !stack.Empty() {
										id, _ := stack.Item().EncodeItem()
										got = append(got, nativePistonStack{slot, id, stack.Count()})
									}
								}
							}
							if !slices.Equal(got, want) {
								t.Errorf("tick %d inventory %v, native %v", sample.Tick, got, want)
							}
						}
					}
					if trace.Sensor != nil {
						if sample.Sensor == nil || sample.SensorInput == nil || sample.SensorOutput == nil || sample.Sensor.Power == nil {
							t.Fatal("missing native piston vibration sample")
						}
						compareSculkContactNativeBlock(t, tx, sample.Tick, *trace.Sensor, sample.Sensor.nativeTripwireBlock)
						compareSculkContactNativeBlock(t, tx, sample.Tick, *trace.SensorInput, sample.SensorInput.nativeTripwireBlock)
						compareSculkContactNativeBlock(t, tx, sample.Tick, *trace.SensorOutput, sample.SensorOutput.nativeTripwireBlock)
						if power := tx.Block(*trace.Sensor).(CalibratedSculkSensor).OutputPower(); power != *sample.Sensor.Power {
							t.Errorf("tick %d piston vibration strength=%d, native %d", sample.Tick, power, *sample.Sensor.Power)
						}
					}
					if len(sample.Extra) != len(trace.ExtraProbes) {
						t.Fatal("missing native branch observations")
					}
					for i, pos := range trace.ExtraProbes {
						compareSculkContactNativeBlock(t, tx, sample.Tick, pos, sample.Extra[i].nativeTripwireBlock)
					}
					for _, change := range trace.Changes {
						if change.Tick == sample.Tick {
							var b world.Block = Air{}
							if change.Powered {
								b = RedstoneBlock{}
							}
							if c.Kind == "lever" {
								b = Lever{Facing: cube.Face(c.Input), Direction: cube.North, Powered: change.Powered}
							}
							tx.SetBlock(trace.Source, b, nil)
						}
					}
					for _, injection := range trace.Injections {
						if injection.Tick == sample.Tick {
							b := pistonNativeBlock(byName, injection.Block)
							if b == nil {
								t.Fatalf("unmapped native injected block %s", injection.Block.ID)
							}
							tx.SetBlock(injection.Pos, b, nil)
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

// Match every stored palette property against the authored native permutation.
// Extra Script API legacy aliases are checked by the frame comparison itself.
func pistonNativeBlock(byName map[string][]world.Block, expected nativeTripwireBlock) world.Block {
	for _, candidate := range byName[expected.ID] {
		_, properties := candidate.EncodeBlock()
		matches := true
		for key, value := range properties {
			var normalized any = value
			switch value := value.(type) {
			case int32:
				normalized = float64(value)
			case uint8:
				normalized = value != 0
			}
			if normalized != expected.States[key] {
				matches = false
				break
			}
		}
		if matches {
			return candidate
		}
	}
	return nil
}

func comparePistonNativeActor(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, expected *nativePistonActor) {
	t.Helper()
	p, ok := tx.Block(pos).(Piston)
	if expected == nil {
		if ok {
			t.Errorf("tick %d at %v has unexpected piston actor", tick, pos)
		}
		return
	}
	if !ok {
		t.Errorf("tick %d at %v missing native piston actor", tick, pos)
		return
	}
	states := [...]string{"Retracted", "Expanding", "Expanded", "Retracting"}
	if int(p.State) >= len(states) || states[p.State] != expected.State || p.Moving != expected.Moving {
		t.Errorf("tick %d at %v state=%d moving=%t, native %s moving=%t", tick, pos, p.State, p.Moving, expected.State, expected.Moving)
	}
	attached := make([]cube.Pos, 0, len(expected.Attached))
	for _, v := range expected.Attached {
		attached = append(attached, cube.Pos{v.X, v.Y, v.Z})
	}
	if !slices.Equal(p.Attached, attached) {
		t.Errorf("tick %d at %v attached=%v, native %v", tick, pos, p.Attached, attached)
	}
}
