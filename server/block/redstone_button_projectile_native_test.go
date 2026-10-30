package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This replay supplies the actual native projectile positions/AABBs. It
// isolates button contact, holding, release and wire timing from the separate
// kernel projectile flight integrator. The entity integration test exercises
// the real projectile collision path as well.
func TestButtonProjectileSignalsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_button_projectiles.json")
	if err != nil {
		t.Fatal(err)
	}
	type vector struct{ X, Y, Z float64 }
	vec := func(v vector) mgl64.Vec3 { return mgl64.Vec3{v.X, v.Y, v.Z} }
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Projectile string
				Facing, Phase     int
			}
			X, Z    int
			Samples []struct {
				Tick   int64
				Source struct {
					ID     string
					States struct {
						Pressed bool `json:"button_pressed_bit"`
					}
				}
				Wire       torchNetworkBlock
				Projectile *struct {
					ID       string
					Position vector
					AABB     *struct{ Center, Extent vector }
				}
			}
			Changes []struct {
				Tick int64
				Kind string
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 180 {
		t.Fatal("incomplete native projectile button capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/facing%d/phase%d", c.Block, c.Facing, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 || trace.Changes[0].Kind != "spawn" || trace.Changes[1].Kind != "remove" || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native projectile button trace")
			}
			var kind ButtonType
			found := false
			for _, k := range ButtonTypes() {
				if k.blockName() == c.Block {
					kind = k
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("unknown native button %s", c.Block)
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			pos, wirePos := cube.Pos{0, 65, 0}, cube.Pos{1, 65, 0}
			if c.Facing == 4 {
				wirePos[0] = -1
			}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(pos.Side(cube.Face(c.Facing).Opposite()), Stone{}, nil)
				tx.SetBlock(wirePos.Side(cube.FaceDown), Stone{}, nil)
				tx.SetBlock(pos, Button{Type: kind, Facing: cube.Face(c.Facing)}, nil)
				tx.SetBlock(wirePos, RedstoneWire{}, nil)
			})
			var current world.Entity
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					b, ok := tx.Block(pos).(Button)
					if !ok {
						t.Errorf("tick %d button disappeared", sample.Tick)
						return
					}
					if b.Pressed != sample.Source.States.Pressed {
						t.Errorf("tick %d pressed=%v native=%v", sample.Tick, b.Pressed, sample.Source.States.Pressed)
					}
					compareTorchNetworkBlock(t, tx, sample.Tick, wirePos, sample.Wire)
					if t.Failed() {
						return
					}
					if current != nil {
						h := tx.RemoveEntity(current)
						_ = h.Close()
						current = nil
					}
					if p := sample.Projectile; p != nil && sample.Tick != trace.Changes[1].Tick {
						if p.ID != c.Projectile || p.AABB == nil {
							t.Error("missing native projectile AABB")
							return
						}
						location := vec(p.Position).Sub(mgl64.Vec3{float64(trace.X), 0, float64(trace.Z)})
						center := vec(p.AABB.Center).Sub(vec(p.Position))
						extent := vec(p.AABB.Extent)
						box := cube.Box(center[0]-extent[0], center[1]-extent[1], center[2]-extent[2], center[0]+extent[0], center[1]+extent[1], center[2]+extent[2])
						h := (world.EntitySpawnOpts{Position: location}).New(buttonNativeProjectileType{id: p.ID, box: box}, buttonNativeProjectileConfig{})
						current = tx.AddEntity(h)
						// Contact runs in the next world entity phase, between
						// interval snapshots, before that phase's scheduled ticks.
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

type buttonNativeProjectileType struct {
	id  string
	box cube.BBox
}

type buttonNativeProjectileConfig struct{}

func (buttonNativeProjectileConfig) Apply(*world.EntityData) {}

func (t buttonNativeProjectileType) EncodeEntity() string                      { return t.id }
func (t buttonNativeProjectileType) BBox(world.Entity) cube.BBox               { return t.box }
func (buttonNativeProjectileType) DecodeNBT(map[string]any, *world.EntityData) {}
func (buttonNativeProjectileType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
func (buttonNativeProjectileType) Open(_ *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	return buttonNativeProjectile{h: h, d: d}
}

type buttonNativeProjectile struct {
	h *world.EntityHandle
	d *world.EntityData
}

func (buttonNativeProjectile) Close() error              { return nil }
func (e buttonNativeProjectile) H() *world.EntityHandle  { return e.h }
func (e buttonNativeProjectile) Position() mgl64.Vec3    { return e.d.Pos }
func (e buttonNativeProjectile) Rotation() cube.Rotation { return e.d.Rot }

func (e buttonNativeProjectile) Tick(tx *world.Tx, _ int64) {
	pos := cube.Pos{0, 65, 0}
	if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(cube.Box(0, 65, 0, 1, 66, 1)) {
		if inside, ok := tx.Block(pos).(EntityInsider); ok {
			inside.EntityInside(pos, tx, e)
		}
	}
}
