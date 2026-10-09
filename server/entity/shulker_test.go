package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
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

func TestShulkerSurvivesMCDBRestart(t *testing.T) {
	coloured, err := NewFromJavaNBT(map[string]any{
		"id": "minecraft:shulker", "AttachFace": "east",
		"Peek": int8(73), "Color": int8(5), "Health": float32(18),
		"FutureSourceData": map[string]any{
			"Flag": int8(-1), "Bytes": []int8{-128, -1, 0, 127},
			"Records": []any{map[string]any{"Flag": int8(-128)}},
		},
	}, world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 70, 8.5}})
	if err != nil {
		t.Fatal(err)
	}
	natural := NewShulker(world.EntitySpawnOpts{Position: mgl64.Vec3{8.5, 70, 8.5}}, ShulkerBehaviourConfig{})
	cases := []struct {
		handle       *world.EntityHandle
		face         cube.Face
		peek, colour uint8
		health       float64
	}{
		{natural, cube.FaceDown, 0, 16, 30},
		{coloured, cube.FaceEast, 73, 5, 18},
	}
	path := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Synchronous: true, Provider: db, Generator: world.NopGenerator{}, Entities: DefaultRegistry, RandomTickSpeed: -1}.New()
	}
	w := open()
	t.Cleanup(func() {
		if w != nil {
			_ = w.Close()
		}
	})
	<-w.Exec(func(tx *world.Tx) {
		for _, tc := range cases {
			tx.AddEntity(tc.handle)
		}
	})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w = nil
	w = open()
	found := make([]bool, len(cases))
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(cube.Pos{8, 70, 8})
		for e := range tx.Entities() {
			for i, tc := range cases {
				if e.H().UUID() != tc.handle.UUID() {
					continue
				}
				shulker, ok := e.(*Shulker)
				if !ok {
					t.Errorf("restored actor = %T", e)
					continue
				}
				found[i] = true
				if shulker.AttachFace() != tc.face || shulker.Peek() != tc.peek || shulker.Color() != tc.colour || shulker.Health() != tc.health {
					t.Errorf("restored shulker %d: face=%v peek=%d colour=%d health=%v", i, shulker.AttachFace(), shulker.Peek(), shulker.Color(), shulker.Health())
				}
			}
		}
	})
	for i, restored := range found {
		if !restored {
			t.Errorf("shulker %d disappeared after MCDB restart", i)
		}
	}
}
