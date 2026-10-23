package entity

import (
	"context"
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

func TestDrownedJavaSpawnEquipmentAttributesRoundTrip(t *testing.T) {
	weapon := map[string]any{"id": "minecraft:iron_sword", "count": int32(1), "components": map[string]any{
		"minecraft:damage":       int32(7),
		"minecraft:enchantments": map[string]any{"levels": map[string]any{"minecraft:sharpness": int32(3)}},
	}}
	helm := map[string]any{"id": "minecraft:carved_pumpkin", "count": int32(1)}
	source := map[string]any{
		"Health": float32(6), "LeftHanded": true,
		"equipment":    map[string]any{"mainhand": weapon, "head": helm, "offhand": map[string]any{"id": "minecraft:nautilus_shell", "count": int32(1)}},
		"drop_chances": map[string]any{"offhand": float32(2), "head": float32(0)},
		"attributes": []any{
			map[string]any{"id": "minecraft:max_health", "base": float64(20), "modifiers": []any{map[string]any{"amount": float64(12), "operation": "add_value"}}},
			map[string]any{"id": "minecraft:movement_speed", "base": float64(.31)},
			map[string]any{"id": "minecraft:follow_range", "base": float64(40), "modifiers": []any{map[string]any{"amount": float64(.2), "operation": "add_multiplied_base"}}},
			map[string]any{"id": "minecraft:knockback_resistance", "base": float64(.4)},
		},
	}
	var data world.EntityData
	data.NativeNBT = CloneNativeMobNBT(source)
	DrownedType.DecodeNBT(source, &data)
	state := data.Data.(*oceanBehaviour)
	if state.health.MaxHealth() != 32 || state.health.Health() != 6 || state.speed != .31 || state.followRange != 48 || state.knockbackResistance != .4 {
		t.Fatalf("spawn health/attributes decoded incorrectly: max=%v health=%v speed=%v follow=%v kb=%v", state.health.MaxHealth(), state.health.Health(), state.speed, state.followRange, state.knockbackResistance)
	}
	if !state.leftHanded || state.mainHand.Empty() || state.head.Empty() {
		t.Fatalf("spawn equipment/handedness missing: %#v", state)
	}
	encoded := DrownedType.EncodeNBT(&data)
	if encoded["LeftHanded"] != byte(1) || encoded["drop_chances"].(map[string]any)["offhand"] != float32(2) || encoded["drop_chances"].(map[string]any)["head"] != float32(0) {
		t.Fatalf("mob state lost on encode: %#v", encoded)
	}
	var restarted world.EntityData
	restarted.NativeNBT = CloneNativeMobNBT(encoded)
	DrownedType.DecodeNBT(encoded, &restarted)
	again := restarted.Data.(*oceanBehaviour)
	if again.health.MaxHealth() != 32 || again.health.Health() != 6 || again.speed != .31 || again.followRange != 48 || again.knockbackResistance != .4 || again.mainHand.Empty() || again.head.Empty() {
		t.Fatalf("round-tripped mob state changed: %#v", again)
	}
	if wear := again.mainHand.MaxDurability() - again.mainHand.Durability(); wear != 7 {
		t.Fatalf("round-tripped held weapon wear = %d, want 7", wear)
	}
	if sharpness := oceanStackEnchantmentLevel(again.mainHand, "Sharpness", "minecraft:sharpness"); sharpness != 3 {
		t.Fatalf("round-tripped held weapon Sharpness = %d, want 3", sharpness)
	}
}

func TestZombieNautilusSourceDefaultsAndVariant(t *testing.T) {
	source := map[string]any{
		"Health": float32(9), "variant": "minecraft:warm",
		"attributes": []any{
			map[string]any{"id": "minecraft:movement_speed", "base": float64(1.1)},
			map[string]any{"id": "minecraft:follow_range", "base": float64(16)},
			map[string]any{"id": "minecraft:knockback_resistance", "base": float64(.3)},
		},
	}
	var data world.EntityData
	data.NativeNBT = CloneNativeMobNBT(source)
	ZombieNautilusType.DecodeNBT(source, &data)
	mob := ZombieNautilusType.Open(nil, nil, &data).(*ZombieNautilus)
	if mob.Health() != 9 || mob.MaxHealth() != 15 || mob.Speed() != 1.1 || mob.Variant() != "warm" {
		t.Fatalf("Source Zombie Nautilus defaults/state: health=%v max=%v speed=%v variant=%q", mob.Health(), mob.MaxHealth(), mob.Speed(), mob.Variant())
	}
	if got := ZombieNautilusType.EncodeNBT(&data)["variant"]; got != "minecraft:warm" {
		t.Fatalf("Source variant namespace not retained: %#v", got)
	}
}

func TestDrownedBabyTransientMovementModifierSurvivesReloadOnce(t *testing.T) {
	source := map[string]any{
		"Health": float32(20), "IsBaby": byte(1),
		"attributes": []any{map[string]any{"id": "minecraft:movement_speed", "base": float64(.23000000417232513)}},
	}
	var data world.EntityData
	data.NativeNBT = CloneNativeMobNBT(source)
	DrownedType.DecodeNBT(source, &data)
	mob := DrownedType.Open(nil, nil, &data).(*Drowned)
	if got := mob.Speed(); got != .23000000417232513*1.5 {
		t.Fatalf("baby runtime speed=%v, want Source transient modifier", got)
	}
	encoded := DrownedType.EncodeNBT(&data)
	if encoded["attributes"].([]any)[0].(map[string]any)["base"] != float64(.23000000417232513) {
		t.Fatalf("transient modifier was written into Source base attribute: %#v", encoded["attributes"])
	}
	var restarted world.EntityData
	restarted.NativeNBT = CloneNativeMobNBT(encoded)
	DrownedType.DecodeNBT(encoded, &restarted)
	again := DrownedType.Open(nil, nil, &restarted).(*Drowned)
	if again.Speed() != mob.Speed() {
		t.Fatalf("baby speed changed or multiplied twice after reload: before=%v after=%v", mob.Speed(), again.Speed())
	}
}

func TestDrownedJockeyRiderFirstMCDBReloadAndTick(t *testing.T) {
	for _, kind := range []string{"minecraft:chicken", "minecraft:zombie_nautilus"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			open := func() *world.World {
				db, err := mcdb.Open(dir)
				if err != nil {
					t.Fatal(err)
				}
				return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
			}
			w := open()
			t.Cleanup(func() { _ = w.Close() })
			mountPos, riderPos := mgl64.Vec3{.5, 65, .5}, mgl64.Vec3{16.5, 65, .5}
			data := map[string]any{"id": kind, "Health": float32(15), "variant": "minecraft:warm"}
			baby := byte(0)
			if kind == "minecraft:chicken" {
				data = map[string]any{"id": kind, "Health": float32(4), "IsChickenJockey": byte(1), "EggLayTime": int32(1), "variant": "minecraft:cold"}
				baby = 1
			}
			mount, err := NewFromStructureNBT(data, world.EntitySpawnOpts{Position: mountPos, Rotation: cube.Rotation{90, 0}, SpawnReason: "structure", SpawnFinalized: true})
			if err != nil {
				t.Fatal(err)
			}
			rider, err := NewFromStructureNBT(map[string]any{"id": "minecraft:drowned", "Health": float32(20), "IsBaby": baby}, world.EntitySpawnOpts{Position: riderPos, Riding: mount, SpawnReason: "structure", SpawnFinalized: true})
			if err != nil {
				t.Fatal(err)
			}
			<-w.Exec(func(tx *world.Tx) { tx.AddEntity(mount); tx.AddEntity(rider) })
			mountID, riderID := mount.UniqueID(), rider.UniqueID()
			w.Save()
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			w = open()
			<-w.Exec(func(tx *world.Tx) {
				tx.Block(cube.PosFromVec3(riderPos))
				for e := range tx.Entities() {
					if e.H().UniqueID() == riderID {
						rider = e.H()
					}
				}
				r, ok := rider.Entity(tx)
				if !ok || r.(Rider).Riding() != nil {
					t.Fatal("rider-first load did not leave the vehicle unresolved")
				}
				tx.Block(cube.PosFromVec3(mountPos))
				for e := range tx.Entities() {
					if e.H().UniqueID() == mountID {
						mount = e.H()
					}
				}
				if r.(Rider).Riding() != mount {
					t.Fatal("loading the vehicle did not resolve its saved rider")
				}
			})
			for range 4 {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					r, riderLoaded := rider.Entity(tx)
					v, mountLoaded := mount.Entity(tx)
					if !riderLoaded || !mountLoaded {
						t.Fatal("ticking lost a mounted actor")
					}
					offset := mgl64.Vec3{0, float64(float32(1.1375)) - float64(float32(.7)), 0}
					if kind == "minecraft:chicken" {
						sin, cos := math.Sincos(-v.Rotation().Yaw() * math.Pi / 180)
						offset = mgl64.Vec3{-.1 * sin, .7 - .1875, -.1 * cos}
					}
					if got, want := r.Position(), v.Position().Add(offset); !got.ApproxEqualThreshold(want, 1e-6) {
						t.Fatalf("passenger tick used stale vehicle position: got=%v Source=%v", got, want)
					}
					if r.(interface{ Velocity() mgl64.Vec3 }).Velocity() != (mgl64.Vec3{}) {
						t.Fatal("mounted drowned retained independent velocity")
					}
					for e := range tx.Entities() {
						if e.H().Type().EncodeEntity() == "minecraft:item" {
							t.Fatal("chicken jockey laid an egg")
						}
					}
				})
			}
		})
	}
}

