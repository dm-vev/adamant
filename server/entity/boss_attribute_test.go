package entity

import (
	"math"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBossSpeedEffectsAndZeroPersistence(t *testing.T) {
	for _, kind := range []world.EntityType{WardenType, PhantomType, WitherType} {
		t.Run(kind.EncodeEntity(), func(t *testing.T) {
			var data world.EntityData
			kind.DecodeNBT(map[string]any{"Attributes": []any{
				map[string]any{"Name": "minecraft:movement", "Current": float32(.125)},
			}}, &data)
			mob := kind.Open(nil, nil, &data).(Living)
			mob.AddEffect(effect.New(effect.Speed, 1, time.Second))
			if got := mob.Speed(); math.Abs(got-.15) > 1e-12 {
				t.Fatalf("Speed I on configured movement .125 = %g, want .15", got)
			}
			mob.RemoveEffect(effect.Speed)
			if got := mob.Speed(); math.Abs(got-.125) > 1e-12 {
				t.Fatalf("speed after effect removal = %g, want .125", got)
			}
			mob.SetSpeed(0)
			var restored world.EntityData
			kind.DecodeNBT(kind.EncodeNBT(&data), &restored)
			if got := kind.Open(nil, nil, &restored).(Living).Speed(); got != 0 {
				t.Fatalf("zero movement became default speed %g after native reload", got)
			}
		})
	}
}

func TestPhantomSizeDoesNotScaleHealth(t *testing.T) {
	var data world.EntityData
	PhantomType.DecodeNBT(map[string]any{"Size": int32(64)}, &data)
	phantom := PhantomType.Open(nil, nil, &data).(*Phantom)
	if phantom.Health() != 20 || phantom.MaxHealth() != 20 {
		t.Fatalf("size-64 phantom health = %g/%g, want 20/20", phantom.Health(), phantom.MaxHealth())
	}
}
