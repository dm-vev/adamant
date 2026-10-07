package item

import "strings"

// FilledMapVariant identifies a Java map item ID. These named map items are
// used by loot and exploration-map trades; the referenced map contents live in
// the world map service.
type FilledMapVariant uint8

const (
	FilledMapDefault FilledMapVariant = iota
	FilledMapOceanMonument
	FilledMapWoodlandMansion
	FilledMapBuriedTrialChambers
	FilledMapJunglePyramid
	FilledMapSwampHut
	FilledMapDesertVillage
	FilledMapPlainsVillage
	FilledMapSavannaVillage
	FilledMapSnowyVillage
	FilledMapTaigaVillage
	FilledMapBuriedTreasure
	FilledMapBuriedAncientCity
	FilledMapBuriedMineshaft
	FilledMapDesertPyramid
	FilledMapAbandonedCamp
	FilledMapWarmOceanRuins
)

var filledMapItemIDs = [...]string{
	"filled_map",
	"ocean_monument_map",
	"woodland_mansion_map",
	"buried_trial_chambers_map",
	"jungle_pyramid_map",
	"swamp_hut_map",
	"desert_village_map",
	"plains_village_map",
	"savanna_village_map",
	"snowy_village_map",
	"taiga_village_map",
	"buried_treasure_map",
	"buried_ancient_city_map",
	"buried_mineshaft_map",
	"desert_pyramid_map",
	"abandoned_camp_map",
	"warm_ocean_ruins_map",
}

// Valid reports whether the variant has a Java map item identifier.
func (v FilledMapVariant) Valid() bool { return int(v) < len(filledMapItemIDs) }

// JavaItemID returns the Java item identifier for the map variant.
func (v FilledMapVariant) JavaItemID() string {
	if !v.Valid() {
		panic("invalid filled map variant")
	}
	return "minecraft:" + filledMapItemIDs[v]
}

// FilledMapVariantFromJavaItemID resolves a Java map item identifier.
func FilledMapVariantFromJavaItemID(id string) (FilledMapVariant, bool) {
	id = strings.TrimPrefix(id, "minecraft:")
	for variant, itemID := range filledMapItemIDs {
		if id == itemID {
			return FilledMapVariant(variant), true
		}
	}
	return 0, false
}

// FilledMap is a Bedrock filled map referencing persisted world data by its
// native signed map UUID. JavaID is a separate Java map registry ID.
type FilledMap struct {
	JavaID      int32
	HasJavaID   bool
	NativeID    int64
	HasNativeID bool
	Variant     FilledMapVariant
}

// MaxCount returns Java's default item stack size for maps.
func (FilledMap) MaxCount() int { return 64 }

func (FilledMap) EncodeItem() (name string, meta int16) {
	return "minecraft:filled_map", 0
}

// EncodeNBT stores native Bedrock map UUIDs separately from the Java map ID.
func (m FilledMap) EncodeNBT() map[string]any {
	data := make(map[string]any, 2)
	if m.HasNativeID {
		data["map_uuid"] = m.NativeID
	}
	if m.HasJavaID {
		data["adamant:java_map_id"] = m.JavaID
	}
	if len(data) == 0 {
		return nil
	}
	return data
}

// DecodeNBT restores native Bedrock map UUIDs and the distinct Java map ID.
func (m FilledMap) DecodeNBT(data map[string]any) any {
	switch id := data["map_uuid"].(type) {
	case int64:
		m.NativeID, m.HasNativeID = id, true
	case int32:
		m.NativeID, m.HasNativeID = int64(id), true
	case int:
		m.NativeID, m.HasNativeID = int64(id), true
	}
	switch id := data["adamant:java_map_id"].(type) {
	case int32:
		m.JavaID, m.HasJavaID = id, true
	case int16:
		m.JavaID, m.HasJavaID = int32(id), true
	case int:
		m.JavaID, m.HasJavaID = int32(id), true
	}
	return m
}
