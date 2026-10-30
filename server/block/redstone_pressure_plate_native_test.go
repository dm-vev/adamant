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

// Recorded native bodies isolate the plate query and scheduled release from
// the kernel's independent movement, AI and item merging implementations.
func TestPressurePlatesMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_pressure_plates.json")
	if err != nil {
		t.Fatal(err)
	}
	type vector struct{ X, Y, Z float64 }
	vec := func(v vector) mgl64.Vec3 { return mgl64.Vec3{v.X, v.Y, v.Z} }
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Entity string
				Phase, Count  int
				XOffset       float64 `json:"x_offset"`
				YOffset       float64 `json:"y_offset"`
			}
			X, Z    int
			Samples []struct {
				Tick         int64
				Source, Wire torchNetworkBlock
				Entities     []struct {
					ID       string
					Position vector
					AABB     struct{ Center, Extent vector }
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
	if !reference.Complete || len(reference.Scenarios) != 302 {
		t.Fatal("incomplete native pressure-plate capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/%s/count%d/x%g/y%g/phase%d", c.Block, c.Entity, c.Count, c.XOffset, c.YOffset, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 80 || len(trace.Changes) != 2 || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native plate trace")
			}
			var kind PressurePlateType
			found := false
			for _, k := range PressurePlateTypes() {
				if k.blockName() == c.Block {
					kind = k
					found = true
					break
				}
			}
			if !found {
				t.Fatal("unknown plate type")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			// Preserve native coordinates: typed actor queries at chunk edges
			// deliberately differ from the wood/weighted broad phase.
			pos := cube.Pos{trace.X, 65, trace.Z}
			wirePos := pos.Side(cube.FaceEast)
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 2; x++ {
					for z := -2; z <= 2; z++ {
						tx.SetBlock(cube.Pos{pos[0] + x, 64, pos[2] + z}, Stone{}, nil)
					}
				}
				tx.SetBlock(pos, PressurePlate{Type: kind}, nil)
				tx.SetBlock(wirePos, RedstoneWire{}, nil)
			})
			var current []world.Entity
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					p, ok := tx.Block(pos).(PressurePlate)
					if !ok {
						t.Error("plate disappeared")
						return
					}
					if p.Signal != sample.Source.States.Signal {
						t.Errorf("tick %d signal=%d native=%d", sample.Tick, p.Signal, sample.Source.States.Signal)
					}
					compareTorchNetworkBlock(t, tx, sample.Tick, wirePos, sample.Wire)
					for _, e := range current {
						_ = tx.RemoveEntity(e).Close()
					}
					current = nil
					if i+1 == len(trace.Samples) {
						return
					}
					// These are the actual bodies observed after the next native
					// entity phase. The stand-ins don't integrate movement.
					for _, body := range trace.Samples[i+1].Entities {
						location := vec(body.Position)
						center := vec(body.AABB.Center).Sub(vec(body.Position))
						extent := vec(body.AABB.Extent)
						box := cube.Box(center[0]-extent[0], center[1]-extent[1], center[2]-extent[2], center[0]+extent[0], center[1]+extent[1], center[2]+extent[2])
						h := (world.EntitySpawnOpts{Position: location}).New(plateNativeBodyType{buttonNativeProjectileType: buttonNativeProjectileType{id: body.ID, box: box}, source: pos}, buttonNativeProjectileConfig{})
						current = append(current, tx.AddEntity(h))
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}

type plateNativeBodyType struct {
	buttonNativeProjectileType
	source cube.Pos
}

func (p plateNativeBodyType) Open(_ *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	e := plateNativeBody{buttonNativeProjectile: buttonNativeProjectile{h: h, d: d}, source: p.source}
	if p.id == "minecraft:pig" {
		return plateNativeMob{e}
	}
	return e
}

type plateNativeBody struct {
	buttonNativeProjectile
	source cube.Pos
}

func (e plateNativeBody) Tick(tx *world.Tx, _ int64) {
	plateNativeContact(tx, e, e.source)
}

type plateNativeMob struct{ plateNativeBody }

func (plateNativeMob) Health() float64 { return 10 }
func (e plateNativeMob) Tick(tx *world.Tx, _ int64) {
	plateNativeContact(tx, e, e.source)
}

func plateNativeContact(tx *world.Tx, e world.Entity, pos cube.Pos) {
	if e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3())) {
		if p, ok := tx.Block(pos).(EntityInsider); ok {
			p.EntityInside(pos, tx, e)
		}
	}
}

func TestPressurePlatePowerMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_pressure_plate_methods.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete bool
		Methods  map[string]struct{ Cases [][2]int }
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Methods) != 5 {
		t.Fatal("incomplete executed-client plate methods")
	}
	for _, kind := range PressurePlateTypes() {
		decode, delay := "decode_binary", "binary_delay"
		if kind.weighted() {
			decode, delay = "decode_weighted", "weighted_delay"
		}
		if cases := reference.Methods[delay].Cases; len(cases) != 18 || kind.pressedTicks() != cases[0][1] {
			t.Errorf("%s recheck delay differs from native", kind.blockName())
		}
		cases := reference.Methods[decode].Cases
		if len(cases) != 18 {
			t.Fatal("incomplete native state-conversion range")
		}
		for _, c := range cases {
			p := PressurePlate{Type: kind, Signal: c[0]}
			if got := p.RedstonePower(cube.Pos{}, nil, cube.FaceUp); got != c[1] {
				t.Errorf("%s state %d power=%d native=%d", kind.blockName(), c[0], got, c[1])
			}
		}
	}
}
