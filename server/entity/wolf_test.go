package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestWolfNBTPreservesTamableAndArmorState(t *testing.T) {
	const ownerUUID = "123e4567-e89b-12d3-a456-426614174000"
	data := &world.EntityData{}
	WolfType.DecodeNBT(map[string]any{
		"Health": float32(40), "MaxHealth": float32(40),
		"Owner": javaUUIDIntArray(ownerUUID), "CollarColor": int32(14), "variant": "minecraft:rusty",
		"sound_variant": "minecraft:puglin", "Sitting": byte(1), "Age": int32(-1200),
		"equipment": map[string]any{"body": map[string]any{
			"id": "minecraft:wolf_armor", "count": int32(1),
			"components": map[string]any{"minecraft:damage": int32(7)},
		}},
	}, data)
	nbt := WolfType.EncodeNBT(data)
	javaState := ReadNativeMobJavaState(nbt)
	if javaState["Tame"] != byte(1) || javaUUIDString(javaState["Owner"]) != ownerUUID || javaState["variant"] != "minecraft:rusty" || javaState["sound_variant"] != "minecraft:puglin" || javaState["CollarColor"] != int32(14) || javaState["Sitting"] != byte(1) || javaState["Age"] != int32(-1200) {
		t.Fatalf("wolf Java state did not persist: %#v", javaState)
	}
	if nbt["IsTamed"] != byte(1) || nbt["Sitting"] != byte(1) {
		t.Fatalf("native wolf state did not persist: %#v", nbt)
	}
	variant, ok := NativeMobVariant(nbt)
	if !ok || variant != 8 {
		t.Fatalf("native wolf variant = %d, want 8", variant)
	}
	armorList, ok := nbt["Armor"].([]any)
	if !ok || len(armorList) != 5 {
		t.Fatalf("native wolf armor slots did not persist: %#v", nbt["Armor"])
	}
	armor, ok := armorList[4].(map[string]any)
	if !ok {
		t.Fatalf("native wolf body armor is not a compound: %#v", armorList[4])
	}
	restoredArmor := item.ReadNBT(armor, nil, world.DefaultBlockRegistry)
	if _, ok := restoredArmor.Item().(item.WolfArmor); !ok || restoredArmor.Count() != 1 || restoredArmor.Durability() != item.NewStack(item.WolfArmor{}, 1).Durability()-7 {
		t.Fatalf("native wolf body armor did not restore its item/count/durability: %v", restoredArmor)
	}
	javaEquipment, ok := javaState["equipment"].(map[string]any)
	if !ok {
		t.Fatalf("wolf Java equipment state did not persist: %#v", javaState["equipment"])
	}
	body, ok := javaEquipment["body"].(map[string]any)
	if !ok || body["id"] != "minecraft:wolf_armor" {
		t.Fatalf("wolf Java body armor did not persist: %#v", javaEquipment["body"])
	}
	components, ok := body["components"].(map[string]any)
	if !ok || components["minecraft:damage"] != int32(7) {
		t.Fatalf("wolf Java armor durability did not persist: %#v", body["components"])
	}
	if _, ok := data.Data.(*WolfBehaviour).bodyArmor.Item().(item.WolfArmor); !ok {
		t.Fatalf("restored armor item = %T", data.Data.(*WolfBehaviour).bodyArmor.Item())
	}
}
func TestNewFromJavaNBTRejectsUnknownWolfEquipment(t *testing.T) {
	_, err := NewFromStructureNBT(map[string]any{
		"id": "minecraft:wolf",
		"equipment": map[string]any{"body": map[string]any{"id": "minecraft:not_an_item", "count": int32(1)}},
	}, world.EntitySpawnOpts{})
	if err == nil {
		t.Fatal("unknown wolf equipment item was accepted")
	}
}
