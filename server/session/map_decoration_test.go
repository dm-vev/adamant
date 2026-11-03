package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestMapDecorationTypesAreProtocolTypes checks the map decorations of
// exploration maps against protocol 2193: the session sends the type the
// world gives a decoration as it is.
func TestMapDecorationTypesAreProtocolTypes(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]byte{
		"minecraft:red_x":            v2193.MapDecorationTypeCrossWhite,
		"minecraft:woodland_mansion": v2193.MapDecorationTypeMansion,
		"minecraft:monument":         v2193.MapDecorationTypeMonument,
		"minecraft:village_desert":   v2193.MapDecorationTypeVillageDesert,
		"minecraft:village_plains":   v2193.MapDecorationTypeVillagePlains,
		"minecraft:village_savanna":  v2193.MapDecorationTypeVillageSavanna,
		"minecraft:village_snowy":    v2193.MapDecorationTypeVillageSnowy,
		"minecraft:village_taiga":    v2193.MapDecorationTypeVillageTaiga,
		"minecraft:jungle_temple":    v2193.MapDecorationTypeJungleTemple,
		"minecraft:swamp_hut":        v2193.MapDecorationTypeWitchHut,
		"minecraft:trial_chambers":   v2193.MapDecorationTypeTrialChambers,
		"minecraft:abandoned_camp":   v2193.MapDecorationTypeAbandonedCamp,
		"minecraft:ancient_city":     v2193.MapDecorationTypeBuriedAncientCity,
		"minecraft:mineshaft":        v2193.MapDecorationTypeBuriedMineshaft,
		"minecraft:desert_pyramid":   v2193.MapDecorationTypeDesertPyramid,
		"minecraft:ocean_ruin_warm":  v2193.MapDecorationTypeWarmOceanRuins,
	} {
		if got, ok := world.MapDecorationTypeFromJava(name); !ok || got != want {
			t.Errorf("%s: %d, %v; want %d", name, got, ok, want)
		}
	}
}
