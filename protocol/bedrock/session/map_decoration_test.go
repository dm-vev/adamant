package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// TestMapDecorationTypesAreProtocolTypes checks the map decorations of
// exploration maps against protocol 2193: the session sends the type the
// world gives a decoration as it is.
func TestMapDecorationTypesAreProtocolTypes(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]byte{
		"minecraft:red_x":            mc.MapDecorationTypeCrossWhite,
		"minecraft:woodland_mansion": mc.MapDecorationTypeMansion,
		"minecraft:monument":         mc.MapDecorationTypeMonument,
		"minecraft:village_desert":   mc.MapDecorationTypeVillageDesert,
		"minecraft:village_plains":   mc.MapDecorationTypeVillagePlains,
		"minecraft:village_savanna":  mc.MapDecorationTypeVillageSavanna,
		"minecraft:village_snowy":    mc.MapDecorationTypeVillageSnowy,
		"minecraft:village_taiga":    mc.MapDecorationTypeVillageTaiga,
		"minecraft:jungle_temple":    mc.MapDecorationTypeJungleTemple,
		"minecraft:swamp_hut":        mc.MapDecorationTypeWitchHut,
		"minecraft:trial_chambers":   mc.MapDecorationTypeTrialChambers,
		"minecraft:abandoned_camp":   mc.MapDecorationTypeAbandonedCamp,
		"minecraft:ancient_city":     mc.MapDecorationTypeBuriedAncientCity,
		"minecraft:mineshaft":        mc.MapDecorationTypeBuriedMineshaft,
		"minecraft:desert_pyramid":   mc.MapDecorationTypeDesertPyramid,
		"minecraft:ocean_ruin_warm":  mc.MapDecorationTypeWarmOceanRuins,
	} {
		if got, ok := world.MapDecorationTypeFromJava(name); !ok || got != want {
			t.Errorf("%s: %d, %v; want %d", name, got, ok, want)
		}
	}
}
