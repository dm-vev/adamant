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

func TestActivatorCartEffectsMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_activator_cart_effects.json")
	if err != nil {
		t.Fatal(err)
	}
	type body struct {
		minecartMotionNativeBody
		Items, Riders int
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Entity, Mode string
				InputPhase   int `json:"input_phase"`
				Rider        bool
			}
			Base      cube.Pos
			SetupTick int64 `json:"setup_tick"`
			Spawn     struct {
				Tick int64
				Body body
			}
			Changes []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick        int64
				Body        *body
				Rail        pistonEntityNativeBlock
				SourceItems *int `json:"source_items"`
			}
			Explosions []struct {
				Tick   int64
				Source string
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 24 {
		t.Fatal("incomplete native activator capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/phase%d/rider%t", c.Entity, c.Mode, c.InputPhase, c.Rider), func(t *testing.T) {
			if len(trace.Samples) != 200 || trace.Spawn.Tick != trace.SetupTick+40 {
				t.Fatal("invalid native activator trace")
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
			chestPos := p.Side(cube.FaceUp)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, p.Vec3Centre())
				loader.Load(tx, 25)
				for x := -3; x <= 3; x++ {
					for z := -5; z <= 5; z++ {
						tx.SetBlock(p.Add(cube.Pos{x, -1, z}), block.Bedrock{}, nil)
					}
				}
				if c.Mode == "steady_on" {
					tx.SetBlock(p.Side(cube.FaceDown), block.RedstoneBlock{}, nil)
				}
				for z := -4; z <= 4; z++ {
					tx.SetBlock(p.Add(cube.Pos{0, 0, z}), block.ActivatorRail{}, nil)
				}
				if c.Entity == "minecraft:hopper_minecart" {
					tx.SetBlock(chestPos, block.NewChest(), nil)
					chest := tx.Block(chestPos).(block.Container)
					if err := chest.Inventory(tx, chestPos).SetItem(0, item.NewStack(item.Diamond{}, 64)); err != nil {
						t.Fatal(err)
					}
				}
			})
			var handle *world.EntityHandle
			var previous mgl64.Vec3
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native activator clock mismatch")
					}
					if handle != nil {
						e, alive := handle.Entity(tx)
						if alive != (frame.Body != nil) {
							t.Fatalf("tick %d cart alive=%t, native=%t", frame.Tick, alive, frame.Body != nil)
						}
						if alive {
							b := minecartBaseBehaviour(e.(interface{ Behaviour() Behaviour }).Behaviour())
							riders := 0
							if b.passenger != nil {
								riders = 1
							}
							if riders != frame.Body.Riders {
								t.Errorf("tick %d riders=%d, native=%d", frame.Tick, riders, frame.Body.Riders)
							}
							// The rider scenes compare the rail event, not autonomous
							// cow travel. All rider-free bodies retain full comparison.
							if !c.Rider {
								compareMinecartMotionNativeBody(t, frame.Tick, e, previous, frame.Body.minecartMotionNativeBody)
							}
							previous = e.Position()
							count := 0
							if cart, ok := e.(interface{ Inventory() *inventory.Inventory }); ok {
								for _, s := range cart.Inventory().Slots() {
									count += s.Count()
								}
							}
							if count != frame.Body.Items {
								t.Errorf("tick %d cart items=%d, native=%d", frame.Tick, count, frame.Body.Items)
							}
						}
					}
					if frame.SourceItems != nil {
						count := 0
						for _, s := range tx.Block(chestPos).(block.Container).Inventory(tx, chestPos).Slots() {
							count += s.Count()
						}
						if count != *frame.SourceItems {
							t.Errorf("tick %d chest items=%d, native=%d", frame.Tick, count, *frame.SourceItems)
						}
					}
					if frame.Body != nil || handle == nil {
						comparePistonEntityNativeBlock(t, tx, frame.Tick, p, frame.Rail)
					}
					if frame.Tick == trace.Spawn.Tick {
						opts := world.EntitySpawnOpts{Position: trace.Spawn.Body.Position.vec()}
						switch c.Entity {
						case "minecraft:minecart":
							handle = opts.New(MinecartType, minecartConf)
						case "minecraft:tnt_minecart":
							handle = opts.New(TNTMinecartType, tntMinecartConf)
						case "minecraft:hopper_minecart":
							handle = opts.New(HopperMinecartType, hopperMinecartConf)
						default:
							t.Fatal("unknown native activator cart")
						}
						e := tx.AddEntity(handle)
						previous = e.Position()
						if c.Rider {
							rider := tx.AddEntity(world.EntitySpawnOpts{Position: p.Vec3Centre()}.New(seatTestRiderType{}, seatTestRiderConfig{}))
							if !e.(*Minecart).Behaviour().(*MinecartBehaviour).Mount(e.(*Minecart).Ent, tx, rider) {
								t.Fatal("test rider rejected")
							}
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							switch change.Kind {
							case "on":
								tx.SetBlock(p.Side(cube.FaceDown), block.RedstoneBlock{}, nil)
							case "off":
								tx.SetBlock(p.Side(cube.FaceDown), block.Bedrock{}, nil)
							case "setup":
							default:
								t.Fatal("unknown native activator edit")
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
