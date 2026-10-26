package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/internal/nbtconv"
)

// smelterActorNBT writes a furnace, blast furnace or smoker actor as BDS
// does: BurnTime is the fuel left and BurnDuration how long the burning fuel
// lasts in all, both in ticks. The recipes cooked since a player last took
// the result are kept under adamant:java, and StoredXPInt holds what they
// are worth at least.
func smelterActorNBT(id string, s *smelter) map[string]any {
	remaining, maximum, cook := s.Durations()
	data := map[string]any{
		"BurnTime":     smelterTicks(remaining),
		"BurnDuration": smelterTicks(maximum),
		"CookTime":     smelterTicks(cook),
		"StoredXPInt":  int32(s.Experience()),
		"Items":        nbtconv.InvToNBT(s.inventory),
		"id":           id,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.recipesUsed) > 0 || s.storedXP > 0 {
		recipes := make(map[string]any, len(s.recipesUsed))
		for id, n := range s.recipesUsed {
			recipes[id] = int32(n)
		}
		data["adamant:java"] = map[string]any{"RecipesUsed": recipes, "StoredXP": int32(s.storedXP)}
	}
	return data
}

// decodeSmelter reads the inventory, fuel, cooking progress and cooked
// recipes of a smelter actor. Actors without the recipes, such as those BDS
// saved, keep their StoredXPInt.
func decodeSmelter(s *smelter, data map[string]any) {
	nbtconv.InvFromNBT(s.inventory, nbtconv.Slice(data, "Items"), data)
	remaining, maximum, cook := smelterActorDurations(data)
	s.setDurations(remaining, maximum, cook)
	s.mu.Lock()
	defer s.mu.Unlock()
	java, ok := data["adamant:java"].(map[string]any)
	if !ok {
		s.storedXP = smelterStoredXP(data)
		return
	}
	s.storedXP = int(nbtconv.Int32(java, "StoredXP"))
	recipes, _ := java["RecipesUsed"].(map[string]any)
	for id, n := range recipes {
		if count, ok := n.(int32); ok && count > 0 {
			s.recipesUsed[id] = int(count)
		}
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
	const tick = time.Millisecond * 50
	return time.Duration(burn) * tick, time.Duration(full) * tick, time.Duration(cooked) * tick
}

// smelterBurnDurationTicks scales the fuel ticks left into the burn duration
// furnace viewers are sent, relative to the cooking time.
func smelterBurnDurationTicks(remaining, maximum, requirement int) int {
	if remaining <= 0 || maximum <= 0 || requirement <= 0 {
		return 0
	}
	return (remaining*requirement + maximum - 1) / maximum
}

// smelterTicks converts a duration to furnace ticks using 50ms increments.
func smelterTicks(duration time.Duration) int16 {
	return int16(duration.Milliseconds() / 50)
}
