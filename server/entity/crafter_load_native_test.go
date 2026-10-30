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

func TestCrafterSavedAnimationMatchesBDS(t *testing.T) {
	nativeRecipesOnce.Do(func() { world.DefaultBlockRegistry.Finalize(); registerNativeVanillaRecipes() })
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_crafter_loads.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario            struct{ Age, Phase int }
			Base, Source, Clone cube.Pos
			SetupTick           int64 `json:"setup_tick"`
			FirstCraft          int64 `json:"first_craft"`
			Load                struct {
				Tick         int64
				Block, Clone pistonEntityNativeBlock
				Actor        struct {
					ID            string
					Items         []any
					DisabledSlots int `json:"disabled_slots"`
				}
			}
			Changes []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick         int64
				Block, Clone pistonEntityNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 12 {
		t.Fatal("incomplete native crafter save/load corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("age%d/phase%d", c.Age, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 112 || len(trace.Changes) != 2 || trace.SetupTick%20 != 2 || trace.Changes[0].Tick%2 != int64(c.Phase) || trace.Load.Tick != trace.FirstCraft+int64(c.Age) || trace.Load.Actor.ID != "Crafter" || len(trace.Load.Actor.Items) != 0 || trace.Load.Actor.DisabledSlots != 0 {
				t.Fatal("native crafter save/load controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			settings.WeatherCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				b := block.NewCrafter()
				for slot := range 9 {
					_ = b.Inventory(tx, trace.Base).SetItem(slot, item.NewStack(item.IronIngot{}, 1))
				}
				tx.SetBlock(trace.Base, b, nil)
				tx.SetBlock(trace.Base.Side(cube.FaceNorth), block.NewBarrel(), nil)
				tx.SetBlock(trace.Clone.Side(cube.FaceNorth), block.NewBarrel(), nil)
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native crafter save/load clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, frame.Block)
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Clone, frame.Clone)
					if frame.Tick == trace.Load.Tick {
						comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, trace.Load.Block)
						live := tx.Block(trace.Base).(block.Crafter)
						name, states := live.EncodeBlock()
						b, ok := tx.World().BlockRegistry().BlockByName(name, states)
						if !ok {
							t.Fatal("saved crafter permutation missing")
						}
						restored := b.(block.Crafter).DecodeNBT(live.EncodeNBT()).(block.Crafter)
						tx.SetBlock(trace.Clone, restored, nil)
						comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Clone, trace.Load.Clone)
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var power world.Block
							if change.Powered {
								power = block.RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, power, nil)
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
