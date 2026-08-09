package entity

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
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
