package world

import "github.com/df-mc/dragonfly/server/block/cube"

// LocalDifficulty is the difficulty of a specific position in a World. Effective
// includes the effects of the Overworld clock, chunk inhabited time, and moon phase.
type LocalDifficulty struct {
	Base      Difficulty
	Effective float32
}

// SpecialMultiplier returns the multiplier used by difficulty-scaled special
// hostile mob behaviour.
func (d LocalDifficulty) SpecialMultiplier() float32 {
	if d.Effective < 2 {
		return 0
	}
	if d.Effective > 4 {
		return 1
	}
	return (d.Effective - 2) / 2
}

// LocalDifficulty returns the difficulty at pos, using the world clock and the
// inhabited time stored on the position's chunk.
func (tx *Tx) LocalDifficulty(pos cube.Pos) LocalDifficulty {
	w := tx.World()
	col := tx.chunk(chunkPosFromBlockPos(pos))
	w.set.Lock()
	clockTime, base := w.set.Time, w.set.Difficulty
	w.set.Unlock()
	return LocalDifficulty{
		Base:      base,
		Effective: calculateEffectiveDifficulty(base, clockTime, col.InhabitedTime, moonBrightness(clockTime)),
	}
}

func calculateEffectiveDifficulty(base Difficulty, totalGameTime int64, localGameTime uint64, moonBrightness float32) float32 {
	if base == DifficultyPeaceful {
		return 0
	}
	baseID, ok := DifficultyID(base)
	if !ok {
		baseID = 2
	}
	isHard := base == DifficultyHard
	scale := float32(0.75)
	globalScale := clampDifficulty((float32(totalGameTime)-72000)/1440000, 0, 1) * 0.25
	scale += globalScale
	localScale := clampDifficulty(float32(localGameTime)/3600000, 0, 1) * float32(0.75)
	if isHard {
		localScale = clampDifficulty(float32(localGameTime)/3600000, 0, 1)
	}
	localScale += clampDifficulty(moonBrightness*0.25, 0, globalScale)
	if base == DifficultyEasy {
		localScale *= 0.5
	}
	scale += localScale
	return float32(baseID) * scale
}

func clampDifficulty(v, min, max float32) float32 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func moonBrightness(dayTime int64) float32 {
	const dayLength = int64(24000)
	phase := dayTime / dayLength
	if dayTime < 0 && dayTime%dayLength != 0 {
		phase--
	}
	phase %= 8
	if phase < 0 {
		phase += 8
	}
	return [...]float32{1, .75, .5, .25, 0, .25, .5, .75}[phase]
}
