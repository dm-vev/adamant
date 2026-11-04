package builtin

import (
	"slices"
	"testing"
)

// TestParticleName holds the names /particle takes to the names it shows
// particles by: Java's particle types with their namespace, other
// namespaced names as they are, and nothing for other names.
func TestParticleName(t *testing.T) {
	for input, want := range map[string]string{
		"heart":                          "minecraft:heart",
		"minecraft:flame":                "minecraft:flame",
		"flame":                          "minecraft:flame",
		"minecraft:basic_flame_particle": "minecraft:basic_flame_particle",
		"custom:behaviour_particle":      "custom:behaviour_particle",
		"namespace:name":                 "namespace:name",
		"not_a_particle":                 "",
		"":                               "",
	} {
		if got := particleName(input); got != want {
			t.Errorf("particleName(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestJavaParticleNames holds the particle types /particle takes by name to
// those of the table the adapter maps them by, which held them before.
func TestJavaParticleNames(t *testing.T) {
	want := []string{
		"angry_villager", "cloud", "end_rod", "explosion", "explosion_emitter", "flame", "happy_villager",
		"heart", "large_smoke", "poof", "portal", "smoke", "soul_fire_flame", "totem_of_undying",
	}
	if !slices.Equal(javaParticleNames, want) {
		t.Errorf("javaParticleNames = %v, want %v", javaParticleNames, want)
	}
}
