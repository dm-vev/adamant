package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestDispenserDropperPulsesMatchBDS(t *testing.T) {
	replayDispenserPulsesBDS(t, "bedrock_1_26_51_dispenser_pulses.json", 288)
}

func TestDispenserDropperSwitchPulsesMatchBDS(t *testing.T) {
	replayDispenserPulsesBDS(t, "bedrock_1_26_51_dispenser_switch_pulses.json", 144)
}

func TestHopperLockPulsesMatchBDS(t *testing.T) {
	replayDispenserPulsesBDS(t, "bedrock_1_26_51_hopper_pulses.json", 144)
}

func replayDispenserPulsesBDS(t *testing.T, filename string, count int) {
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				SourceKind string `json:"source_kind"`
				Kind       string
				Face       int
				Conducted  bool
				Phase      int
				Edges      []int
			}
			Base, Source, Support cube.Pos
			SetupTick             int64 `json:"setup_tick"`
			LoadTick              int64 `json:"load_tick"`
			Changes               []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick   int64
				Block  pistonEntityNativeBlock
				Source *pistonEntityNativeBlock
				Items  int
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native dispenser/dropper pulse capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/face%d/conducted%t/phase%d/edges%v", c.Kind, c.Face, c.Conducted, c.Phase, c.Edges), func(t *testing.T) {
			if len(trace.Samples) != 128 || len(trace.Changes) != len(c.Edges) || trace.Changes[0].Tick%2 != int64(c.Phase) || trace.LoadTick != trace.SetupTick+40 {
				t.Fatal("invalid native pulse controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			if err := w.SetGameRule("spawn_mobs", false); err != nil {
				t.Fatal(err)
			}
			p := trace.Base
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, p.Vec3Centre())
				loader.Load(tx, 25)
				for x := -8; x <= 8; x++ {
					for z := -8; z <= 8; z++ {
						tx.SetBlock(cube.Pos{p[0] + x, 63, p[2] + z}, block.Bedrock{}, nil)
					}
				}
				switch c.Kind {
				case "dispenser":
					d := block.NewDispenser()
					d.Facing = cube.FaceNorth
					tx.SetBlock(p, d, nil)
				case "dropper":
					d := block.NewDropper()
					d.Facing = cube.FaceNorth
					tx.SetBlock(p, d, nil)
				case "hopper":
					h := block.NewHopper()
					h.Facing = cube.FaceNorth
					tx.SetBlock(p, h, nil)
				default:
					t.Fatal("unknown native dispenser type")
				}
				if c.Conducted {
					tx.SetBlock(trace.Support, block.Stone{}, nil)
				}
				if c.SourceKind == "stone_button" {
					tx.SetBlock(trace.Source, block.Button{Type: block.StoneButton(), Facing: cube.Face(c.Face)}, nil)
				} else if c.SourceKind != "" {
					t.Fatal("unknown native pulse source")
				}
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native pulse clock mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, p, frame.Block)
					if frame.Source != nil {
						comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Source, *frame.Source)
					}
					inv := tx.Block(p).(block.Container).Inventory(tx, p)
					if got := inventoryCount(inv.Slots()); got != frame.Items {
						t.Errorf("tick %d items=%d, native=%d", frame.Tick, got, frame.Items)
					}
					if frame.Tick == trace.LoadTick {
						if err := inv.SetItem(0, item.NewStack(item.Diamond{}, 64)); err != nil {
							t.Fatal(err)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block
							if c.SourceKind == "stone_button" {
								// The native capture recreates the source. Setting
								// only pressed_bit does not update a live producer.
								tx.SetBlock(trace.Source, nil, nil)
								source = block.Button{Type: block.StoneButton(), Facing: cube.Face(c.Face), Pressed: change.Powered}
							} else if change.Powered {
								source = block.RedstoneBlock{}
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
