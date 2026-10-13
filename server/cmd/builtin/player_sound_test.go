package builtin

import "testing"

func TestBedrockParticleIdentifier(t *testing.T) {
	for input, want := range map[string]string{
		"heart":                     "minecraft:heart_particle",
		"minecraft:flame":           "minecraft:basic_flame_particle",
		"custom:behaviour_particle": "custom:behaviour_particle",
		"namespace:name":            "namespace:name",
	} {
		if got := bedrockParticleIdentifier(input); got != want {
			t.Errorf("bedrockParticleIdentifier(%q) = %q, want %q", input, got, want)
		}
	}
	if got := bedrockParticleIdentifier("not_a_particle"); got != "" {
		t.Errorf("unknown particle mapping = %q, want empty", got)
	}
}
