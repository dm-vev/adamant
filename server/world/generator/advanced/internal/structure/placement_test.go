package structure

import "testing"

// TestLegacyType3FrequencyMatchesJavaMineshafts checks the legacy_type_3
// frequency reduction of the mineshaft structure set against the starts the
// Java 26.3 dedicated server generated for two seeds, read from the
// structure references of its saved chunks. The method seeds its random
// with setLargeFeatureSeed rather than the salted large feature seed.
func TestLegacyType3FrequencyMatchesJavaMineshafts(t *testing.T) {
	starts := []struct {
		seed int64
		x, z int32
	}{
		{12345, 4, -1}, {12345, 103, 99}, {12345, 299, 10},
		{-7406315911040434051, -1006, -1494}, {-7406315911040434051, -1000, -1498},
		{-7406315911040434051, -398, 256}, {-7406315911040434051, 695, 694},
		{-7406315911040434051, 1495, -305},
	}
	for _, s := range starts {
		if !FrequencyApplies(s.seed, 0, s.x, s.z, 0.004, FrequencyLegacyType3) {
			t.Errorf("seed %d: no mineshaft start at chunk %d,%d, where Java has one", s.seed, s.x, s.z)
		}
	}
	// About one chunk in 250 passes.
	n := 0
	for x := int32(-100); x < 100; x++ {
		for z := int32(-100); z < 100; z++ {
			if FrequencyApplies(12345, 0, x, z, 0.004, FrequencyLegacyType3) {
				n++
			}
		}
	}
	if n < 120 || n > 200 {
		t.Fatalf("%d of 40000 chunks pass a frequency of 0.004", n)
	}
}
