package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type pistonEntityNativeVec struct{ X, Y, Z float64 }

func (v pistonEntityNativeVec) vec() mgl64.Vec3 { return mgl64.Vec3{v.X, v.Y, v.Z} }

type pistonEntityNativeBlock struct {
	ID     string
	States map[string]any
}

type pistonEntityNativeBody struct {
	ID                 string
	Position, Velocity pistonEntityNativeVec
	AABB               struct{ Center, Extent pistonEntityNativeVec }
}

func TestPistonVanillaEntitiesMatchBDS(t *testing.T) {
	testPistonVanillaEntitiesNative(t, "bedrock_1_26_51_piston_vanilla_entities.json", 512)
}

func TestPistonVanillaObstructionMatchesBDS(t *testing.T) {
	testPistonVanillaEntitiesNative(t, "bedrock_1_26_51_piston_vanilla_obstruction.json", 256)
}

func testPistonVanillaEntitiesNative(t *testing.T, name string, count int) {
	data, err := os.ReadFile("../block/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sticky, Obstruction bool
				Facing, Phase       int
				Payload, Motion     string
				Entity              string
				Distance            float64
			}
			Base, Source cube.Pos
			Front        []cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			SetupControl struct {
				CheckedCells   int `json:"checked_cells"`
				NonAir         int `json:"non_air"`
				ExpectedNonAir int `json:"expected_non_air"`
				ClearRadius    int `json:"clear_radius"`
			} `json:"setup_control"`
			Changes []struct {
				Tick    int64
				Powered bool
			}
			Spawn struct {
				Tick     int64
				Position pistonEntityNativeVec
				Body     pistonEntityNativeBody
			}
			Samples []struct {
				Tick         int64
				Base, Source pistonEntityNativeBlock
				Front        []pistonEntityNativeBlock
				Actor        *struct {
					State    string
					Moving   bool
					Attached []struct{ X, Y, Z int }
				}
				Body *pistonEntityNativeBody
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete original-BDS vanilla entity fixture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("sticky%t/face%d/phase%d/%s/distance%g/%s/%s", c.Sticky, c.Facing, c.Phase, c.Payload, c.Distance, c.Motion, c.Entity), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 || trace.Changes[0].Tick&1 != int64(c.Phase) || trace.Spawn.Tick == 0 {
				t.Fatal("invalid original vanilla entity trace")
			}
			if trace.SetupControl.CheckedCells != 338 || trace.SetupControl.ClearRadius != 6 || trace.SetupControl.NonAir != trace.SetupControl.ExpectedNonAir {
				t.Fatal("missing native initial-environment validation")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.Overworld, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			t.Cleanup(func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() })
			if err := w.SetGameRule("block_drops", false); err != nil {
				t.Fatal(err)
			}
			if err := w.SetGameRule("spawn_mobs", false); err != nil {
				t.Fatal(err)
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := trace.Base.X() - 4; x <= trace.Base.X()+4; x++ {
					for z := trace.Base.Z() - 4; z <= trace.Base.Z()+4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(trace.Base, block.Piston{Facing: cube.Face(c.Facing), Sticky: c.Sticky}, nil)
				var payload world.Block
				switch c.Payload {
				case "air":
					payload = block.Air{}
				case "stone":
					payload = block.Stone{}
				case "honey_block":
					payload = block.HoneyBlock{}
				case "slime":
					payload = block.Slime{}
				default:
					t.Fatal("unmapped vanilla piston payload")
				}
				tx.SetBlock(trace.Front[0], payload, nil)
				if c.Obstruction {
					wall := 2
					if c.Payload == "air" {
						wall = 1
					}
					tx.SetBlock(trace.Front[wall], block.Stone{}, nil)
				}
			})
			var handle *world.EntityHandle
			for _, s := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != s.Tick || len(s.Front) != len(trace.Front) {
						t.Fatal("vanilla entity replay clock/cell mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, s.Tick, trace.Base, s.Base)
					comparePistonEntityNativeBlock(t, tx, s.Tick, trace.Source, s.Source)
					for i, pos := range trace.Front {
						comparePistonEntityNativeBlock(t, tx, s.Tick, pos, s.Front[i])
					}
					p, ok := tx.Block(trace.Base).(block.Piston)
					if !ok || s.Actor == nil || s.Actor.Attached == nil {
						t.Fatal("missing native piston actor or attached positions")
					}
					states := [...]string{"Retracted", "Expanding", "Expanded", "Retracting"}
					if int(p.State) >= len(states) || states[p.State] != s.Actor.State || p.Moving != s.Actor.Moving {
						t.Errorf("tick %d piston=%d/%t native=%s/%t", s.Tick, p.State, p.Moving, s.Actor.State, s.Actor.Moving)
					}
					var attached []cube.Pos
					for _, pos := range s.Actor.Attached {
						attached = append(attached, cube.Pos{pos.X, pos.Y, pos.Z})
					}
					if !slices.Equal(p.Attached, attached) {
						t.Errorf("tick %d attached=%v, native=%v", s.Tick, p.Attached, attached)
					}
					if s.Body != nil {
						if handle == nil {
							t.Fatal("native body precedes spawn")
						}
						e, ok := handle.Entity(tx)
						if !ok {
							t.Fatal("vanilla replay entity disappeared")
						}
						comparePistonVanillaBody(t, s.Tick, e.(*Ent), *s.Body)
					} else if handle != nil {
						t.Fatal("native entity unexpectedly absent")
					}
					for _, change := range trace.Changes {
						if s.Tick == change.Tick {
							var b world.Block = block.Air{}
							if change.Powered {
								b = block.RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, b, nil)
						}
					}
					if s.Tick == trace.Spawn.Tick {
						opts := world.EntitySpawnOpts{Position: trace.Spawn.Position.vec()}
						switch c.Entity {
						case "minecraft:armor_stand":
							handle = NewArmourStand(opts)
						case "minecraft:item":
							handle = NewItem(opts, item.NewStack(item.Diamond{}, 1))
						default:
							t.Fatal("unmapped native piston entity")
						}
						comparePistonVanillaBody(t, s.Tick, tx.AddEntity(handle).(*Ent), trace.Spawn.Body)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

func comparePistonEntityNativeBlock(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, want pistonEntityNativeBlock) {
	t.Helper()
	id, properties := tx.Block(pos).EncodeBlock()
	if id != want.ID {
		t.Errorf("tick %d at %v block=%s, native=%s", tick, pos, id, want.ID)
		return
	}
	for key, expected := range want.States {
		var got any = properties[key]
		// Script API retains this legacy alias beside the modern stone ID.
		if key == "stone_type" && got == nil && id == "minecraft:stone" {
			if stone, ok := tx.Block(pos).(block.Stone); ok && !stone.Smooth {
				got = "stone"
			}
		}
		switch value := got.(type) {
		case int32:
			got = float64(value)
		case uint8:
			got = value != 0
		}
		if got != expected {
			t.Errorf("tick %d at %v %s=%v, native=%v", tick, pos, key, got, expected)
		}
	}
}

func comparePistonVanillaBody(t *testing.T, tick int64, e *Ent, want pistonEntityNativeBody) {
	t.Helper()
	box := e.H().Type().BBox(e).Translate(e.Position())
	velocity := e.Velocity()
	if b, ok := e.Behaviour().(*ArmourStandBehaviour); ok {
		// ScriptActor::getVelocity (0xbd8ac30) returns component 0xf96968b0
		// for mobs: the last travel delta, not StateVector's next velocity.
		velocity = b.passive.mc.nativeLastDelta
	}
	for name, pair := range map[string][2]mgl64.Vec3{
		"position": {e.Position(), want.Position.vec()}, "velocity": {velocity, want.Velocity.vec()},
		"aabb centre": {box.Min().Add(box.Max()).Mul(.5), want.AABB.Center.vec()},
		"aabb extent": {box.Max().Sub(box.Min()).Mul(.5), want.AABB.Extent.vec()},
	} {
		if pair[0].Sub(pair[1]).Len() > 0.00002 {
			t.Errorf("tick %d %s=%v, native=%v", tick, name, pair[0], pair[1])
		}
	}
}
