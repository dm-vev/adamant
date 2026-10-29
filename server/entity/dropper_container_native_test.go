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

func TestDropperContainerActionsMatchBDS(t *testing.T) {
	replayContainerActionsBDS(t, "bedrock_1_26_51_dropper_containers.json", 96, 4)
}

func TestHopperTransferPulsesMatchBDS(t *testing.T) {
	replayContainerActionsBDS(t, "bedrock_1_26_51_hopper_transfer_pulses.json", 30, 4)
}

func TestHopperIdleTransfersMatchBDS(t *testing.T) {
	replayContainerActionsBDS(t, "bedrock_1_26_51_hopper_idle_even.json", 40, 2)
	replayContainerActionsBDS(t, "bedrock_1_26_51_hopper_idle_odd.json", 40, 2)
}

func TestHopperChainTransfersMatchBDS(t *testing.T) {
	replayContainerActionsBDS(t, "bedrock_1_26_51_hopper_chains.json", 60, 2)
}

func replayContainerActionsBDS(t *testing.T, filename string, count, changes int) {
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	type nativeItem struct {
		ID    string
		Count int
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Kind, Target, Fill string
				Face               int
				InputPhase         int `json:"input_phase"`
				Load               int
				SetupPhase         int `json:"phase"`
				Count, Offset      int
				Reverse            bool
			}
			Base, Target, Source cube.Pos
			Lock                 *cube.Pos
			Destination          *cube.Pos
			SetupTick            int64 `json:"setup_tick"`
			Changes              []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick             int64
				Block, Target    pistonEntityNativeBlock
				Items            []*nativeItem
				TargetItems      []*nativeItem `json:"target_items"`
				Drops            []nativeItem
				Destination      *pistonEntityNativeBlock
				DestinationItems []*nativeItem `json:"destination_items"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native dropper/container capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/face%d/%s/input%d/setup%d/load%d/count%d/offset%d/reverse%t", c.Kind, c.Target, c.Face, c.Fill, c.InputPhase, c.SetupPhase, c.Load, c.Count, c.Offset, c.Reverse), func(t *testing.T) {
			if len(trace.Samples) != 128 || len(trace.Changes) != changes || trace.SetupTick%2 != int64(c.SetupPhase) {
				t.Fatal("invalid native container controls")
			}
			// The native API reports the deprecated facing_direction alias
			// alongside cardinal_direction for chests/furnaces. Check that they
			// agree before comparing the canonical property used by the kernel.
			for _, frame := range trace.Samples {
				props := frame.Target.States
				if cardinal, ok := props["minecraft:cardinal_direction"].(string); ok {
					faces := map[string]int{"north": 2, "south": 3, "west": 4, "east": 5}
					face, known := faces[cardinal]
					if legacy, ok := props["facing_direction"].(float64); !ok || !known || int(legacy) != face {
						t.Fatal("native direction aliases disagree")
					}
					delete(props, "facing_direction")
				}
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
				var origin world.Block
				if c.Kind == "dropper" {
					d := block.NewDropper()
					d.Facing = cube.Face(c.Face)
					origin = d
				} else if c.Kind == "dispenser" {
					d := block.NewDispenser()
					d.Facing = cube.Face(c.Face)
					origin = d
				} else if c.Kind == "hopper" {
					h := block.NewHopper()
					h.Facing = cube.Face(c.Face)
					origin = h
				} else {
					t.Fatal("unknown native source container")
				}
				if !c.Reverse {
					tx.SetBlock(p, origin, nil)
				}
				if trace.Destination != nil {
					tx.SetBlock(*trace.Destination, block.NewBarrel(), nil)
				} else if c.Face != 1 {
					tx.SetBlock(trace.Target.Side(cube.FaceDown), block.Stone{}, nil)
				}
				var target world.Block
				switch c.Target {
				case "barrel":
					target = block.NewBarrel()
				case "chest":
					target = block.NewChest()
				case "hopper":
					target = block.NewHopper()
				case "furnace":
					target = block.NewFurnace(cube.South)
				case "brewing_stand":
					target = block.NewBrewingStand()
				default:
					t.Fatal("unknown native target container")
				}
				tx.SetBlock(trace.Target, target, nil)
				if c.Reverse {
					tx.SetBlock(p, origin, nil)
				}
				if trace.Lock != nil {
					tx.SetBlock(*trace.Lock, block.RedstoneBlock{}, nil)
				}
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native container clock mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, p, frame.Block)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Target, frame.Target)
					compare := func(pos cube.Pos, want []*nativeItem) {
						slots := tx.Block(pos).(block.Container).Inventory(tx, pos).Slots()
						if len(slots) != len(want) {
							t.Fatal("native container capacity differs")
						}
						for i, stack := range slots {
							if want[i] == nil {
								if !stack.Empty() {
									t.Errorf("tick %d %v slot %d: want empty, got %v", frame.Tick, pos, i, stack)
								}
								continue
							}
							id := ""
							if !stack.Empty() {
								id, _ = stack.Item().EncodeItem()
							}
							if id != want[i].ID || stack.Count() != want[i].Count {
								t.Errorf("tick %d %v slot %d: got %v, native=%+v", frame.Tick, pos, i, stack, want[i])
							}
						}
					}
					compare(p, frame.Items)
					compare(trace.Target, frame.TargetItems)
					if trace.Destination != nil {
						if frame.Destination == nil {
							t.Fatal("missing native chain destination")
						}
						comparePistonEntityNativeBlock(t, tx, frame.Tick, *trace.Destination, *frame.Destination)
						compare(*trace.Destination, frame.DestinationItems)
					}
					drops := 0
					for e := range tx.Entities() {
						if e.H().Type() == ItemType {
							s := e.(*Ent).Behaviour().(*ItemBehaviour).Item()
							if _, ok := s.Item().(item.Diamond); !ok {
								t.Fatal("unexpected dropped item")
							}
							drops += s.Count()
						}
					}
					wantDrops := 0
					for _, s := range frame.Drops {
						if s.ID != "minecraft:diamond" {
							t.Fatal("unknown native dropped item")
						}
						wantDrops += s.Count
					}
					if drops != wantDrops {
						t.Errorf("tick %d drops=%d native=%d", frame.Tick, drops, wantDrops)
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							switch change.Kind {
							case "load":
								inv := tx.Block(p).(block.Container).Inventory(tx, p)
								n := c.Count
								if n == 0 {
									n = 4
								}
								if err := inv.SetItem(0, item.NewStack(item.Diamond{}, n)); err != nil {
									t.Fatal(err)
								}
								if c.Fill != "empty" {
									inv = tx.Block(trace.Target).(block.Container).Inventory(tx, trace.Target)
									for i := range inv.Size() {
										n := 64
										if c.Fill == "partial" && i == inv.Size()-1 {
											n--
										}
										if err := inv.SetItem(i, item.NewStack(item.Diamond{}, n)); err != nil {
											t.Fatal(err)
										}
									}
								}
							case "power":
								tx.SetBlock(trace.Source, block.RedstoneBlock{}, nil)
							case "off":
								tx.SetBlock(trace.Source, nil, nil)
							default:
								t.Fatal("unknown native container change")
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
