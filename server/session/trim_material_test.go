package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	nbtext "github.com/dm-vev/adamant/net/bedrock/text"
)

// TestTrimMaterialColoursMatchProtocol checks that TrimData names the colour
// of every trim material with the § code the protocol's text package has for
// the material, as it did when the items returned those codes themselves.
func TestTrimMaterialColoursMatchProtocol(t *testing.T) {
	want := map[string]string{
		"amethyst":  nbtext.Amethyst,
		"copper":    nbtext.Copper,
		"diamond":   nbtext.Diamond,
		"emerald":   nbtext.Emerald,
		"gold":      nbtext.Gold,
		"iron":      nbtext.Iron,
		"lapis":     nbtext.Lapis,
		"netherite": nbtext.Netherite,
		"quartz":    nbtext.Quartz,
		"redstone":  nbtext.Redstone,
		"resin":     nbtext.Resin,
	}
	seen := map[string]bool{}
	for _, i := range item.ArmourTrimMaterials() {
		material, ok := i.(item.ArmourTrimMaterial)
		if !ok {
			continue
		}
		name := material.TrimMaterial()
		seen[name] = true
		code, known := want[name]
		if !known {
			t.Errorf("trim material %q has no expected colour", name)
			continue
		}
		if got := trimMaterialColour(material); got != code {
			t.Errorf("trim material %q has colour %q, want %q", name, got, code)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("trim material %q is missing from ArmourTrimMaterials", name)
		}
	}
	if got := trimMaterialColour(block.RedstoneWire{}); got != nbtext.Redstone {
		t.Errorf("redstone wire block has colour %q, want %q", got, nbtext.Redstone)
	}
}
