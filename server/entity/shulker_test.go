package entity

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

func TestNewFromJavaNBTBuildsWorldlessShulkerAndPreservesState(t *testing.T) {
	position := mgl64.Vec3{12.5, 70, -9.25}
	rotation := cube.Rotation{135, -10}
	handle, err := NewFromStructureNBT(map[string]any{
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
		// Java lime (5) is Bedrock variant 10.
		if shulker.Health() != 18 || shulker.AttachFace() != cube.FaceEast || shulker.Peek() != 73 || shulker.Color() != 10 {
			t.Fatalf("decoded shulker state = health %v, face %v, peek %v, color %v", shulker.Health(), shulker.AttachFace(), shulker.Peek(), shulker.Color())
		}
		nbt = ShulkerType.EncodeNBT(shulker.data)
	})
	health, ok := nativeMobAttribute(nbt, "minecraft:health")
	if !ok || health.Current != 18 || health.Base != 30 {
		t.Fatalf("native shulker health attribute = %#v, want current 18/base 30", health)
	}
	variant, ok := NativeMobVariant(nbt)
	if !ok || variant != 10 {
		t.Fatalf("native shulker variant = %d, want 10", variant)
	}
	if java := ReadNativeMobJavaState(nbt); java["Color"] != byte(5) {
		t.Fatalf("Java shulker colour = %v, want 5", java["Color"])
	}
}

func TestNewFromJavaNBTRejectsUnknownEntity(t *testing.T) {
	if _, err := NewFromStructureNBT(map[string]any{"id": "minecraft:unknown_template_mob"}, world.EntitySpawnOpts{}); err == nil {
		t.Fatal("expected an unknown Java entity id to fail rather than create a placeholder")
	}
}

func TestShulkerSurvivesMCDBRestart(t *testing.T) {
	coloured, err := NewFromStructureNBT(map[string]any{
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
		{coloured, cube.FaceEast, 73, 10, 18},
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

func TestShulkerBulletHitAppliesLevitation(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		owner := tx.AddEntity(NewShulker(world.EntitySpawnOpts{
			Position: mgl64.Vec3{6.5, 70, 8.5},
		}, ShulkerBehaviourConfig{Peek: 100})).(*Shulker)
		target := tx.AddEntity(NewShulker(world.EntitySpawnOpts{
			Position: mgl64.Vec3{12.5, 70, 8.5},
		}, ShulkerBehaviourConfig{Peek: 100})).(*Shulker)
		handle := NewShulkerBullet(world.EntitySpawnOpts{
			Position: owner.Position().Add(mgl64.Vec3{0, .5, 0}),
			Velocity: mgl64.Vec3{.15, 0, 0},
		}, owner.H(), target.H())
		bullet := tx.AddEntity(handle).(*ShulkerBullet)
		for tick := range 40 {
			if _, loaded := handle.Entity(tx); !loaded {
				break
			}
			bullet.Tick(tx, int64(tick))
		}
		if owner.Health() != 30 || len(owner.Effects()) != 0 {
			t.Errorf("bullet hit its launcher: health=%v effects=%v", owner.Health(), owner.Effects())
		}
		if target.Health() != 26 {
			t.Errorf("health after bullet hit = %v, want 26", target.Health())
		}
		effects := target.Effects()
		if len(effects) != 1 || effects[0].Type() != effect.Levitation ||
			effects[0].Level() != 1 || effects[0].Duration() != 10*time.Second {
			t.Fatalf("effects after bullet hit = %v, want Levitation I for 10 seconds", effects)
		}
		if _, loaded := handle.Entity(tx); loaded {
			t.Error("bullet remained in world after its hit")
		}
	})
}

func TestShulkerNativeVariantOverridesGenericColor(t *testing.T) {
	for _, variant := range []int32{0, 15, 16} {
		var data world.EntityData
		ShulkerType.DecodeNBT(map[string]any{
			"Variant": variant,
			"Color":   byte(5),
		}, &data)
		shulker := ShulkerType.Open(nil, nil, &data).(*Shulker)
		if got := shulker.Color(); got != uint8(variant) {
			t.Errorf("native Variant %d with generic Color 5 decoded shell colour %d", variant, got)
		}
	}
}

func TestShulkerBulletCanHitOwnerAfterLeaving(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		owner := tx.AddEntity(NewShulker(world.EntitySpawnOpts{
			Position: mgl64.Vec3{8.5, 70, 8.5},
		}, ShulkerBehaviourConfig{Peek: 100})).(*Shulker)
		handle := NewShulkerBullet(world.EntitySpawnOpts{
			Position: mgl64.Vec3{12.5, 70.5, 8.5},
			Velocity: mgl64.Vec3{.15, 0, 0},
		}, owner.H(), nil)
		bullet := tx.AddEntity(handle).(*ShulkerBullet)
		bullet.Tick(tx, 0)
		bullet.SetVelocity(mgl64.Vec3{-1, 0, 0})
		for tick := range 4 {
			bullet.Tick(tx, int64(tick+1))
		}
		if owner.Health() != 26 {
			t.Fatalf("returning bullet left launcher health %v, want 26", owner.Health())
		}
		effects := owner.Effects()
		if len(effects) != 1 || effects[0].Type() != effect.Levitation || effects[0].Level() != 1 {
			t.Fatalf("effects after returning bullet hit = %v, want Levitation I", effects)
		}
		bullet.Tick(tx, 5)
		if _, loaded := handle.Entity(tx); loaded {
			t.Error("returning bullet remained in world after its hit")
		}
	})
}
