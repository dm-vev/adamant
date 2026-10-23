package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestAquaticVariantsSaveAsBDS(t *testing.T) {
	const packed = 1 | 1<<8 | 11<<16 | 7<<24 // large stripey, blue and gray
	data := &world.EntityData{}
	TropicalFishType.DecodeNBT(map[string]any{"id": "minecraft:tropical_fish", "Variant": int32(packed)}, data)
	saved := TropicalFishType.EncodeNBT(data)
	for key, want := range map[string]any{"Variant": int32(1), "MarkVariant": int32(1), "Color": byte(11), "Color2": byte(7)} {
		if saved[key] != want {
			t.Errorf("tropical fish %s = %v, want %v", key, saved[key], want)
		}
	}
	reloaded := &world.EntityData{}
	TropicalFishType.DecodeNBT(saved, reloaded)
	if got := reloaded.Data.(*AquaticMobBehaviour).variant; got != packed {
		t.Errorf("reloaded tropical fish variant %#x, want %#x", got, packed)
	}

	cases := []struct {
		name string
		t    aquaticMobType
		nbt  map[string]any
		want int
	}{
		{"BDS tropical fish", TropicalFishType, map[string]any{"identifier": "minecraft:tropicalfish", "Variant": int32(1), "MarkVariant": int32(2), "Color": byte(14), "Color2": byte(0)}, 1 | 2<<8 | 14<<16},
		{"BDS cyan axolotl", AxolotlType, map[string]any{"identifier": "minecraft:axolotl", "Variant": int32(1)}, 3},
		{"BDS large salmon", SalmonType, map[string]any{"identifier": "minecraft:salmon", "Variant": int32(0), "definitions": []any{"+minecraft:salmon", "+scale_large"}}, 2},
		{"earlier Adamant save", TropicalFishType, map[string]any{"identifier": "minecraft:tropicalfish", "Variant": int32(packed), "PuffState": int32(0)}, packed},
	}
	for _, c := range cases {
		data := &world.EntityData{}
		c.t.DecodeNBT(c.nbt, data)
		if got := data.Data.(*AquaticMobBehaviour).variant; got != c.want {
			t.Errorf("%s: variant %#x, want %#x", c.name, got, c.want)
		}
	}
}
