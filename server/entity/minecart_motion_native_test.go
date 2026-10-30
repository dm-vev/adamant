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

type minecartMotionNativeBody struct {
	pistonEntityNativeBody
	OnGround bool `json:"on_ground"`
}

func TestMinecartFlatMotionMatchesBDS(t *testing.T) {
	replayMinecartFlatMotionBDS(t, "bedrock_1_26_51_minecart_motion_flat.json", 96)
}

func TestMinecartInventoryMotionMatchesBDS(t *testing.T) {
	replayMinecartFlatMotionBDS(t, "bedrock_1_26_51_minecart_motion_inventory.json", 160)
}

func TestMinecartSlopeMotionMatchesBDS(t *testing.T) {
	replayMinecartFlatMotionBDS(t, "bedrock_1_26_51_minecart_motion_slopes.json", 64)
}

func TestMinecartOverlapMatchesBDS(t *testing.T) {
	replayMinecartFlatMotionBDS(t, "bedrock_1_26_51_cart_overlap.json", 72)
}

func replayMinecartFlatMotionBDS(t *testing.T, filename string, count int) {
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Entity, Mode, Axis, Load string
				Speed                    float64
				Phase                    int
				Slope                    int
				Obstacle                 string
				Shift                    int
			}
			Base          cube.Pos
			SetupTick     int64 `json:"setup_tick"`
			Obstacle      pistonEntityNativeBlock
			RailPositions []cube.Pos `json:"rail_positions"`
			Environment   []struct {
				Position cube.Pos
				ID       string
				States   map[string]any
			}
			SetupControl struct {
				Rails int `json:"checked_rails"`
				Floor int `json:"checked_floor"`
			} `json:"setup_control"`
			Spawn struct {
				Tick          int64
				Body          minecartMotionNativeBody
				InventorySize *int `json:"inventory_size"`
				Inventory     []struct {
					Slot, Count int
					ID          string
				}
			}
			Samples []struct {
				Tick int64
				Body *minecartMotionNativeBody
				Rail *struct {
					Position cube.Pos
					Block    pistonEntityNativeBlock
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native cart motion capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/%s/%s/slope%d/%s/shift%d/speed%g", c.Entity, c.Load, c.Mode, c.Axis, c.Slope, c.Obstacle, c.Shift, c.Speed), func(t *testing.T) {
			if len(trace.Samples) != 120 || trace.Spawn.Tick != trace.SetupTick+40 || trace.SetupTick&1 != int64(c.Phase) {
				t.Fatal("invalid isolated cart motion scene")
			}
			if c.Obstacle != "" {
				if trace.Obstacle.ID != "minecraft:"+c.Obstacle || len(trace.RailPositions) != 0 {
					t.Fatal("invalid native overlap obstacle control")
				}
			} else if len(trace.Environment) == 0 {
				if len(trace.RailPositions) != 97 || trace.SetupControl.Rails != 97 || trace.SetupControl.Floor != 485 {
					t.Fatal("invalid native flat track control")
				}
			} else if len(trace.RailPositions) != 81 || len(trace.Environment) < 405 || c.Slope != 1 && c.Slope != -1 {
				t.Fatal("invalid native slope track control")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(4, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			if err := w.SetGameRule("spawn_mobs", false); err != nil {
				t.Fatal(err)
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 81)
				if c.Obstacle != "" {
					for x := -6; x <= 6; x++ {
						for z := -6; z <= 6; z++ {
							tx.SetBlock(trace.Base.Add(cube.Pos{x, -1, z}), block.Bedrock{}, nil)
						}
					}
					var obstacle world.Block
					switch c.Obstacle {
					case "stone":
						obstacle = block.Stone{}
					case "glass":
						obstacle = block.Glass{}
					case "dispenser":
						d := block.NewDispenser()
						d.Facing = cube.FaceNorth
						obstacle = d
					default:
						t.Fatal("unknown native overlap obstacle")
					}
					tx.SetBlock(trace.Base, obstacle, nil)
					return
				}
				if len(trace.Environment) != 0 {
					for _, saved := range trace.Environment {
						states := make(map[string]any, len(saved.States))
						for k, v := range saved.States {
							if saved.ID == "minecraft:stone" && k == "stone_type" && v == "stone" {
								continue // Script API's legacy alias beside the modern stone ID.
							}
							if number, ok := v.(float64); ok {
								v = int32(number)
							}
							states[k] = v
						}
						b, ok := world.BlockByName(saved.ID, states)
						if !ok {
							t.Fatalf("unknown native slope block %s %v", saved.ID, states)
						}
						tx.SetBlock(saved.Position, b, nil)
					}
					return
				}
				for _, p := range trace.RailPositions {
					for n := -2; n <= 2; n++ {
						floor := p.Side(cube.FaceDown)
						if c.Axis == "x" {
							floor[2] += n
						} else {
							floor[0] += n
						}
						var b world.Block = block.Stone{}
						if c.Mode == "golden_on" && n == 0 {
							b = block.RedstoneBlock{}
						}
						tx.SetBlock(floor, b, nil)
					}
				}
				for _, p := range trace.RailPositions {
					dir := block.RailNorthSouth
					if c.Axis == "x" {
						dir = block.RailEastWest
					}
					switch c.Mode {
					case "rail":
						tx.SetBlock(p, block.Rail{Direction: dir}, nil)
					case "golden_off", "golden_on":
						tx.SetBlock(p, block.PoweredRail{Direction: dir}, nil)
					case "detector":
						tx.SetBlock(p, block.DetectorRail{Direction: dir}, nil)
					default:
						t.Fatal("unhandled native track mode")
					}
				}
			})
			var handle *world.EntityHandle
			var previous mgl64.Vec3
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("cart replay clock mismatch")
					}
					if frame.Body != nil {
						if handle == nil || len(trace.Environment) == 0 && frame.Rail == nil {
							t.Fatal("native body precedes spawn or lacks rail readback")
						}
						e, ok := handle.Entity(tx)
						if !ok {
							t.Fatal("cart disappeared")
						}
						compareMinecartMotionNativeBody(t, frame.Tick, e, previous, *frame.Body)
						previous = e.Position()
						if frame.Rail != nil {
							comparePistonEntityNativeBlock(t, tx, frame.Tick, frame.Rail.Position, frame.Rail.Block)
						}
					} else if handle != nil {
						t.Fatal("native body unexpectedly absent")
					}
					if frame.Tick == trace.Spawn.Tick {
						if c.Obstacle != "" {
							comparePistonEntityNativeBlock(t, tx, frame.Tick, trace.Base, trace.Obstacle)
						}
						for _, saved := range trace.Environment {
							comparePistonEntityNativeBlock(t, tx, frame.Tick, saved.Position, pistonEntityNativeBlock{ID: saved.ID, States: saved.States})
						}
						// Script spawn coordinates address the cart's center. Begin
						// with its observed public feet position and compare that
						// snapshot and the authored impulse before the first tick.
						opts := world.EntitySpawnOpts{Position: trace.Spawn.Body.Position.vec()}
						switch c.Entity {
						case "minecraft:minecart":
							handle = opts.New(MinecartType, minecartConf)
						case "minecraft:chest_minecart":
							handle = opts.New(ChestMinecartType, chestMinecartConf)
						case "minecraft:hopper_minecart":
							handle = opts.New(HopperMinecartType, hopperMinecartConf)
						case "minecraft:tnt_minecart":
							handle = opts.New(TNTMinecartType, tntMinecartConf)
						case "minecraft:command_block_minecart":
							handle = NewCommandBlockMinecart(opts, CommandBlockMinecartConfig{})
						default:
							t.Fatal("unhandled native cart")
						}
						e := tx.AddEntity(handle)
						if container, ok := e.(interface{ Inventory() *inventory.Inventory }); ok {
							inv := container.Inventory()
							if trace.Spawn.InventorySize != nil && inv.Size() != *trace.Spawn.InventorySize {
								t.Fatal("native cart container size mismatch")
							}
							for _, stack := range trace.Spawn.Inventory {
								it, ok := world.ItemByName(stack.ID, 0)
								if !ok {
									t.Fatal("unknown native cart item", stack.ID)
								}
								if err := inv.SetItem(stack.Slot, item.NewStack(it, stack.Count)); err != nil {
									t.Fatal(err)
								}
							}
						} else if trace.Spawn.InventorySize != nil && *trace.Spawn.InventorySize != 0 || len(trace.Spawn.Inventory) != 0 {
							t.Fatal("native container missing")
						}
						previous = e.Position()
						impulse := mgl64.Vec3{}
						if c.Axis == "x" || c.Obstacle != "" {
							impulse[0] = float64(float32(c.Speed))
						} else {
							impulse[2] = float64(float32(c.Speed))
						}
						e.(interface{ SetVelocity(mgl64.Vec3) }).SetVelocity(impulse)
						compareMinecartMotionNativeBody(t, frame.Tick, e, previous, trace.Spawn.Body)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

func compareMinecartMotionNativeBody(t *testing.T, tick int64, e world.Entity, previous mgl64.Vec3, want minecartMotionNativeBody) {
	t.Helper()
	box := e.H().Type().BBox(e).Translate(e.Position())
	velocity := e.(interface{ Velocity() mgl64.Vec3 }).Velocity()
	// The original ScriptActor getter selects last travel for plain carts,
	// while these container carts expose StateVector velocity (0xbd8ac30).
	if e.H().Type().EncodeEntity() == "minecraft:minecart" {
		velocity = e.Position().Sub(previous)
	}
	for name, pair := range map[string][2]mgl64.Vec3{
		"position": {e.Position(), want.Position.vec()}, "velocity": {velocity, want.Velocity.vec()},
		"aabb center": {box.Min().Add(box.Max()).Mul(.5), want.AABB.Center.vec()},
		"aabb extent": {box.Max().Sub(box.Min()).Mul(.5), want.AABB.Extent.vec()},
	} {
		if pair[0].Sub(pair[1]).Len() > 0.00002 {
			t.Errorf("tick %d %s=%v, BDS=%v", tick, name, pair[0], pair[1])
		}
	}
	if onGround := e.(interface{ OnGround() bool }).OnGround(); onGround != want.OnGround {
		t.Errorf("tick %d ground=%t, BDS=%t", tick, onGround, want.OnGround)
	}
}
