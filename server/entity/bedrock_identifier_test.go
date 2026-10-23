package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

// TestBedrockEntityIdentifiers checks the types whose registered names differ
// from their Bedrock identifiers save and spawn as BDS does, and that worlds
// holding either name load them.
func TestBedrockEntityIdentifiers(t *testing.T) {
	for name, bedrock := range map[string]string{
		"minecraft:villager":        "minecraft:villager_v2",
		"minecraft:zombie_villager": "minecraft:zombie_villager_v2",
		"minecraft:evoker":          "minecraft:evocation_illager",
		"minecraft:tropical_fish":   "minecraft:tropicalfish",
	} {
		if got := world.BedrockEntityIdentifier(name); got != bedrock {
			t.Errorf("BedrockEntityIdentifier(%s) = %s, want %s", name, got, bedrock)
		}
		registered, ok := entity.DefaultRegistry.Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if t2, ok := entity.DefaultRegistry.Lookup(bedrock); !ok || t2 != registered {
			t.Errorf("Lookup(%s) = %v, %v; want the %s type", bedrock, t2, ok, name)
		}
	}
	if got := world.BedrockEntityIdentifier("minecraft:zombie_pigman"); got != "minecraft:zombie_pigman" {
		t.Errorf("BedrockEntityIdentifier changed an identifier that matches: %s", got)
	}
}
