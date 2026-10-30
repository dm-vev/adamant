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

// The stable native API exposes crafter permutations, but not its inventory.
// A barrel receives outputs; authored structures supply the input grid.
func TestCrafterPulsesMatchBDS(t *testing.T) {
	nativeRecipesOnce.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		registerNativeVanillaRecipes()
	})
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_crafter_pulses.json")
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
				Recipe string
				Phase  int
				Edges  []int
			}
			Base, Source cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			LoadTick     int64 `json:"load_tick"`
			Changes      []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick  int64
				Block pistonEntityNativeBlock
				Items []*nativeItem
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 36 {
		t.Fatal("incomplete native crafter pulse capture")
	}
	for _, trace := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/phase%d/edges%v", trace.Scenario.Recipe, trace.Scenario.Phase, trace.Scenario.Edges), func(t *testing.T) {
			if len(trace.Samples) != 128 || len(trace.Changes) != len(trace.Scenario.Edges) || trace.Changes[0].Tick%2 != int64(trace.Scenario.Phase) || trace.LoadTick != trace.SetupTick+40 {
				t.Fatal("invalid native crafter pulse controls")
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
			dest := p.Side(cube.FaceNorth)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, p.Vec3Centre())
				loader.Load(tx, 25)
				tx.SetBlock(p, block.NewCrafter(), nil)
				tx.SetBlock(dest, block.NewBarrel(), nil)
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("crafter pulse clock mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, p, frame.Block)
					inv := tx.Block(dest).(block.Barrel).Inventory(tx, dest)
					if len(frame.Items) != inv.Size() {
						t.Fatal("invalid native output slot count")
					}
					for slot, want := range frame.Items {
						got, _ := inv.Item(slot)
						if want == nil {
							if !got.Empty() {
								t.Errorf("tick %d output slot %d=%v, native empty", frame.Tick, slot, got)
							}
						} else if got.Empty() {
							t.Errorf("tick %d output slot %d empty, native=%v", frame.Tick, slot, want)
						} else if name, _ := got.Item().EncodeItem(); name != want.ID || got.Count() != want.Count {
							t.Errorf("tick %d output slot %d=%v, native=%v", frame.Tick, slot, got, want)
						}
					}
					if frame.Tick == trace.LoadTick {
						c := block.NewCrafter()
						var input world.Item = item.IronIngot{}
						if trace.Scenario.Recipe == "invalid" {
							input = block.Dirt{}
						} else if trace.Scenario.Recipe != "iron_block" {
							t.Fatal("unknown native recipe input")
						}
						for slot := range 9 {
							_ = c.Inventory(tx, p).SetItem(slot, item.NewStack(input, 8))
						}
						tx.SetBlock(p, c, nil)
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block
							if change.Powered {
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
