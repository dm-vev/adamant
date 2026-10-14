package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/internal/nbtconv"
)

// smelterActorNBT writes a furnace, blast furnace or smoker actor as BDS
// does: BurnTime is the fuel left and BurnDuration how long the burning fuel
// lasts in all, both in ticks.
func smelterActorNBT(id string, s *smelter) map[string]any {
	remaining, maximum, cook := s.Durations()
	return map[string]any{
		"BurnTime":     smelterTicks(remaining),
		"BurnDuration": smelterTicks(maximum),
		"CookTime":     smelterTicks(cook),
		"StoredXPInt":  int32(s.Experience()),
		"Items":        nbtconv.InvToNBT(s.inventory),
		"id":           id,
	}
}

// smelterActorDurations reads the fuel left, the burning fuel's full
// duration and the cooking progress from a smelter actor. Worlds saved by
// earlier versions kept the full duration in MaxTime, with BurnDuration
// scaled to the cooking time.
func smelterActorDurations(data map[string]any) (remaining, maximum, cook time.Duration) {
	burn := max(nbtconv.Int16(data, "BurnTime"), 0)
	cooked := max(nbtconv.Int16(data, "CookTime"), 0)
	full := max(nbtconv.Int16(data, "BurnDuration"), 0)
	if legacy := nbtconv.Int16(data, "MaxTime"); legacy > 0 {
		full = legacy
	}
	full = max(full, burn)
	if burn == 0 {
		cooked = 0
	}
	const tick = time.Millisecond * 50
	return time.Duration(burn) * tick, time.Duration(full) * tick, time.Duration(cooked) * tick
}

// smelterBurnDurationTicks scales remaining fuel into the burn duration
// furnace viewers are sent, relative to the per-item cooking time.
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
