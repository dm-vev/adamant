package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type cowTestEntityConfig struct{}

func (cowTestEntityConfig) Apply(*world.EntityData) {}

func TestCowNBT(t *testing.T) {
	data := &world.EntityData{}
	CowType.DecodeNBT(map[string]any{"Health": float32(0), "MaxHealth": float32(12), "Variant": int32(2)}, data)

	nbt := CowType.EncodeNBT(data)
	if nbt["Health"] != float32(0) || nbt["MaxHealth"] != float32(12) || nbt["Variant"] != int32(2) {
		t.Fatalf("unexpected cow NBT: %#v", nbt)
	}
}

func TestCowConfigDefaults(t *testing.T) {
	cow := CowConfig{}.New()
	if cow.health.Health() != 10 || cow.health.MaxHealth() != 10 {
		t.Fatalf("unexpected default health: %v/%v", cow.health.Health(), cow.health.MaxHealth())
	}
}

func TestCowSpawnWithoutSpecialisedFactory(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		handle := world.EntitySpawnOpts{Position: mgl64.Vec3{0, 2, 0}}.New(CowType, cowTestEntityConfig{})
		tx.AddEntity(handle)
		entity, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("cow was not opened in the world")
		}
		cow, ok := entity.(*Cow)
		if !ok || cow.Health() != 10 {
			t.Fatalf("unexpected spawned cow: %T health=%v", entity, cow.Health())
		}
	})
}
