package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestNewFromJavaNBTBuildsWorldlessShulkerAndPreservesState(t *testing.T) {
	position := mgl64.Vec3{12.5, 70, -9.25}
	rotation := cube.Rotation{135, -10}
	handle, err := NewFromJavaNBT(map[string]any{
		"id":         "minecraft:shulker",
		"AttachFace": "east",
		"Peek":       int8(73),
		"Color":      int8(5),
		"Health":     float32(18),
	}, world.EntitySpawnOpts{Position: position, Rotation: rotation})
	if err != nil {
		t.Fatal(err)
	}
	if handle.World() != nil || handle.Type() != ShulkerType {
		t.Fatalf("expected a worldless shulker handle, got world=%v type=%v", handle.World(), handle.Type().EncodeEntity())
	}
	w := world.Config{Entities: DefaultRegistry, Synchronous: true}.New()
	defer w.Close()
	var nbt map[string]any
	<-w.Exec(func(tx *world.Tx) {
		shulker := tx.AddEntity(handle).(*Shulker)
		if shulker.Position() != position || shulker.Rotation() != rotation {
			t.Fatalf("spawn transform = %v/%v, want %v/%v", shulker.Position(), shulker.Rotation(), position, rotation)
		}
		if shulker.Health() != 18 || shulker.AttachFace() != cube.FaceEast || shulker.Peek() != 73 || shulker.Color() != 5 {
			t.Fatalf("decoded shulker state = health %v, face %v, peek %v, color %v", shulker.Health(), shulker.AttachFace(), shulker.Peek(), shulker.Color())
		}
		nbt = ShulkerType.EncodeNBT(shulker.data)
	})
	javaState := ReadNativeMobJavaState(nbt)
	if javaState["AttachFace"] != "east" || javaState["Peek"] != int8(73) || javaState["Color"] != int8(5) {
		t.Fatalf("encoded Java shulker state = %#v", javaState)
	}
	health, ok := nativeMobAttribute(nbt, "minecraft:health")
	if !ok || health.Current != 18 || health.Base != 30 {
		t.Fatalf("native shulker health attribute = %#v, want current 18/base 30", health)
	}
	color, ok := NativeMobColor(nbt)
	if !ok || color != 5 {
		t.Fatalf("native shulker color = %d, want 5", color)
	}
}

func TestNewFromJavaNBTRejectsUnknownEntity(t *testing.T) {
	if _, err := NewFromJavaNBT(map[string]any{"id": "minecraft:unknown_template_mob"}, world.EntitySpawnOpts{}); err == nil {
		t.Fatal("expected an unknown Java entity id to fail rather than create a placeholder")
	}
}
