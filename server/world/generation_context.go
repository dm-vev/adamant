package world

import "time"

// GenerationContext captures the live server inputs used by WorldGenRegion.
// Workers receive a value snapshot and never access mutable World settings.
// Generated chunks have no inhabited-time contribution to local difficulty.
type GenerationContext struct {
	Difficulty LocalDifficulty
	Halloween  bool
	GameTime   int64
	// MoonBrightness is the Overworld lunar timeline value, independent of
	// Difficulty.SpecialMultiplier (which may be zero at any moon phase).
	MoonBrightness float32
}

func (w *World) generationContext() GenerationContext {
	w.set.Lock()
	clockTime, gameTime, base := w.set.Time, w.set.CurrentTick, w.set.Difficulty
	w.set.Unlock()
	now := time.Now()
	return GenerationContext{
		Difficulty: LocalDifficulty{Base: base, Effective: calculateEffectiveDifficulty(base, clockTime, 0, moonBrightness(clockTime))},
		Halloween:  now.Month() == time.October && now.Day() == 31,
		GameTime:   gameTime,
		MoonBrightness: moonBrightness(clockTime),
	}
}
