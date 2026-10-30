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
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

type crafterSoundObserver struct {
	world.NopHandler
	sounds    []sound.NativeEvent
	positions []mgl64.Vec3
}

func (o *crafterSoundObserver) HandleSound(_ *world.Context, s world.Sound, p mgl64.Vec3) {
	if event, ok := s.(sound.NativeEvent); ok {
		o.sounds = append(o.sounds, event)
		o.positions = append(o.positions, p)
	}
}

func TestCrafterSoundsMatchNativePackets(t *testing.T) {
	nativeRecipesOnce.Do(func() { world.DefaultBlockRegistry.Finalize(); registerNativeVanillaRecipes() })
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_crafter_sounds.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Recipe string
				Phase  int
			}
			Base, Source, Target cube.Pos
			SetupTick            int64 `json:"setup_tick"`
			Changes              []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick    int64
				Block   pistonEntityNativeBlock
				Outputs int
			}
			Sounds []struct {
				SoundType, EntityType string
				ExtraData             int32
				EntityUniqueID        int64
				Position              mgl64.Vec3
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 12 {
		t.Fatal("incomplete native crafter sound capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/phase%d", c.Recipe, c.Phase), func(t *testing.T) {
			if len(trace.Changes) != 2 || len(trace.Samples) != 100 || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("invalid native sound controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			observer := &crafterSoundObserver{}
			w.Handle(observer)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				crafter := block.NewCrafter()
				inv := crafter.Inventory(tx, trace.Base)
				switch c.Recipe {
				case "valid", "valid_air", "valid_full":
					for slot := range 9 {
						_ = inv.SetItem(slot, item.NewStack(item.IronIngot{}, 1))
					}
				case "invalid":
					for slot := range 9 {
						_ = inv.SetItem(slot, item.NewStack(block.Dirt{}, 1))
					}
				case "empty":
				case "cake_air":
					for slot, name := range []string{"milk_bucket", "milk_bucket", "milk_bucket", "sugar", "egg", "sugar", "wheat", "wheat", "wheat"} {
						it, ok := world.ItemByName("minecraft:"+name, 0)
						if !ok {
							t.Fatal("native cake ingredient not registered")
						}
						_ = inv.SetItem(slot, item.NewStack(it, 1))
					}
				default:
					t.Fatal("unknown native sound recipe")
				}
				tx.SetBlock(trace.Base, crafter, nil)
				if c.Recipe != "valid_air" && c.Recipe != "cake_air" {
					barrel := block.NewBarrel()
					if c.Recipe == "valid_full" {
						for slot := range 27 {
							_ = barrel.Inventory(tx, trace.Target).SetItem(slot, item.NewStack(block.Stone{}, 64))
						}
					}
					tx.SetBlock(trace.Target, barrel, nil)
				}
			})
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native sound scene clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, frame.Block)
					if b, ok := tx.Block(trace.Target).(block.Barrel); ok {
						s, _ := b.Inventory(tx, trace.Target).Item(0)
						if s.Count() != frame.Outputs {
							t.Errorf("tick %d output=%d, native=%d", frame.Tick, s.Count(), frame.Outputs)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var b world.Block
							if change.Powered {
								b = block.RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, b, nil)
						}
					}
				})
				if t.Failed() {
					break
				}
			}
			if len(observer.sounds) != len(trace.Sounds) {
				t.Fatalf("sound events=%v, native=%v", observer.sounds, trace.Sounds)
			}
			for i, want := range trace.Sounds {
				got := observer.sounds[i]
				if got.SoundType != want.SoundType || got.EntityType != want.EntityType || got.ExtraData != want.ExtraData || got.EntityUniqueID != want.EntityUniqueID || observer.positions[i] != want.Position {
					t.Errorf("sound=%+v at %v, native=%+v", got, observer.positions[i], want)
				}
			}
		})
	}
}
