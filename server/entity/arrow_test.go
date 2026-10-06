package entity

import (
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestCalculateArrowDamage(t *testing.T) {
	tests := []struct {
		name             string
		critical         bool
		power            int
		minimum, maximum float64
	}{
		{name: "normal", minimum: 5.85, maximum: 5.85},
		{name: "power", power: 3, minimum: 11.7, maximum: 11.7},
		{name: "critical", critical: true, minimum: 9, maximum: 10},
		{name: "power critical", critical: true, power: 3, minimum: 18, maximum: 20},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := calculateArrowDamage(1, 5, test.critical, test.power)
			if got < test.minimum || got > test.maximum {
				t.Fatalf("damage = %v, want range [%v, %v]", got, test.minimum, test.maximum)
			}
		})
	}
}

func TestArrowNBTRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		damage     float64
		critical   bool
		powerLevel int
	}{
		{name: "plain", damage: 1},
		{name: "power critical", damage: 3.5, critical: true, powerLevel: 5},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conf := arrowConf
			conf.Damage, conf.Critical, conf.powerLevel = test.damage, test.critical, test.powerLevel
			encoded := ArrowType.EncodeNBT(&world.EntityData{Data: conf.New()})

			decoded := new(world.EntityData)
			ArrowType.DecodeNBT(encoded, decoded)
			got := decoded.Data.(*ProjectileBehaviour).conf
			if got.Damage != test.damage || got.Critical != test.critical || got.powerLevel != test.powerLevel {
				t.Fatalf("decoded arrow = {damage: %v, critical: %v, power: %v}, want {damage: %v, critical: %v, power: %v}", got.Damage, got.Critical, got.powerLevel, test.damage, test.critical, test.powerLevel)
			}
			if reencoded := ArrowType.EncodeNBT(decoded); !reflect.DeepEqual(reencoded, encoded) {
				t.Fatalf("round-trip NBT differs\ngot:  %#v\nwant: %#v", reencoded, encoded)
			}
		})
	}
}

func TestArrowUsesBDSStairRayShape(t *testing.T) {
	// BDS 1.26.52.3 projectile captures use the straight stair outline even
	// where an outer corner removes physical collision from this quadrant.
	for _, scenario := range []struct {
		name                    string
		upsideDown              bool
		startY, velocityY, hitY float64
	}{
		{"above outer corner", false, 66, -3, 65},
		{"below upside-down outer corner", true, 62.5, 3, 64},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			t.Cleanup(func() { _ = w.Close() })
			b, ok := world.BlockByName("minecraft:oak_stairs", map[string]any{
				"minecraft:corner": "outer_right", "upside_down_bit": scenario.upsideDown, "weirdo_direction": int32(0),
			})
			if !ok {
				t.Fatal("oak stairs unavailable")
			}
			stair := b.(block.Stairs)
			stair.Facing = cube.East
			stair.UpsideDown = scenario.upsideDown
			stair.Corner = model.StairCornerOuterRight
			conf := arrowConf
			conf.Gravity, conf.Drag = 0, 0
			handle := (world.EntitySpawnOpts{
				Position: mgl64.Vec3{8.75, scenario.startY, 8.25},
				Velocity: mgl64.Vec3{0, scenario.velocityY, 0},
			}).New(ArrowType, conf)
			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{8, 64, 8}
				tx.SetBlock(pos, stair, nil)
				tx.AddEntity(handle)
				entity, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("arrow did not enter world")
				}
				arrow := entity.(*Ent)
				behaviour := arrow.data.Data.(*ProjectileBehaviour)
				behaviour.Tick(arrow, tx)
				if !behaviour.collided || behaviour.collisionPos != pos || math.Abs(arrow.Position()[1]-scenario.hitY) > 0.00001 {
					t.Fatalf("arrow hit = %v at %v, want block %v at Y=%g", behaviour.collided, arrow.Position(), pos, scenario.hitY)
				}
			})
		})
	}
}
