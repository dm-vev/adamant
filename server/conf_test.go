package server

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
	"github.com/df-mc/dragonfly/server/world/generator/vanilla"
)

func TestConfiguredGeneratorProviderDefaultsToAdvanced(t *testing.T) {
	world_finaliseBlockRegistry()

	provider := configuredGeneratorProvider("", 0, nil)
	if _, ok := provider(world.Overworld).(*overworld.Overworld); !ok {
		t.Fatal("expected empty generator config to select advanced overworld generator")
	}
}

func TestConfiguredGeneratorProviderSelectsVanilla(t *testing.T) {
	world_finaliseBlockRegistry()

	provider := configuredGeneratorProvider("vanilla", 0, nil)
	if _, ok := provider(world.Overworld).(vanilla.Generator); !ok {
		t.Fatal("expected vanilla generator config to select vanilla overworld generator")
	}
}
