package server

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestDefaultGameDataIncludesSharedFallDamageRule(t *testing.T) {
	settings := &world.Settings{DefaultGameMode: world.GameModeSurvival, FallDamage: true}
	provider := world.NopProvider{Set: settings}
	overworld := world.Config{Synchronous: true, Provider: provider}.New()
	nether := world.Config{Synchronous: true, Provider: provider, Dim: world.Nether}.New()
	defer overworld.Close()
	defer nether.Close()

	nether.SetFallDamage(false)
	srv := &Server{world: overworld}
	data := srv.defaultGameData()

	found := false
	for _, rule := range data.GameRules {
		if rule.Name == "falldamage" {
			found = true
			if enabled, ok := rule.Value.(bool); !ok || enabled {
				t.Fatalf("falldamage value = %#v, want false", rule.Value)
			}
		}
	}
	if !found {
		t.Fatal("falldamage missing from initial game rules")
	}
}
