package world

import (
	"math/rand/v2"
	"sync"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// randomness is the random sources of the world.
type randomness struct {
	r                   *rand.Rand
	lootSequences       map[string]*vanilla.Random
	javaRandom          vanilla.Random
	javaRandomReady     bool
	nativeGaussianFloat nativeGaussianFloatState
	// weatherRand is used for out-of-band weather changes to avoid racing the
	// main world RNG, which is owned by the tick loop.
	weatherRandMu sync.Mutex
	weatherRand   *rand.Rand
}
