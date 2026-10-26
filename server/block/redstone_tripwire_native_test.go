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

type nativeTripwireBlock struct {
	ID     string
	States map[string]any
}

func TestTripwireCollisionMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_tripwire_geometry.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Collision struct{ Bounds []float64 }
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Collision.Bounds) != 6 {
		t.Fatal("incomplete native collision evidence")
	}
	for _, v := range reference.Collision.Bounds {
		if v != 0 {
			t.Fatal("native collision is not empty")
		}
	}
	for _, b := range allTripwires() {
		for _, box := range b.Model().BBox(cube.Pos{}, nil) {
			if box.Width() > 0 && box.Height() > 0 && box.Length() > 0 {
				t.Fatalf("%+v has physical collision %v; native returns an empty box", b, box)
			}
		}
	}
}

func compareNativeTripwireBlock(t *testing.T, tx *world.Tx, tick int64, pos cube.Pos, expected nativeTripwireBlock) {
	t.Helper()
	id, states := tx.Block(pos).EncodeBlock()
	if id != expected.ID {
		t.Errorf("tick %d at %v id=%s native=%s", tick, pos, id, expected.ID)
		return
	}
	for name, value := range expected.States {
		actual := states[name]
		switch v := value.(type) {
		case bool:
			if b, ok := actual.(uint8); ok {
				actual = b != 0
			}
			if actual != v {
				t.Errorf("tick %d at %v %s=%v native=%v", tick, pos, name, actual, v)
			}
		case float64:
			if b, ok := actual.(int32); ok {
				actual = float64(b)
			}
			if actual != v {
				t.Errorf("tick %d at %v %s=%v native=%v", tick, pos, name, actual, v)
			}
		default:
			t.Fatalf("unsupported native state %s=%v", name, value)
		}
	}
}

func TestTripwireContactsMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_tripwire_contacts.json")
	if err != nil {
		t.Fatal(err)
	}
	type vector struct{ X, Y, Z float64 }
	vec := func(v vector) mgl64.Vec3 { return mgl64.Vec3{v.X, v.Y, v.Z} }
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Mode, Entity string
				Suspended    bool
				Phase        int
				XOffset      float64 `json:"x_offset"`
				YOffset      float64 `json:"y_offset"`
			}
			X, Z          int
			Configuration struct {
				Tick   int64
				Source nativeTripwireBlock
			}
			Changes []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick                      int64
				Source, Left, Right, Wire nativeTripwireBlock
				Entities                  []struct {
					ID       string
					Position vector
					AABB     struct{ Center, Extent vector }
				}
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 140 {
		t.Fatal("incomplete native tripwire contact capture")
	}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/suspended%v/%s/x%g/y%g/phase%d", c.Mode, c.Suspended, c.Entity, c.XOffset, c.YOffset, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 80 || len(trace.Changes) != 2 || trace.Changes[0].Tick&1 != int64(c.Phase) || trace.Configuration.Tick == 0 {
				t.Fatal("invalid native tripwire trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			left := cube.Pos{trace.X, 65, trace.Z}
			source := left.Side(cube.FaceEast)
			right := source.Side(cube.FaceEast)
			wire := left.Side(cube.FaceWest).Side(cube.FaceWest)
			<-w.Exec(func(tx *world.Tx) {
				for x := -3; x <= 4; x++ {
					for z := -2; z <= 2; z++ {
						tx.SetBlock(cube.Pos{left[0] + x, 64, left[2] + z}, Stone{}, nil)
					}
				}
				if c.Mode != "unhooked" {
					tx.SetBlock(left.Side(cube.FaceWest), Stone{}, nil)
					tx.SetBlock(right.Side(cube.FaceEast), Stone{}, nil)
					tx.SetBlock(left, TripwireHook{Facing: cube.East}, nil)
					tx.SetBlock(right, TripwireHook{Facing: cube.West}, nil)
				}
				tx.SetBlock(source, Tripwire{Suspended: c.Suspended, Disarmed: c.Mode == "disarmed"}, nil)
				tx.SetBlock(wire, RedstoneWire{}, nil)
			})
			var current []world.Entity
			for i, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					compareNativeTripwireBlock(t, tx, sample.Tick, source, sample.Source)
					compareNativeTripwireBlock(t, tx, sample.Tick, left, sample.Left)
					compareNativeTripwireBlock(t, tx, sample.Tick, right, sample.Right)
					compareNativeTripwireBlock(t, tx, sample.Tick, wire, sample.Wire)
					if sample.Tick == trace.Configuration.Tick {
						b := tx.Block(source).(Tripwire)
						b.Suspended = c.Suspended
						b.Disarmed = c.Mode == "disarmed"
						tx.SetBlock(source, b, nil)
					}
					for _, e := range current {
						_ = tx.RemoveEntity(e).Close()
					}
					current = nil
					if i+1 == len(trace.Samples) {
						return
					}
					for _, body := range trace.Samples[i+1].Entities {
						center := vec(body.AABB.Center).Sub(vec(body.Position))
						extent := vec(body.AABB.Extent)
						box := cube.Box(center[0]-extent[0], center[1]-extent[1], center[2]-extent[2], center[0]+extent[0], center[1]+extent[1], center[2]+extent[2])
						h := (world.EntitySpawnOpts{Position: vec(body.Position)}).New(plateNativeBodyType{buttonNativeProjectileType: buttonNativeProjectileType{id: body.ID, box: box}, source: source}, buttonNativeProjectileConfig{})
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

func TestTripwireLinesMatchNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_tripwire_lines.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Direction, Length, Phase int
				Disarmed                 bool
			}
			Left, Right, Output, Cut [2]int
			Wires                    [][2]int
			Configuration            struct{ Tick int64 }
			Changes                  []struct {
				Tick int64
				Kind string
			}
			Samples []struct {
				Tick                int64
				Left, Right, Output nativeTripwireBlock
				Wires               []nativeTripwireBlock
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 80 {
		t.Fatal("incomplete native tripwire line capture")
	}
	pos := func(p [2]int) cube.Pos { return cube.Pos{p[0], 65, p[1]} }
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("direction%d/length%d/disarmed%v/phase%d", c.Direction, c.Length, c.Disarmed, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 64 || len(trace.Changes) != 1 || len(trace.Wires) != c.Length || trace.Changes[0].Tick&1 != int64(c.Phase) {
				t.Fatal("invalid native line trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			left, right, output, cut := pos(trace.Left), pos(trace.Right), pos(trace.Output), pos(trace.Cut)
			facing := [...]cube.Direction{cube.North, cube.East, cube.South, cube.West}[c.Direction]
			<-w.Exec(func(tx *world.Tx) {
				points := []cube.Pos{left.Side(facing.Opposite().Face()), right.Side(facing.Face()), left, right, output}
				for _, p := range trace.Wires {
					points = append(points, pos(p))
				}
				for _, p := range points {
					tx.SetBlock(p.Side(cube.FaceDown), Stone{}, nil)
				}
				tx.SetBlock(left.Side(facing.Opposite().Face()), Stone{}, nil)
				tx.SetBlock(right.Side(facing.Face()), Stone{}, nil)
				tx.SetBlock(left, TripwireHook{Facing: facing}, nil)
				tx.SetBlock(right, TripwireHook{Facing: facing.Opposite()}, nil)
				for _, p := range trace.Wires {
					tx.SetBlock(pos(p), Tripwire{}, nil)
				}
				tx.SetBlock(output, RedstoneWire{}, nil)
			})
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					compareNativeTripwireBlock(t, tx, sample.Tick, left, sample.Left)
					compareNativeTripwireBlock(t, tx, sample.Tick, right, sample.Right)
					compareNativeTripwireBlock(t, tx, sample.Tick, output, sample.Output)
					if len(sample.Wires) != len(trace.Wires) {
						t.Error("missing native string state")
						return
					}
					for i, p := range trace.Wires {
						compareNativeTripwireBlock(t, tx, sample.Tick, pos(p), sample.Wires[i])
					}
					if sample.Tick == trace.Configuration.Tick {
						b := tx.Block(cut).(Tripwire)
						b.Disarmed = true
						tx.SetBlock(cut, b, nil)
					}
					if sample.Tick == trace.Changes[0].Tick {
						tx.SetBlock(cut, Air{}, nil)
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
