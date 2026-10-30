package block

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type pistonNativeVec struct{ X, Y, Z float64 }

func (v pistonNativeVec) vec() mgl64.Vec3 { return mgl64.Vec3{v.X, v.Y, v.Z} }

type pistonNativeBodySnapshot struct {
	ID                 string
	Position, Velocity pistonNativeVec
	AABB               struct{ Center, Extent pistonNativeVec }
}

// The passive BDS definitions have no gravity or AI. Collision-enabled bodies
// spawned inside an already extended block also exercise native escape physics;
// those 144 cases remain separate from this direct-displacement replay.
func TestPistonPassiveDisplacementMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_piston_entity_displacement.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sticky, Collision bool
				Facing, Phase     int
				Payload, Motion   string
				Distance          float64
			}
			Base, Source cube.Pos
			Front        []cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			Changes      []struct {
				Tick    int64
				Powered bool
			}
			Spawn struct {
				Tick     int64
				Position pistonNativeVec
				Body     pistonNativeBodySnapshot
			}
			Samples []struct {
				Tick         int64
				Base, Source nativeTripwireBlock
				Front        []nativeTripwireBlock
				Actor        *nativePistonActor
				Body         *pistonNativeBodySnapshot
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 576 {
		t.Fatal("incomplete native piston displacement fixture")
	}
	compared, escape := 0, 0
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		if c.Collision && c.Motion == "pull" {
			escape++
			continue
		}
		compared++
		t.Run(fmt.Sprintf("sticky%t/face%d/phase%d/%s/distance%g/collision%t/%s", c.Sticky, c.Facing, c.Phase, c.Payload, c.Distance, c.Collision, c.Motion), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 || len(trace.Front) != 2 || trace.Changes[0].Tick&1 != int64(c.Phase) || trace.Spawn.Tick == 0 {
				t.Fatal("invalid native piston displacement case")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			if err := w.SetGameRule("block_drops", false); err != nil {
				t.Fatal(err)
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(trace.Base, Piston{Facing: cube.Face(c.Facing), Sticky: c.Sticky}, nil)
				switch c.Payload {
				case "stone":
					tx.SetBlock(trace.Front[0], Stone{}, nil)
				case "honey_block":
					tx.SetBlock(trace.Front[0], HoneyBlock{}, nil)
				case "air":
				default:
					t.Fatal("unmapped native displacement payload")
				}
			})
			var handle *world.EntityHandle
			for _, s := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != s.Tick || len(s.Front) != 2 {
						t.Fatal("piston displacement replay clock/cells differ")
					}
					comparePistonNativeActor(t, tx, s.Tick, trace.Base, s.Actor)
					compareSculkContactNativeBlock(t, tx, s.Tick, trace.Base, s.Base)
					compareSculkContactNativeBlock(t, tx, s.Tick, trace.Source, s.Source)
					for i, pos := range trace.Front {
						compareSculkContactNativeBlock(t, tx, s.Tick, pos, s.Front[i])
					}
					if s.Body != nil {
						if handle == nil {
							t.Fatal("native body precedes its spawn")
						}
						e, ok := handle.Entity(tx)
						if !ok {
							t.Fatal("missing piston replay body")
						}
						comparePistonNativeBody(t, s.Tick, e.(*pistonNativeBody), *s.Body)
					} else if handle != nil {
						t.Fatal("native body unexpectedly disappeared")
					}
					for _, change := range trace.Changes {
						if s.Tick == change.Tick {
							var b world.Block = Air{}
							if change.Powered {
								b = RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, b, nil)
						}
					}
					if s.Tick == trace.Spawn.Tick {
						if handle != nil {
							t.Fatal("duplicate body spawn")
						}
						handle = (world.EntitySpawnOpts{Position: trace.Spawn.Position.vec()}).New(pistonNativeBodyType{trace.Spawn.Body.ID}, buttonNativeProjectileConfig{})
						e := tx.AddEntity(handle).(*pistonNativeBody)
						comparePistonNativeBody(t, s.Tick, e, trace.Spawn.Body)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
	if compared != 432 || escape != 144 {
		t.Fatalf("compared %d direct cases, retained %d escape cases", compared, escape)
	}
}

func comparePistonNativeBody(t *testing.T, tick int64, e *pistonNativeBody, want pistonNativeBodySnapshot) {
	t.Helper()
	box := e.H().Type().BBox(e).Translate(e.Position())
	for name, pair := range map[string][2]mgl64.Vec3{
		"position": {e.Position(), want.Position.vec()}, "velocity": {e.Velocity(), want.Velocity.vec()},
		"aabb centre": {box.Min().Add(box.Max()).Mul(.5), want.AABB.Center.vec()},
		"aabb extent": {box.Max().Sub(box.Min()).Mul(.5), want.AABB.Extent.vec()},
	} {
		if pair[0].Sub(pair[1]).Len() > 0.00001 {
			t.Errorf("tick %d %s=%v, native=%v", tick, name, pair[0], pair[1])
		}
	}
}

type pistonNativeBodyType struct{ id string }

func (t pistonNativeBodyType) EncodeEntity() string { return t.id }
func (pistonNativeBodyType) BBox(world.Entity) cube.BBox {
	return cube.Box(-.25, 0, -.25, .25, .5, .25)
}
func (pistonNativeBodyType) DecodeNBT(map[string]any, *world.EntityData) {}
func (pistonNativeBodyType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
func (pistonNativeBodyType) Open(_ *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return &pistonNativeBody{h: h, d: d}
}

type pistonNativeBody struct {
	h *world.EntityHandle
	d *world.EntityData
}

func (*pistonNativeBody) Close() error               { return nil }
func (e *pistonNativeBody) H() *world.EntityHandle   { return e.h }
func (e *pistonNativeBody) Position() mgl64.Vec3     { return e.d.Pos }
func (e *pistonNativeBody) Rotation() cube.Rotation  { return e.d.Rot }
func (e *pistonNativeBody) Velocity() mgl64.Vec3     { return e.d.Vel }
func (e *pistonNativeBody) SetVelocity(v mgl64.Vec3) { e.d.Vel = v }
func (e *pistonNativeBody) Move(v mgl64.Vec3, _, _ float64) {
	for axis := range 3 {
		e.d.Pos[axis] = float64(float32(e.d.Pos[axis]) + float32(v[axis]))
		if math.IsNaN(e.d.Pos[axis]) {
			panic("non-finite piston body motion")
		}
	}
}
