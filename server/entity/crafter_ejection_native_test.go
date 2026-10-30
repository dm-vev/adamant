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
	"github.com/go-gl/mathgl/mgl64"
)

func TestCrafterEjectedItemsMatchOwnedBDS(t *testing.T) {
	replayItemEjectionsOwnedBDS(t, "crafter", "bedrock_1_26_51_crafter_ejection_actor_inputs.json", 12)
}

func TestDispenserDropperEjectedItemsMatchOwnedBDS(t *testing.T) {
	replayItemEjectionsOwnedBDS(t, "ordinary", "bedrock_1_26_51_dispenser_ejection_actor_inputs.json", 24)
}

func replayItemEjectionsOwnedBDS(t *testing.T, kind, filename string, count int) {
	t.Helper()
	nativeRecipesOnce.Do(func() { world.DefaultBlockRegistry.Finalize(); registerNativeVanillaRecipes() })
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	type word struct {
		Value         uint32
		Sequence      int64
		FloatCaller   string `json:"float_caller"`
		Bound         uint32
		BoundedCaller string `json:"bounded_caller"`
	}
	type scope struct {
		Launch    mgl64.Vec3
		Facing    int
		Entry     int64 `json:"entry_sequence"`
		Exit      int64 `json:"exit_sequence"`
		Words     []word
		Selection *word
		Tick      int64
	}
	var native struct {
		Complete    bool
		Cache       struct{ Cached bool } `json:"initial_gaussian_cache"`
		Checkpoints []struct {
			Tick      int64
			Outside   []word
			Ejections []scope
			ItemTicks []struct {
				Sequence    int64
				PickupDelay int `json:"pickup_delay"`
			} `json:"item_post_ticks"`
		}
		Scenarios []struct {
			Scenario struct {
				Face, Phase int
				Orientation string
				Kind        string
			}
			Base, Source cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			Changes      []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick  int64
				Block pistonEntityNativeBlock
				Drops []hopperNativeItemBody
				Slots []*struct {
					ID    string
					Count int
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || native.Cache.Cached || len(native.Checkpoints) != 100 || len(native.Scenarios) != count {
		t.Fatal("incomplete owned native ejection corpus")
	}
	var scopes []scope
	for _, checkpoint := range native.Checkpoints {
		lastExit := int64(0)
		for _, s := range checkpoint.Ejections {
			s.Tick = checkpoint.Tick
			for _, w := range checkpoint.Outside {
				if kind == "ordinary" && w.Sequence == s.Entry-1 {
					if w.Bound != 1 || (w.BoundedCaller != "0x8a2ca55" && w.BoundedCaller != "0x8a226f5") {
						t.Fatal("native ordinary item selection control differs")
					}
					selection := w
					s.Selection = &selection
				}
				if w.Sequence > s.Entry && w.Sequence < s.Exit {
					s.Words = append(s.Words, w)
				}
			}
			if len(s.Words) < 2 || s.Words[0].FloatCaller != "0xd1c84d" || s.Words[1].FloatCaller != "0x8a23385" {
				t.Fatal("native construction/speed float controls differ")
			}
			if kind == "ordinary" && s.Selection == nil {
				t.Fatal("missing native pre-ejection selection")
			}
			scopes = append(scopes, s)
			lastExit = max(lastExit, s.Exit)
		}
		for _, entry := range checkpoint.ItemTicks {
			if entry.PickupDelay != 0 {
				t.Fatal("native crafter item pickup delay differs")
			}
			if entry.Sequence <= lastExit {
				t.Fatal("native item post tick precedes scheduled ejections")
			}
		}
	}
	if len(scopes) != count {
		t.Fatal("missing native ejection scopes")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/face%d/phase%d", c.Kind, c.Face, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 || trace.SetupTick%20 != 2 || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("invalid native ejection controls")
			}
			target := -1
			for i, s := range scopes {
				if s.Launch.Sub(trace.Base.Vec3().Add(mgl64.Vec3{.5, .3, .5})).Len() < .8 {
					if target != -1 || s.Facing != c.Face {
						t.Fatal("ambiguous native source association")
					}
					target = i
				}
			}
			if target < 0 {
				t.Fatal("native source scope missing")
			}
			if kind == "ordinary" {
				wantCaller := "0x8a2ca55"
				if c.Kind == "dispenser" {
					wantCaller = "0x8a226f5"
				}
				if scopes[target].Selection.BoundedCaller != wantCaller {
					t.Fatal("native source/selection kind differs")
				}
			}
			source := &nativeContainerRandom{}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			settings.WeatherCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, RandSource: source, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := -8; x <= 8; x++ {
					for z := -8; z <= 8; z++ {
						tx.SetBlock(cube.Pos{trace.Base[0] + x, 63, trace.Base[2] + z}, block.Bedrock{}, nil)
					}
				}
				if kind == "ordinary" {
					var b world.Block
					switch c.Kind {
					case "dropper":
						d := block.NewDropper()
						d.Facing = cube.Face(c.Face)
						b = d
					case "dispenser":
						d := block.NewDispenser()
						d.Facing = cube.Face(c.Face)
						b = d
					default:
						t.Fatal("unknown native ejection source")
					}
					_ = b.(block.Container).Inventory(tx, trace.Base).SetItem(4, item.NewStack(item.IronIngot{}, 3))
					tx.SetBlock(trace.Base, b, nil)
					return
				}
				b := block.NewCrafter()
				b.Facing = cube.Face(c.Face)
				if b.Facing.Axis() == cube.Y {
					b.Top = cube.FaceNorth
				}
				_, states := b.EncodeBlock()
				if states["orientation"] != c.Orientation {
					t.Fatal("native ejection facing differs")
				}
				for slot := range 9 {
					_ = b.Inventory(tx, trace.Base).SetItem(slot, item.NewStack(item.IronIngot{}, 1))
				}
				tx.SetBlock(trace.Base, b, nil)
			})
			var handle *world.EntityHandle
			for _, frame := range trace.Samples {
				source.words, source.used, source.active = nil, 0, true
				if frame.Tick == scopes[target].Tick {
					for _, s := range scopes[:target+1] {
						if s.Selection != nil {
							source.words = append(source.words, uint64(s.Selection.Value)<<32|8)
						}
						for _, word := range s.Words {
							source.words = append(source.words, uint64(word.Value)<<32|8)
						}
					}
					// Warm only the independently recorded earlier ejections' RNG
					// state. Their actor simulation is outside this isolated scene.
					<-w.Exec(func(tx *world.Tx) {
						for range target {
							if kind == "ordinary" {
								tx.RandomUint32()
							}
							tx.RandomFloat32()
							tx.RandomFloat32()
							for range 3 {
								tx.RandomGaussianFloat32()
							}
						}
					})
				}
				w.AdvanceTick()
				source.active = false
				if source.used != len(source.words) {
					t.Fatalf("tick %d consumed %d native words, expected %d", frame.Tick, source.used, len(source.words))
				}
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native item scene clock differs")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, frame.Block)
					if kind == "ordinary" {
						if len(frame.Slots) != 9 {
							t.Fatal("missing native source slots")
						}
						inv := tx.Block(trace.Base).(block.Container).Inventory(tx, trace.Base)
						for slot, want := range frame.Slots {
							stack, _ := inv.Item(slot)
							if want == nil {
								if !stack.Empty() {
									t.Fatal("unexpected source stack")
								}
								continue
							}
							name, _ := stack.Item().EncodeItem()
							if name != want.ID || stack.Count() != want.Count {
								t.Fatalf("tick %d source slot %d differs", frame.Tick, slot)
							}
						}
					}
					var drops []world.Entity
					for e := range tx.Entities() {
						if e.H().Type() == ItemType {
							drops = append(drops, e)
						}
					}
					if len(drops) != len(frame.Drops) {
						t.Fatalf("tick %d item actors=%d, native=%d", frame.Tick, len(drops), len(frame.Drops))
					}
					if len(drops) > 1 {
						t.Fatal("unexpected multiple native output actors")
					}
					if len(drops) == 1 {
						e, want := drops[0], frame.Drops[0]
						if handle != nil && handle != e.H() {
							t.Fatal("native item identity was replaced")
						}
						handle = e.H()
						stack := e.(*Ent).Behaviour().(*ItemBehaviour).Item()
						if e.(*Ent).Behaviour().(*ItemBehaviour).pickupDelay != 0 {
							t.Fatal("ejected item retained a pickup delay")
						}
						name, _ := stack.Item().EncodeItem()
						if name != want.ItemID || stack.Count() != want.Count {
							t.Fatal("native emitted stack differs")
						}
						compareMinecartMotionNativeBody(t, frame.Tick, e, e.Position(), want.minecartMotionNativeBody)
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
		})
	}
}
