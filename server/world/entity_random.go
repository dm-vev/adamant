package world

import (
	"encoding/binary"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// RegionRandom returns Entity.random, independently of the world's Level.random.
// The caller must own this handle (a world transaction or unpublished creation)
// and must not retain the stream across an ownership transfer. Like Java entity
// RNG, it is runtime state and is not part of saved entity NBT.
func (e *EntityHandle) RegionRandom() *vanilla.Random {
	if e.javaRandom == nil {
		seed := int64(binary.LittleEndian.Uint64(e.id[:8]) ^ binary.LittleEndian.Uint64(e.id[8:]))
		random := vanilla.NewLegacyRandom(seed)
		e.javaRandom = &random
	}
	return e.javaRandom
}
