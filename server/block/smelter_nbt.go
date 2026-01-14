package block

import "time"

// smelterNBTMaxTimeTicks resolves the maximum fuel duration from NBT tags.
// Expected behavior is to prefer MaxTime and only fall back when older saves lack it.
// The old format stored the max fuel time in BurnDuration, which otherwise is UI-scaled.
func smelterNBTMaxTimeTicks(burnTimeTicks, maxTimeTicks, burnDurationTicks, requirementTicks int16) int16 {
	if maxTimeTicks > 0 {
		return maxTimeTicks
	}
	if burnDurationTicks > requirementTicks {
		return burnDurationTicks
	}
	if burnTimeTicks > 0 {
		return burnTimeTicks
	}
	return 0
}

// smelterBurnDurationTicks scales remaining fuel into the client-facing burn duration.
// The client-visible value is expected to be scaled to the per-item cook requirement, not the full fuel time.
// Writing full fuel ticks previously caused incorrect fuel bars after reload or chunk send.
func smelterBurnDurationTicks(remaining, maximum, requirement time.Duration) int16 {
	remainingTicks := smelterTicks(remaining)
	if remainingTicks <= 0 {
		return 0
	}
	maximumTicks := smelterTicks(maximum)
	if maximumTicks <= 0 {
		return 0
	}
	requirementTicks := smelterTicks(requirement)
	if requirementTicks <= 0 {
		return 0
	}
	scaled := (int64(remainingTicks)*int64(requirementTicks) + int64(maximumTicks) - 1) / int64(maximumTicks)
	if scaled <= 0 {
		return 0
	}
	return int16(scaled)
}

// smelterTicks converts a duration to furnace ticks using 50ms increments.
func smelterTicks(duration time.Duration) int16 {
	return int16(duration.Milliseconds() / 50)
}
