package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This compares settled analog output, not minecart travel or update timing.
func TestDetectorRailInventorySignalMatchesBDS(t *testing.T) {
	testDetectorRailSettledNative(t, "bedrock_1_26_51_detector_rail_inventory.json", 15)
}

func TestCommandCartInventorySignalMatchesBDS(t *testing.T) {
	testDetectorRailSettledNative(t, "bedrock_1_26_51_command_cart_inventory.json", 5)
}

func TestDetectorRailTimingMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_detector_rail_timing.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Entity       string
				Phase, Dwell int
			}
			SetupTick int64 `json:"setup_tick"`
			Changes   []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick               int64
				Source, Wire, Lamp pistonEntityNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 32 {
		t.Fatal("incomplete native detector timing capture")
	}
	for _, trace := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/phase%d/dwell%d", trace.Scenario.Entity, trace.Scenario.Phase, trace.Scenario.Dwell), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 || trace.Changes[0].Tick&1 != int64(trace.Scenario.Phase) || trace.Changes[1].Tick-trace.Changes[0].Tick != int64(trace.Scenario.Dwell) {
				t.Fatal("invalid native timing trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, mgl64.Vec3{.5, 65, .5})
				loader.Load(tx, 25)
				for x := -3; x <= 4; x++ {
					for z := -3; z <= 3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(cube.Pos{0, 65, 0}, block.DetectorRail{}, nil)
				tx.SetBlock(cube.Pos{1, 65, 0}, block.RedstoneWire{}, nil)
				tx.SetBlock(cube.Pos{2, 65, 0}, block.RedstoneLamp{}, nil)
			})
			var handle *world.EntityHandle
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native rail clock mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, cube.Pos{0, 65, 0}, frame.Source)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, cube.Pos{1, 65, 0}, frame.Wire)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, cube.Pos{2, 65, 0}, frame.Lamp)
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							switch change.Kind {
							case "spawn":
								opts := world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 65.0625, .5}}
								if trace.Scenario.Entity == "minecraft:chest_minecart" {
									handle = opts.New(ChestMinecartType, chestMinecartConf)
								} else {
									handle = opts.New(MinecartType, minecartConf)
								}
								tx.AddEntity(handle)
							case "remove":
								e, ok := handle.Entity(tx)
								if !ok {
									t.Fatal("rail cart disappeared before removal")
								}
								tx.RemoveEntity(e)
							default:
								t.Fatal("unknown native change", change.Kind)
							}
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

func TestDetectorRailBoundaryMatchesBDS(t *testing.T) {
	testDetectorRailSettledNative(t, "bedrock_1_26_51_detector_rail_boundary.json", 51)
}

