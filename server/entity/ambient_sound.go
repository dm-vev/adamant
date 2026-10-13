package entity

import "github.com/df-mc/dragonfly/server/internal/java26"

// ambientSoundTimer ports Mob's ambient sound clock: every tick a mob makes
// its ambient sound with a chance of time/1000, where time grows by one each
// tick and restarts at minus the sound interval after each sound.
type ambientSoundTimer struct{ time int }

// tick advances the clock and reports whether the mob makes its sound.
func (a *ambientSoundTimer) tick(random *java26.Random, interval int) bool {
	play := int(random.NextInt(1000)) < a.time
	a.time++
	if play {
		a.reset(interval)
	}
	return play
}

// reset ports Mob.resetAmbientSoundTime.
func (a *ambientSoundTimer) reset(interval int) { a.time = -interval }

// voicePitch ports LivingEntity.getVoicePitch for an adult.
func voicePitch(random *java26.Random) float64 {
	return float64((random.NextFloat()-random.NextFloat())*.2 + 1)
}
