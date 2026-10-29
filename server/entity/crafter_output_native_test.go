package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestCrafterContainerOutputsMatchBDS(t *testing.T) {
	replayCrafterOutputsBDS(t, "bedrock_1_26_51_crafter_outputs.json", 144)
}

func TestCrafterRemainderOutputsMatchBDS(t *testing.T) {
	replayCrafterOutputsBDS(t, "bedrock_1_26_51_crafter_remainders.json", 96)
}

func replayCrafterOutputsBDS(t *testing.T, filename string, count int) {
	t.Helper()
	nativeRecipesOnce.Do(func() { world.DefaultBlockRegistry.Finalize(); registerNativeVanillaRecipes() })
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
				Recipe, Target, Fill, Orientation string
				Face                              int
				InitialSource                     []*nativeItem `json:"initial_source"`
				InitialTarget                     []*nativeItem `json:"initial_target"`
			}
			Base, Target, Source cube.Pos
			Lock                 *cube.Pos
			SetupTick            int64         `json:"setup_tick"`
			InitialSource        []*nativeItem `json:"initial_source"`
			FinalSource          []*nativeItem `json:"final_source"`
			LoadControl          []*nativeItem `json:"load_control"`
			Changes              []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick          int64
				Block, Target pistonEntityNativeBlock
				TargetItems   []*nativeItem `json:"target_items"`
				Drops         []nativeItem
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native crafter output corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("face%d/%s/%s/%s", c.Face, c.Recipe, c.Target, c.Fill), func(t *testing.T) {
			if len(trace.Samples) != 128 || len(trace.Changes) != 4 || trace.SetupTick%20 != 2 || !reflect.DeepEqual(trace.InitialSource, c.InitialSource) || !reflect.DeepEqual(trace.LoadControl, c.InitialTarget) {
				t.Fatal("invalid native output controls")
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
			newCrafter := func() block.Crafter {
				b := block.NewCrafter()
				b.Facing = cube.Face(c.Face)
				if b.Facing.Axis() == cube.Y {
					b.Top = cube.FaceNorth
				}
				_, states := b.EncodeBlock()
				if states["orientation"] != c.Orientation {
					t.Fatal("native crafter orientation control differs")
				}
				return b
			}
			fill := func(tx *world.Tx, pos cube.Pos, items []*nativeItem) {
				inv := tx.Block(pos).(block.Container).Inventory(tx, pos)
				if inv.Size() != len(items) {
					t.Fatal("native input capacity differs")
				}
				for slot, s := range items {
					if s == nil {
						continue
					}
					it, ok := world.ItemByName(s.ID, 0)
					if !ok {
						t.Fatalf("unregistered native input %s", s.ID)
					}
					if err := inv.SetItem(slot, item.NewStack(it, s.Count)); err != nil {
						t.Fatal(err)
					}
				}
			}
			compare := func(tx *world.Tx, tick int64, pos cube.Pos, want []*nativeItem) {
				slots := tx.Block(pos).(block.Container).Inventory(tx, pos).Slots()
				if len(slots) != len(want) {
					t.Fatal("native slot capacity differs")
				}
				for slot, s := range want {
					got := slots[slot]
					if s == nil {
						if !got.Empty() {
							t.Errorf("tick %d at %v slot %d=%v, native empty", tick, pos, slot, got)
						}
						continue
					}
					if got.Empty() {
						t.Errorf("tick %d at %v slot %d empty, native=%v", tick, pos, slot, s)
						continue
					}
					name, _ := got.Item().EncodeItem()
					if name != s.ID || got.Count() != s.Count {
						t.Errorf("tick %d at %v slot %d=%v, native=%v", tick, pos, slot, got, s)
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, p.Vec3Centre())
				loader.Load(tx, 25)
				for x := -8; x <= 8; x++ {
					for z := -8; z <= 8; z++ {
						tx.SetBlock(cube.Pos{p[0] + x, 63, p[2] + z}, block.Bedrock{}, nil)
					}
				}
				tx.SetBlock(p, newCrafter(), nil)
				if c.Face != 1 {
					tx.SetBlock(trace.Target.Side(cube.FaceDown), block.Stone{}, nil)
				}
				var dest world.Block
				switch c.Target {
				case "barrel":
					dest = block.NewBarrel()
				case "chest":
					dest = block.NewChest()
				case "hopper":
					dest = block.NewHopper()
				case "furnace":
					dest = block.NewFurnace(cube.South)
				case "brewing_stand":
					dest = block.NewBrewingStand()
				default:
					t.Fatal("unknown native crafter destination")
				}
				tx.SetBlock(trace.Target, dest, nil)
				if trace.Lock != nil {
					tx.SetBlock(*trace.Lock, block.RedstoneBlock{}, nil)
				}
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native output clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, p, frame.Block)
					target := frame.Target
					if cardinal, ok := target.States["minecraft:cardinal_direction"].(string); ok {
						faces := map[string]int{"north": 2, "south": 3, "west": 4, "east": 5}
						if legacy, ok := target.States["facing_direction"].(float64); !ok || int(legacy) != faces[cardinal] {
							t.Fatal("native output direction aliases disagree")
						}
						delete(target.States, "facing_direction")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Target, target)
					compare(tx, frame.Tick, trace.Target, frame.TargetItems)
					drops := map[nativeItem]int{}
					wantDrops := map[nativeItem]int{}
					for e := range tx.Entities() {
						if e.H().Type() != ItemType {
							continue
						}
						s := e.(*Ent).Behaviour().(*ItemBehaviour).Item()
						name, _ := s.Item().EncodeItem()
						drops[nativeItem{ID: name, Count: s.Count()}]++
					}
					for _, s := range frame.Drops {
						wantDrops[s]++
					}
					if !reflect.DeepEqual(drops, wantDrops) {
						t.Errorf("tick %d dropped stacks=%v, native=%v", frame.Tick, drops, wantDrops)
					}
					for _, change := range trace.Changes {
						if change.Tick != frame.Tick {
							continue
						}
						switch change.Kind {
						case "load":
							tx.SetBlock(p, newCrafter(), nil)
							fill(tx, p, c.InitialSource)
							fill(tx, trace.Target, c.InitialTarget)
						case "power":
							tx.SetBlock(trace.Source, block.RedstoneBlock{}, nil)
						case "off":
							tx.SetBlock(trace.Source, nil, nil)
						case "setup":
						default:
							t.Fatal("unknown native output change")
						}
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() {
				<-w.Exec(func(tx *world.Tx) { compare(tx, tx.CurrentTick(), p, trace.FinalSource) })
			}
		})
	}
}
