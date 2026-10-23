package overworld

import (
	"reflect"
	"testing"
)

func TestNoisePlacementCountsMatchReference(t *testing.T) {
	// Captured from executed Java 26.3 NoiseBasedCountPlacement(128,200,0)
	// and NoiseThresholdCountPlacement(0,1,3). Negative counts emit no positions.
	positions := []featurePos{
		{0, 64, 0}, {16, 64, -8}, {32, 64, -16}, {64, 64, -32},
		{128, 64, -64}, {-16, 64, 8}, {1024, 64, 1024}, {-100000, 64, 123456},
	}
	for _, tc := range []struct {
		name     string
		modifier map[string]any
		counts   []int
	}{
		{
			"noise_based_count",
			map[string]any{"type": "noise_based_count", "noise_factor": float64(200), "noise_to_count_ratio": float64(128), "noise_offset": float64(0)},
			[]int{0, 43, 69, 30, 0, 0, 39, 111},
		},
		{
			"noise_threshold_count",
			map[string]any{"type": "noise_threshold_count", "noise_level": float64(0), "below_noise": float64(1), "above_noise": float64(3)},
			[]int{3, 3, 3, 3, 1, 1, 3, 3},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var want, got []featurePos
			f := &ModernFeatures{}
			for _, p := range positions {
				got = f.modifyPlacement(nil, p, tc.modifier, nil, "", got)
			}
			for i, count := range tc.counts {
				for range count {
					want = append(want, positions[i])
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("placement positions differ from executed Java counts %v", tc.counts)
			}
		})
	}
}
