package bedrock

import (
	_ "embed"
	"encoding/binary"
)

// climate_data.bin is the native-built tree: fixed 52-byte node records.
// Each record is six little-endian [min,max] int16 pairs, six int32 child
// indexes, a uint16 source-row leaf ID (0xffff for an internal node), a child
// count, and a biome-name index. Leaves occupy the initial source-row indexes;
// internal nodes follow, with the root last. Axis seven is fixed at zero.
//
//go:embed climate_data.bin
var climateTreeData []byte

var climateRootIndex = int32(len(climateTreeData)/climateNodeSize - 1)

var climateBiomeNames = [...]string{
	"minecraft:badlands", "minecraft:bamboo_jungle", "minecraft:beach", "minecraft:birch_forest", "minecraft:cherry_grove", "minecraft:cold_ocean", "minecraft:dappled_forest", "minecraft:dark_forest", "minecraft:deep_cold_ocean", "minecraft:deep_dark", "minecraft:deep_frozen_ocean", "minecraft:deep_lukewarm_ocean", "minecraft:deep_ocean", "minecraft:desert", "minecraft:dripstone_caves", "minecraft:eroded_badlands", "minecraft:flower_forest", "minecraft:forest", "minecraft:frozen_ocean", "minecraft:frozen_peaks", "minecraft:frozen_river", "minecraft:grove", "minecraft:ice_spikes", "minecraft:jagged_peaks", "minecraft:jungle", "minecraft:lukewarm_ocean", "minecraft:lush_caves", "minecraft:mangrove_swamp", "minecraft:meadow", "minecraft:mushroom_fields", "minecraft:ocean", "minecraft:old_growth_birch_forest", "minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga", "minecraft:pale_garden", "minecraft:plains", "minecraft:river", "minecraft:savanna", "minecraft:savanna_plateau", "minecraft:snowy_beach", "minecraft:snowy_plains", "minecraft:snowy_slopes", "minecraft:snowy_taiga", "minecraft:sparse_jungle", "minecraft:stony_peaks", "minecraft:stony_shore", "minecraft:sulfur_caves", "minecraft:sunflower_plains", "minecraft:swamp", "minecraft:taiga", "minecraft:warm_ocean", "minecraft:windswept_forest", "minecraft:windswept_gravelly_hills", "minecraft:windswept_hills", "minecraft:windswept_savanna", "minecraft:wooded_badlands",
}

const climateNodeSize = 52
const climateLeafOffset = 48
const climateChildOffset = 24
const climateCountOffset = 50
const climateBiomeOffset = 51
const climateInternalLeaf = 0xffff

type ClimatePoint [6]int64

// ClimateLookup is the per-column previous winner. Each column requires its
// own lookup; all lookups share the immutable packed tree. Its zero value has
// no previous winner and assignment from ClimateLookup{} resets tie history.
type ClimateLookup struct {
	previous uint16 // one-based row ID; zero means no previous leaf
}

// Biome returns the nearest Java canonical biome resource name. point is read
// synchronously and never retained. Equal fitness leaves do not replace the
// previous winner, matching native search's strict-less-than update.
func (lookup *ClimateLookup) Biome(point *ClimatePoint) string {
	best := int32(-1)
	var bestDistance int64
	if lookup.previous != 0 {
		best = int32(lookup.previous - 1)
		bestDistance = climateDistance(best, point)
	} else {
		bestDistance = int64(^uint64(0) >> 1)
	}
	climateSearch(climateRootIndex, point, &best, &bestDistance)
	lookup.previous = uint16(best + 1)
	biomeID := climateTreeData[int(best)*climateNodeSize+climateBiomeOffset]
	return climateBiomeNames[biomeID]
}

func climateSearch(nodeIndex int32, point *ClimatePoint, best *int32, bestDistance *int64) {
	base := int(nodeIndex) * climateNodeSize
	distance := climateDistanceAt(base, point)
	if distance >= *bestDistance {
		return
	}
	leafID := binary.LittleEndian.Uint16(climateTreeData[base+climateLeafOffset:])
	if leafID != climateInternalLeaf {
		*best = int32(leafID)
		*bestDistance = distance
		return
	}
	count := climateTreeData[base+climateCountOffset]
	for child := range count {
		index := binary.LittleEndian.Uint32(climateTreeData[base+climateChildOffset+int(child)*4:])
		climateSearch(int32(index), point, best, bestDistance)
	}
}

func climateDistance(leafID int32, point *ClimatePoint) int64 {
	return climateDistanceAt(int(leafID)*climateNodeSize, point)
}

func climateDistanceAt(base int, point *ClimatePoint) int64 {
	var total int64
	for dimension, value := range point {
		pair := base + dimension*4
		minimum := int64(int16(binary.LittleEndian.Uint16(climateTreeData[pair:])))
		maximum := int64(int16(binary.LittleEndian.Uint16(climateTreeData[pair+2:])))
		delta := int64(0)
		if value < minimum {
			delta = minimum - value
		} else if value > maximum {
			delta = value - maximum
		}
		total += delta * delta
	}
	return total
}