func TestOceanLiveEquipmentMutationSurvivesMCDBAndDrop(t *testing.T) {
	dir := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
	}
	w := open()
	t.Cleanup(func() { _ = w.Close() })
	pos := mgl64.Vec3{.5, 65, .5}
	source := map[string]any{
		"id": "minecraft:drowned", "Health": float32(20),
		"equipment": map[string]any{"mainhand": map[string]any{
			"id": "minecraft:iron_sword", "count": int32(1),
			"components": map[string]any{
				"minecraft:damage":       int32(7),
				"minecraft:enchantments": map[string]any{"minecraft:looting": int32(3)},
				"minecraft:custom_data":  map[string]any{"consumer_tag": "retained"},
			},
		}},
		"drop_chances": map[string]any{"mainhand": float32(2)},
	}
	handle, err := NewFromStructureNBT(source, world.EntitySpawnOpts{Position: pos, SpawnReason: "structure", SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	<-w.Exec(func(tx *world.Tx) {
		mob := tx.AddEntity(handle).(*Drowned)
		main, off := mob.HeldItems()
		mob.SetHeldItems(main.WithCustomName("renamed by consumer").WithLore("updated equipment"), off)
	})
	w.Save()
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w = open()
	<-w.Exec(func(tx *world.Tx) {
		tx.Block(cube.PosFromVec3(pos))
		var mob *Drowned
		for e := range tx.Entities() {
			if e.H().UniqueID() == handle.UniqueID() {
				mob = e.(*Drowned)
			}
		}
		if mob == nil {
			t.Fatal("MCDB restart lost the equipped drowned")
		}
		check := func(stack item.Stack) {
			if stack.CustomName() != "renamed by consumer" || !reflect.DeepEqual(stack.Lore(), []string{"updated equipment"}) || stack.MaxDurability()-stack.Durability() != 7 || oceanStackEnchantmentLevel(stack, "Looting", "minecraft:looting") != 3 {
				t.Fatalf("live/source equipment metadata changed: %v", stack)
			}
			raw, err := item.StackToStructureNBT(stack)
			if err != nil {
				t.Fatal(err)
			}
			data, ok := raw["components"].(map[string]any)["minecraft:custom_data"].(map[string]any)
			if !ok || data["consumer_tag"] != "retained" {
				t.Fatalf("source-only equipment components lost: %#v", raw)
			}
		}
		main, _ := mob.HeldItems()
		check(main)
		mob.Hurt(100, DrowningDamageSource{})
		var dropped item.Stack
		for e := range tx.Entities() {
			if ent, ok := e.(*Ent); ok {
				if b, ok := ent.Behaviour().(*ItemBehaviour); ok {
					if id, _ := b.Item().Item().EncodeItem(); id == "minecraft:iron_sword" {
						dropped = b.Item()
					}
				}
			}
		}
		if dropped.Empty() {
			t.Fatal("guaranteed equipment drop was missing")
		}
		check(dropped)
	})
}

func TestNativeMapEquipmentAndClearedMetadataSurviveMCDB(t *testing.T) {
	dir := t.TempDir()
	open := func() *world.World {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		return world.Config{Provider: db, Entities: DefaultRegistry, Synchronous: true}.New()
	}
	w := open()
	t.Cleanup(func() { _ = w.Close() })
	pos := mgl64.Vec3{.5, 65, .5}
	handle, err := NewFromStructureNBT(map[string]any{"id": "minecraft:drowned"}, world.EntitySpawnOpts{Position: pos, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	stack := item.NewStack(item.FilledMap{NativeID: 987654321, HasNativeID: true}, 1).
		WithCustomName("native map without Java mapping").WithLore("consumer metadata")
	if err := w.Do(func(tx *world.Tx) {
		tx.AddEntity(handle).(*Drowned).SetHeldItems(stack, item.Stack{})
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	for stage := range 2 {
		w.Save()
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		w = open()
		var restored item.Stack
		if err := w.Do(func(tx *world.Tx) {
			tx.Block(cube.PosFromVec3(pos))
			for e := range tx.Entities() {
				if e.H().UniqueID() == handle.UniqueID() {
					mob := e.(*Drowned)
					restored, _ = mob.HeldItems()
					if stage == 0 {
						mob.SetHeldItems(restored.WithCustomName("").WithLore(), item.Stack{})
					}
					break
				}
			}
		}).Wait(context.Background()); err != nil {
			t.Fatal(err)
		}
		m, ok := restored.Item().(item.FilledMap)
		if !ok || !m.HasNativeID || m.NativeID != 987654321 || m.HasJavaID {
			t.Fatalf("restart %d changed an unmapped native map: %v", stage, restored)
		}
		if stage == 0 {
			if restored.CustomName() != stack.CustomName() || !reflect.DeepEqual(restored.Lore(), stack.Lore()) {
				t.Fatalf("restart lost native map metadata: %v", restored)
			}
		} else if restored.CustomName() != "" || len(restored.Lore()) != 0 {
			t.Fatalf("restart resurrected cleared native map name/lore: %v", restored)
		}
	}
}

func TestChickenJockeyDoesNotLaySourceVariantEgg(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for _, mount := range []bool{false, true} {
			pos := mgl64.Vec3{.5, 65, .5}
			if mount {
				pos[0] = 4.5
			}
			h, err := NewFromStructureNBT(map[string]any{"id": "minecraft:chicken", "Health": float32(4), "Age": int32(0), "EggLayTime": int32(1), "variant": "minecraft:cold", "IsChickenJockey": boolByte(mount)}, world.EntitySpawnOpts{Position: pos, SpawnReason: "structure", SpawnFinalized: true})
			if err != nil {
				t.Fatal(err)
			}
			tx.AddEntity(h)
		}
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		eggs := 0
		for e := range tx.Entities() {
			ent, ok := e.(*Ent)
			if !ok {
				continue
			}
			b, ok := ent.Behaviour().(*ItemBehaviour)
			if !ok {
				continue
			}
			egg, ok := b.Item().Item().(item.Egg)
			if !ok {
				continue
			}
			if egg.Variant != item.EggBlue || e.Position()[0] >= 2 {
				t.Fatalf("Source chicken variant/jockey egg behavior incorrect: egg=%v pos=%v", egg, e.Position())
			}
			eggs += b.Item().Count()
		}
		if eggs != 1 {
			t.Fatalf("ordinary chicken and jockey laid %d eggs, want only the ordinary chicken's blue egg", eggs)
		}
	})
}
