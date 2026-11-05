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

// RegionRandomIn returns Entity.random like RegionRandom, but creates it with a
// seed drawn from the Level.random of the world of tx rather than from the UUID
// of the entity, which is random: the same world seed then gives the same
// entity streams. A stream that exists already is returned as it is.
func (e *EntityHandle) RegionRandomIn(tx *Tx) *vanilla.Random {
	if e.javaRandom == nil {
		random := vanilla.NewLegacyRandom(tx.RegionRandom().NextLong())
		e.javaRandom = &random
	}
	return e.javaRandom
}
