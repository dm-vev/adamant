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

type dispenserCartSpawnRecorder struct {
	world.NopHandler
	origins map[*world.EntityHandle]mgl64.Vec3
	ticks   map[*world.EntityHandle]int64
}

func (h *dispenserCartSpawnRecorder) HandleEntitySpawn(tx *world.Tx, e world.Entity) {
	h.origins[e.H()] = e.Position()
	h.ticks[e.H()] = tx.CurrentTick()
}

func TestDispenserCartMotionMatchesBDS(t *testing.T) {
	replayDispenserCartBDS(t, "bedrock_1_26_51_dispenser_cart_origins.json", 40)
}
func TestDispenserCartFallbackMatchesBDS(t *testing.T) {
	replayDispenserCartBDS(t, "bedrock_1_26_51_dispenser_cart_fallback.json", 60)
}

func TestDispenserCartChunkMotionMatchesBDS(t *testing.T) {
	replayDispenserCartBDS(t, "bedrock_1_26_51_dispenser_cart_chunks.json", 40)
}

func TestDispenserCartSignedChunkMotionMatchesBDS(t *testing.T) {
	replayDispenserCartBDS(t, "bedrock_1_26_51_dispenser_cart_signed_chunks.json", 48)
}

func replayDispenserCartBDS(t *testing.T, filename string, count int) {
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Item, Track string
				Face        int
				Offset      int
				Anchor      int
				Slope       bool
			}
			Base, Source  cube.Pos
			FloorY        int        `json:"floor_y"`
			RailPositions []cube.Pos `json:"rail_positions"`
			SetupTick     int64      `json:"setup_tick"`
			SetupControl  []struct {
				Position cube.Pos
				Block    pistonEntityNativeBlock
			} `json:"setup_control"`
			Changes []struct {
				Tick int64
				Kind string
			}
			EntitySpawns []struct {
				Tick int64
				Body minecartMotionNativeBody
			} `json:"entity_spawns"`
			Samples []struct {
				Tick          int64
				Items         int
				Dispenser     pistonEntityNativeBlock
				Bodies        []minecartMotionNativeBody
				FallbackItems []struct {
					ID    string
					Count int
				} `json:"fallback_items"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native dispenser/cart capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/face%d/slope%t/offset%d/anchor%d/%s", c.Item, c.Face, c.Slope, c.Offset, c.Anchor, c.Track), func(t *testing.T) {
			if len(trace.Samples) != 140 || len(trace.EntitySpawns) != 1 || len(trace.SetupControl) != len(trace.RailPositions) {
				t.Fatal("invalid native dispenser outcome controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			recorder := &dispenserCartSpawnRecorder{origins: make(map[*world.EntityHandle]mgl64.Vec3), ticks: make(map[*world.EntityHandle]int64)}
			w.Handle(recorder)
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			if err := w.SetGameRule("spawn_mobs", false); err != nil {
				t.Fatal(err)
			}
			p := trace.Base
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, p.Vec3Centre())
				loader.Load(tx, 25)
				for x := -12; x <= 12; x++ {
					for z := -12; z <= 12; z++ {
						tx.SetBlock(cube.Pos{p[0] + x, trace.FloorY, p[2] + z}, block.Bedrock{}, nil)
					}
				}
				for _, rail := range trace.RailPositions {
					if rail[1] == 66 {
						tx.SetBlock(rail.Side(cube.FaceDown), block.Stone{}, nil)
					}
				}
				d := block.NewDispenser()
				d.Facing = cube.Face(c.Face)
				tx.SetBlock(p, d, nil)
				if c.Track == "blocked" {
					tx.SetBlock(p.Side(cube.Face(c.Face)), block.Stone{}, nil)
				}
				for n := len(trace.SetupControl) - 1; n >= 0; n-- {
					r := trace.SetupControl[n]
					tx.SetBlock(r.Position, block.Rail{Direction: block.RailDirection(int(r.Block.States["rail_direction"].(float64)))}, nil)
				}
			})
			previous := make(map[*world.EntityHandle]mgl64.Vec3)
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native dispenser/cart clock mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, p, frame.Dispenser)
					d := tx.Block(p).(block.Dispenser)
					if got := inventoryCount(d.Inventory(tx, p).Slots()); got != frame.Items {
						t.Errorf("tick %d dispenser count=%d, native=%d", frame.Tick, got, frame.Items)
					}
					var carts []world.Entity
					var dropped []item.Stack
					for e := range tx.Entities() {
						if e.H().Type().EncodeEntity() == c.Item {
							carts = append(carts, e)
						}
						if e.H().Type() == ItemType {
							dropped = append(dropped, e.(*Ent).Behaviour().(*ItemBehaviour).Item())
						}
					}
					if len(carts) != len(frame.Bodies) {
						t.Fatalf("tick %d cart count=%d, native=%d", frame.Tick, len(carts), len(frame.Bodies))
					}
					for n, e := range carts {
						before, seen := previous[e.H()]
						if !seen {
							before = recorder.origins[e.H()]
							if recorder.ticks[e.H()] != trace.EntitySpawns[0].Tick {
								t.Errorf("cart creation tick=%d, native=%d", recorder.ticks[e.H()], trace.EntitySpawns[0].Tick)
							}
						}
						compareMinecartMotionNativeBody(t, frame.Tick, e, before, frame.Bodies[n])
						previous[e.H()] = e.Position()
					}
					if len(dropped) != len(frame.FallbackItems) {
						t.Errorf("tick %d dropped stacks=%d, native=%d", frame.Tick, len(dropped), len(frame.FallbackItems))
					} else {
						for n, s := range dropped {
							id, _ := s.Item().EncodeItem()
							if id != frame.FallbackItems[n].ID || s.Count() != frame.FallbackItems[n].Count {
								t.Error("native fallback item differs")
							}
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							switch change.Kind {
							case "setup":
							case "load":
								for _, r := range trace.SetupControl {
									comparePistonEntityNativeBlock(t, tx, frame.Tick, r.Position, r.Block)
								}
								it, ok := world.ItemByName(c.Item, 0)
								if !ok {
									t.Fatal("unknown native cart item")
								}
								if err := d.Inventory(tx, p).SetItem(0, item.NewStack(it, 1)); err != nil {
									t.Fatal(err)
								}
							case "power":
								tx.SetBlock(trace.Source, block.RedstoneBlock{}, nil)
							default:
								t.Fatal("unknown native dispenser change")
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
