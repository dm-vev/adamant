package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestCowNBTAndMilkAge(t *testing.T) {
	data := &world.EntityData{}
	CowType.DecodeNBT(map[string]any{
		"Health": float32(10), "Age": int32(-100),
		"variant": "minecraft:cold", "sound_variant": "minecraft:moody",
	}, data)
	got := CowType.EncodeNBT(data)
	javaState := ReadNativeMobJavaState(got)
	if javaState["Age"] != int32(-100) || javaState["variant"] != "minecraft:cold" || javaState["sound_variant"] != "minecraft:moody" {
		t.Fatalf("cow Java state did not round-trip: %#v", javaState)
	}
	health, ok := nativeMobAttribute(got, "minecraft:health")
	if !ok || health.Current != 10 || health.Base != 10 {
		t.Fatalf("native cow health attribute = %#v, want current/base 10", health)
	}
	baby := &Cow{Ent: &Ent{data: data}}
	if baby.Milk(nil) {
		t.Fatal("baby cow could be milked")
	}
	adultData := &world.EntityData{}
	CowType.DecodeNBT(map[string]any{"Health": float32(10), "Age": int32(0)}, adultData)
	adult := &Cow{Ent: &Ent{data: adultData}}
	if !adult.Milk(nil) {
		t.Fatal("adult cow could not be milked")
	}
}
