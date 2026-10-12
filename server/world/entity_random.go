package world

import (
	"encoding/binary"

	"github.com/df-mc/dragonfly/server/internal/java26"
)

// JavaRandom returns Entity.random, independently of the world's Level.random.
// The caller must own this handle (a world transaction or unpublished creation)
// and must not retain the stream across an ownership transfer. Like Java entity
// RNG, it is runtime state and is not part of saved entity NBT.
func (e *EntityHandle) JavaRandom() *java26.Random {
	if e.javaRandom == nil {
		seed := int64(binary.LittleEndian.Uint64(e.id[:8]) ^ binary.LittleEndian.Uint64(e.id[8:]))
		random := java26.NewLegacyRandom(seed)
		e.javaRandom = &random
	}
	return e.javaRandom
}
