package session_test

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
)

// customBiome is a biome the game does not have, registered by
// TestBiomeDefinitionsMatchWorld.
type customBiome struct{ biome.Plains }

func (customBiome) EncodeBiome() int { return 300 }
func (customBiome) String() string   { return "adamant:custom_plains" }
func (customBiome) Tags() []string   { return []string{"plains", "adamant:custom"} }

// TestBiomeDefinitionsMatchWorld checks the session defines the biomes to
// clients as world.BiomeDefinitions does, for the vanilla biomes and with a
// biome registered that is not one of the game's own. It runs in a process
// of its own, which the biome it registers stays in.
func TestBiomeDefinitionsMatchWorld(t *testing.T) {
	if isolated(t) {
		return
	}
	compare := func(name string) {
		t.Helper()
		got, gotStrings := session.BiomeDefinitions()
		want, wantStrings := world.BiomeDefinitions()
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(gotStrings, wantStrings) {
			t.Fatalf("%s: the session defines the biomes as\n%+v\n%q\nthe world as\n%+v\n%q", name, got, gotStrings, want, wantStrings)
		}
	}
	compare("vanilla biomes")
	vanilla, _ := session.BiomeDefinitions()

	world.RegisterBiome(customBiome{})
	compare("with a custom biome")
	defs, _ := session.BiomeDefinitions()
	if len(defs) != len(vanilla)+1 || defs[len(defs)-1].BiomeID != 300 {
		t.Fatalf("the custom biome is not defined last with its ID: %+v", defs[len(defs)-1])
	}
}
