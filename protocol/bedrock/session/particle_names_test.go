package session

import (
	"strings"
	"testing"
)

// oldBedrockParticleIdentifier is bedrockParticleIdentifier of
// server/cmd/builtin as /particle mapped its names before the game named
// particles by their Java names, frozen.
func oldBedrockParticleIdentifier(name string) string {
	if id, ok := oldJavaParticleIDs[strings.TrimPrefix(name, "minecraft:")]; ok {
		return id
	}
	if strings.Contains(name, ":") {
		return name
	}
	return ""
}

var oldJavaParticleIDs = map[string]string{
	"poof": "minecraft:basic_smoke_particle", "cloud": "minecraft:basic_smoke_particle",
	"explosion": "minecraft:huge_explosion_lab_misc_emitter", "explosion_emitter": "minecraft:huge_explosion_lab_misc_emitter",
	"portal": "minecraft:portal_directional", "flame": "minecraft:basic_flame_particle",
	"heart": "minecraft:heart_particle", "smoke": "minecraft:basic_smoke_particle",
	"large_smoke": "minecraft:basic_smoke_particle", "happy_villager": "minecraft:villager_happy",
	"angry_villager": "minecraft:villager_angry", "end_rod": "minecraft:endrod",
	"totem_of_undying": "minecraft:totem_particle", "soul_fire_flame": "minecraft:soul_particle",
}

// TestNamedParticlesKeepTheirIdentifiers holds the Bedrock particle effect
// a name of /particle shows to the one it showed before. The game names a
// particle type of Java, short or not, by minecraft: and its short name and
// passes other namespaced names on (TestParticleName in builtin); the
// adapter must then send what the old function gave for each.
func TestNamedParticlesKeepTheirIdentifiers(t *testing.T) {
	if len(bedrockParticleNames) != len(oldJavaParticleIDs) {
		t.Errorf("%d named particles, want %d", len(bedrockParticleNames), len(oldJavaParticleIDs))
	}
	for short, want := range oldJavaParticleIDs {
		for _, input := range []string{short, "minecraft:" + short} {
			if old := oldBedrockParticleIdentifier(input); old != want {
				t.Fatalf("frozen oracle gives %q for %q, want %q", old, input, want)
			}
		}
		if got := bedrockParticleName("minecraft:" + short); got != want {
			t.Errorf("minecraft:%s sends %q, want %q", short, got, want)
		}
	}
	// Names the game passes on as they are: raw Bedrock identifiers,
	// those of other namespaces and doubled namespaces.
	for _, name := range []string{
		"minecraft:basic_flame_particle", "minecraft:heart_particle", "minecraft:villager_happy",
		"foo:bar", "custom:behaviour_particle", "minecraft:minecraft:flame", "flame:minecraft",
	} {
		if got, want := bedrockParticleName(name), oldBedrockParticleIdentifier(name); got != want {
			t.Errorf("%s sends %q, the old function gave %q", name, got, want)
		}
	}
}
