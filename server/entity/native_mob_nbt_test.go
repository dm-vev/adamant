package entity

import "testing"

func TestMergeNativeMobNBTPreservesUnknownActorState(t *testing.T) {
	loaded := map[string]any{
		"identifier": "minecraft:wolf",
		"UniqueID":   int64(-4294967292),
		"OwnerNew":   int64(-1),
		"Attributes": []any{
			map[string]any{"Name": "minecraft:health", "Base": float32(8), "Current": float32(5), "DefaultMin": float32(0), "DefaultMax": float32(8), "Min": float32(0), "Max": float32(8), "NativeExtension": int32(71)},
			map[string]any{"Name": "minecraft:movement", "Base": float32(.3), "Current": float32(.3), "DefaultMin": float32(0), "DefaultMax": float32(3.4e38), "Min": float32(0), "Max": float32(3.4e38)},
			map[string]any{"Name": "minecraft:future_attribute", "Base": float32(2), "Extension": "keep"},
		},
		"Armor":            []any{map[string]any{"Slot": int8(0), "Name": "minecraft:custom_wolf_armor", "Count": byte(1), "NativeExtension": "preserve"}},
		"FutureActorField": map[string]any{"Enabled": byte(1)},
	}
	retained := CloneNativeMobNBT(loaded)
	retained["FutureActorField"].(map[string]any)["Enabled"] = byte(0)
	if loaded["FutureActorField"].(map[string]any)["Enabled"] != byte(1) {
		t.Fatal("clone shared nested actor data with source map")
	}

	attrs := ReadNativeMobAttributes(retained)
	encodedAttrs := EncodeNativeMobAttributes(attrs, []NativeMobAttribute{{Name: "minecraft:health", Base: 8, Current: 3, DefaultMin: 0, DefaultMax: 8, Min: 0, Max: 8}})
	encoded := MergeNativeMobNBT(retained, map[string]any{
		"Attributes": encodedAttrs,
		"OwnerNew":   int64(9001),
		"Armor":      []any{map[string]any{"Slot": int8(0), "Name": "minecraft:iron_horse_armor", "Count": byte(1)}},
	})
	if encoded["UniqueID"] != int64(-4294967292) || encoded["OwnerNew"] != int64(9001) {
		t.Fatalf("native scalar identity/owner lost or not overridden: %#v", encoded)
	}
	if encoded["FutureActorField"].(map[string]any)["Enabled"] != byte(0) {
		t.Fatalf("unknown actor compound lost: %#v", encoded["FutureActorField"])
	}
	gotAttrs := ReadNativeMobAttributes(encoded)
	if len(gotAttrs) != 3 || gotAttrs[0].Current != 3 || gotAttrs[0].Raw["NativeExtension"] != int32(71) || gotAttrs[2].Raw["Extension"] != "keep" {
		t.Fatalf("native attributes were not merged by Name: %#v", gotAttrs)
	}
	armor, ok := encoded["Armor"].([]any)
	if !ok || len(armor) != 1 || armor[0].(map[string]any)["NativeExtension"] != "preserve" || armor[0].(map[string]any)["Name"] != "minecraft:iron_horse_armor" {
		t.Fatalf("native armor slot was not updated in place: %#v", encoded["Armor"])
	}
}

func TestNativeMobJavaStateUsesSeparateNamespace(t *testing.T) {
	native := map[string]any{"UniqueID": int64(4), "OwnerNew": int64(-1)}
	java := MergeNativeMobJavaState(native, map[string]any{"AngerTime": int32(17), "AngryAt": "java-player-uuid"})
	if _, ok := java["AngerTime"]; ok {
		t.Fatal("Java anger leaked into native actor fields")
	}
	state := ReadNativeMobJavaState(java)
	if state["AngerTime"] != int32(17) || state["AngryAt"] != "java-player-uuid" || java["OwnerNew"] != int64(-1) {
		t.Fatalf("Java-only state did not round-trip independently: native=%#v java=%#v", java, state)
	}
}

func TestNativeMobEquipmentAndActorFieldCodecs(t *testing.T) {
	raw := map[string]any{
		"UniqueID":   int64(-4294967294),
		"OwnerNew":   int64(-1),
		"IsAngry":    byte(0),
		"Variant":    int32(8),
		"Color":      byte(14),
		"Armor":      []any{map[string]any{"Name": "", "Count": byte(0), "Damage": int32(0), "WasPickedUp": byte(0), "NativeExtension": "kept"}},
		"Mainhand":   []any{map[string]any{"Name": "", "Count": byte(0), "Damage": int32(0), "WasPickedUp": byte(0)}},
		"ChestItems": []any{map[string]any{"Slot": byte(0), "Name": "", "Count": byte(0), "Damage": int32(0), "WasPickedUp": byte(0)}},
	}
	equipment := ReadNativeMobEquipment(raw)
	equipment.Armor[0].Name, equipment.Armor[0].Count = "minecraft:wolf_armor", 1
	equipment.Mainhand[0].Name, equipment.Mainhand[0].Count = "minecraft:bone", 1
	slot := int32(0)
	equipment.ChestItems[0].Slot = &slot
	encoded := MergeNativeMobNBT(raw, EncodeNativeMobEquipment(equipment))
	armor := encoded["Armor"].([]any)[0].(map[string]any)
	if armor["Name"] != "minecraft:wolf_armor" || armor["Count"] != byte(1) || armor["NativeExtension"] != "kept" {
		t.Fatalf("native armor slot did not preserve/update fields: %#v", armor)
	}
	chestItem := encoded["ChestItems"].([]any)[0].(map[string]any)
	if chestItem["Slot"] != byte(0) {
		t.Fatalf("native ChestItems Slot must remain a byte: %#v", chestItem["Slot"])
	}
	if id, ok := NativeMobUniqueID(encoded); !ok || id != -4294967294 {
		t.Fatalf("native UniqueID=%d, ok=%t", id, ok)
	}
	if owner, ok := NativeMobOwnerID(encoded); !ok || owner != -1 {
		t.Fatalf("native owner=%d, ok=%t", owner, ok)
	}
	if angry := NativeMobAngry(encoded); angry {
		t.Fatal("zero native IsAngry decoded as true")
	}
	encoded = SetNativeMobOwnerID(encoded, 1234)
	encoded = SetNativeMobAngry(encoded, true)
	encoded = SetNativeMobVariant(encoded, 3)
	encoded = SetNativeMobColor(encoded, 5)
	if owner, ok := NativeMobOwnerID(encoded); !ok || owner != 1234 || !NativeMobAngry(encoded) {
		t.Fatalf("native owner/anger setters did not preserve native types: %#v", encoded)
	}
	if variant, ok := NativeMobVariant(encoded); !ok || variant != 3 {
		t.Fatalf("native Variant=%d, ok=%t", variant, ok)
	}
	if color, ok := NativeMobColor(encoded); !ok || color != 5 {
		t.Fatalf("native Color=%d, ok=%t", color, ok)
	}
}
