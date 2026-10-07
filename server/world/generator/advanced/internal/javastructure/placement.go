package javastructure

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/java26"
)

// SpreadType is a Java RandomSpreadStructurePlacement distribution.
type SpreadType uint8

const (
	Linear SpreadType = iota
	Triangular
)

// FrequencyReduction is Java AbstractSpreadingStructurePlacement's frequency method.
type FrequencyReduction uint8

const (
	FrequencyDefault FrequencyReduction = iota
	FrequencyLegacyType1
	FrequencyLegacyType2
	FrequencyLegacyType3
)

// FrequencyApplies implements Java structure-placement frequency reduction.
func FrequencyApplies(seed int64, salt, sourceX, sourceZ int32, probability float32, method FrequencyReduction) bool {
	if probability <= 0 { return false }
	if probability >= 1 { return true }
	switch method {
	case FrequencyDefault:
		r := java26.NewLegacyRandom(largeFeatureSeed(seed,salt,sourceX,sourceZ))
		return r.NextFloat() < probability
	case FrequencyLegacyType1:
		cx, cz := sourceX>>4, sourceZ>>4
		r := java26.NewLegacyRandom(seed ^ int64(cx) ^ int64(cz<<4))
		r.NextInt32()
		bound := int32(float32(1) / probability)
		return bound > 0 && r.NextInt(bound) == 0
	case FrequencyLegacyType2:
		r := java26.NewLegacyRandom(largeFeatureSeed(seed,sourceX,sourceZ,10387320))
		return r.NextFloat() < probability
	case FrequencyLegacyType3:
		r := java26.NewLegacyRandom(seed + int64(sourceX)*341873128712 + int64(sourceZ)*132897987541)
		return r.NextDouble() < float64(probability)
	default:
		return false
	}
}

// PotentialChunk computes Java RandomSpreadStructurePlacement.getPotentialStructureChunk.
// source chunk coordinates are mapped to spacing-sized regions; spacing and separation come from structure_set data.
func PotentialChunk(seed int64, sourceX, sourceZ, salt, spacing, separation int32, distribution SpreadType) ([2]int32, error) {
	if spacing <= 0 || separation < 0 || separation >= spacing { return [2]int32{}, fmt.Errorf("invalid Java random-spread spacing=%d separation=%d", spacing, separation) }
	if distribution > Triangular { return [2]int32{}, fmt.Errorf("invalid Java random-spread type %d", distribution) }
	regionX, regionZ := floorDiv(sourceX, spacing), floorDiv(sourceZ, spacing)
	random := java26.NewLegacyRandom(largeFeatureSeed(seed,regionX,regionZ,salt))
	limit := spacing - separation
	sample := func() int32 {
		a := random.NextInt(limit)
		if distribution == Triangular { return (a + random.NextInt(limit)) / 2 }
		return a
	}
	return [2]int32{regionX*spacing + sample(), regionZ*spacing + sample()}, nil
}

func largeFeatureSeed(seed int64, x,z,salt int32) int64 { return seed + int64(x)*341873128712 + int64(z)*132897987541 + int64(salt) }
func floorDiv(value, divisor int32) int32 {
	q, r := value/divisor, value%divisor
	if r != 0 && (r < 0) != (divisor < 0) { q-- }
	return q
}
