package bedrock

import "testing"

func TestLegacyRandomMatchesNativeMT19937(t *testing.T) {
	var random LegacyRandom
	random.Seed(5489)
	want := [...]uint32{3499211612, 581869302, 3890346734, 3586334585, 545404204, 4161255391, 3922919429, 949333985}
	for i, expected := range want {
		if actual := random.NextUint32(); actual != expected {
			t.Fatalf("word %d = %d, want native value %d", i, actual, expected)
		}
	}
}
