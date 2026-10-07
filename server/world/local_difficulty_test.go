package world

import "testing"

func TestEffectiveDifficultyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name string
		base Difficulty
		time int64
		local uint64
		moon float32
		want float32
	}{
		{"peaceful ignores modifiers", DifficultyPeaceful, 10_000_000, 10_000_000, 1, 0},
		{"global offset start", DifficultyNormal, 72_000, 0, 1, 1.5},
		{"global cap", DifficultyNormal, 1_512_000, 0, 1, 2.5},
		{"normal local and moon caps", DifficultyNormal, 1_512_000, 3_600_000, 1, 4},
		{"easy halves local modifiers", DifficultyEasy, 1_512_000, 3_600_000, 1, 1.5},
		{"hard local scale", DifficultyHard, 1_512_000, 3_600_000, 1, 6.75},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := calculateEffectiveDifficulty(tc.base, tc.time, tc.local, tc.moon); got != tc.want {
				t.Fatalf("effective difficulty = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMoonBrightnessPhases(t *testing.T) {
	want := [...]float32{1, .75, .5, .25, 0, .25, .5, .75}
	for phase, brightness := range want {
		if got := moonBrightness(int64(phase) * 24_000); got != brightness {
			t.Errorf("phase %d brightness = %v, want %v", phase, got, brightness)
		}
	}
	if got := moonBrightness(-1); got != .75 {
		t.Fatalf("brightness before epoch = %v, want .75", got)
	}
}

func TestSpecialMultiplierBoundaries(t *testing.T) {
	for _, tc := range []struct {
		effective, want float32
	}{
		{1.999, 0},
		{2, 0},
		{3, .5},
		{4, 1},
		{4.001, 1},
	} {
		if got := (LocalDifficulty{Effective: tc.effective}).SpecialMultiplier(); got != tc.want {
			t.Errorf("special multiplier at %v = %v, want %v", tc.effective, got, tc.want)
		}
	}
}