func testDetectorRailSettledNative(t *testing.T, fixture string, count int) {
	data, err := os.ReadFile("../block/testdata/" + fixture)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct{ Entity string }
			Samples  []struct {
				Source    pistonEntityNativeBlock
				Wire      pistonEntityNativeBlock
				Body      pistonEntityNativeBody
				Inventory *struct {
					Size   int
					Stacks []struct {
						Slot       int
						ID         string
						Count, Max int
					}
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete original-BDS rail inventory capture")
	}
	for i, trace := range native.Scenarios {
		t.Run(fmt.Sprintf("%02d/%s", i, trace.Scenario.Entity), func(t *testing.T) {
			if len(trace.Samples) != 32 {
				t.Fatal("incomplete settled trace")
			}
			sample := trace.Samples[len(trace.Samples)-1]
			w := world.Config{Synchronous: true, Entities: DefaultRegistry}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				opts := world.EntitySpawnOpts{Position: sample.Body.Position.vec()}
				var handle *world.EntityHandle
				switch trace.Scenario.Entity {
				case "minecraft:minecart":
					handle = opts.New(MinecartType, minecartConf)
				case "minecraft:tnt_minecart":
					handle = opts.New(TNTMinecartType, tntMinecartConf)
				case "minecraft:command_block_minecart":
					handle = NewCommandBlockMinecart(opts, CommandBlockMinecartConfig{})
				case "minecraft:chest_minecart":
					handle = opts.New(ChestMinecartType, chestMinecartConf)
				case "minecraft:hopper_minecart":
					handle = opts.New(HopperMinecartType, hopperMinecartConf)
				case "minecraft:armor_stand":
					handle = NewArmourStand(opts)
				default:
					t.Fatal("unhandled native entity", trace.Scenario.Entity)
				}
				e := tx.AddEntity(handle)
				if sample.Inventory != nil {
					inv := e.(interface{ Inventory() *inventory.Inventory }).Inventory()
					if inv.Size() != sample.Inventory.Size {
						t.Fatal("inventory size differs from BDS")
					}
					for _, stack := range sample.Inventory.Stacks {
						it, ok := world.ItemByName(stack.ID, 0)
						if !ok {
							t.Fatal("unregistered native item", stack.ID)
						}
						s := item.NewStack(it, stack.Count)
						if s.MaxCount() != stack.Max {
							t.Fatal("native item stack limit differs")
						}
						if err := inv.SetItem(stack.Slot, s); err != nil {
							t.Fatal(err)
						}
					}
				}
				rail := block.DetectorRail{Powered: sample.Source.States["rail_data_bit"] == true}
				pos := cube.Pos{-1, 65, 0}
				tx.SetBlock(pos, rail, nil)
				out, ok := any(rail).(interface {
					ComparatorOutput(*world.Tx, cube.Pos) uint8
				})
				if !ok {
					t.Fatal("detector rail does not provide comparator output")
				}
				want := uint8(sample.Wire.States["redstone_signal"].(float64))
				if got := out.ComparatorOutput(tx, pos); got != want {
					t.Fatalf("analog signal=%d, BDS=%d", got, want)
				}
				// Force a presence resample from the opposite power state. This
				// checks the selection used by scheduled rail updates separately
				// from the analog getter, without claiming native tick timing.
				opposite := rail
				opposite.Powered = !rail.Powered
				tx.SetBlock(pos, opposite, nil)
				opposite.ScheduledTick(pos, tx, nil)
				if got := tx.Block(pos).(block.DetectorRail).Powered; got != rail.Powered {
					t.Fatalf("rail presence=%t, BDS=%t", got, rail.Powered)
				}
			})
		})
	}
}

func TestDetectorRailProducerLifecycleMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_detector_rail_lifecycle.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Powered, Replace bool
				Direction        int
				EdgePhase        int `json:"edge_phase"`
			}
			SetupTick          int64 `json:"setup_tick"`
			Source, Wire, Lamp cube.Pos
			Changes            []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick               int64
				Source, Wire, Lamp pistonEntityNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 16 {
		t.Fatal("incomplete native detector producer capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("powered%t/replace%t/phase%d/dir%d", c.Powered, c.Replace, c.EdgePhase, c.Direction), func(t *testing.T) {
			if len(trace.Samples) != 80 || len(trace.Changes) != 3 || trace.Changes[1].Tick&1 != int64(c.EdgePhase) {
				t.Fatal("invalid native producer trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Source.Vec3Centre())
				loader.Load(tx, 25)
				for x := trace.Source[0] - 3; x <= trace.Source[0]+4; x++ {
					for z := trace.Source[2] - 3; z <= trace.Source[2]+3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(trace.Wire, block.RedstoneWire{}, nil)
				tx.SetBlock(trace.Lamp, block.RedstoneLamp{}, nil)
				tx.SetBlock(trace.Source, block.DetectorRail{Powered: c.Powered}, nil)
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native producer clock mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Source, frame.Source)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Wire, frame.Wire)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Lamp, frame.Lamp)
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							switch change.Kind {
							case "edit":
								if c.Replace {
									tx.SetBlock(trace.Source, block.Air{}, nil)
								}
								tx.SetBlock(trace.Source, block.DetectorRail{Powered: !c.Powered, Direction: block.RailDirection(c.Direction)}, nil)
							case "reset":
								tx.SetBlock(trace.Source, block.Air{}, nil)
								tx.SetBlock(trace.Source, block.DetectorRail{Powered: c.Powered}, nil)
							default:
								t.Fatal("unexpected native producer change", change.Kind)
							}
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
