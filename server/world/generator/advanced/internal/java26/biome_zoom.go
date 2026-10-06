package java26

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// BiomeZoomSeed is BiomeManager.obfuscateSeed: SHA-256 of a little-endian long.
func BiomeZoomSeed(seed int64) int64 {
	var bytes [8]byte
	binary.LittleEndian.PutUint64(bytes[:], uint64(seed))
	digest := sha256.Sum256(bytes[:])
	return int64(binary.LittleEndian.Uint64(digest[:8]))
}

// FiddledBiomeQuart resolves a block position to the Java BiomeManager's
// nearest jittered quart-cell centre. Its seed must already be obfuscated.
func FiddledBiomeQuart(seed int64, x, y, z int32) (quartX, quartY, quartZ int32) {
	x, y, z = x-2, y-2, z-2
	parentX, parentY, parentZ := x>>2, y>>2, z>>2
	fractionX, fractionY, fractionZ := float64(x&3)*0.25, float64(y&3)*0.25, float64(z&3)*0.25
	best, distance := 0, math.Inf(1)
	for corner := range 8 {
		dx, dy, dz := int32(corner>>2), int32((corner>>1)&1), int32(corner&1)
		fiddle := biomeFiddleAt(seed, parentX+dx, parentY+dy, parentZ+dz)
		next := biomeFiddleDistance(fractionX-float64(dx), fractionY-float64(dy), fractionZ-float64(dz), fiddle)
		if next < distance {
			best, distance = corner, next
		}
	}
	return parentX + int32(best>>2), parentY + int32((best>>1)&1), parentZ + int32(best&1)
}

func biomeFiddleAt(seed int64, x, y, z int32) [3]float64 {
	value := seed
	for range 2 {
		value = biomeZoomStep(value, int64(x))
		value = biomeZoomStep(value, int64(y))
		value = biomeZoomStep(value, int64(z))
	}
	fx := (float64((value>>24)&1023)/1024 - 0.5) * 0.9
	value = biomeZoomStep(value, seed)
	fy := (float64((value>>24)&1023)/1024 - 0.5) * 0.9
	value = biomeZoomStep(value, seed)
	fz := (float64((value>>24)&1023)/1024 - 0.5) * 0.9
	return [3]float64{fx, fy, fz}
}

func biomeZoomStep(value, salt int64) int64 {
	return value*(value*6364136223846793005+1442695040888963407) + salt
}

func biomeFiddleDistance(x, y, z float64, fiddle [3]float64) float64 {
	x, y, z = x+fiddle[0], y+fiddle[1], z+fiddle[2]
	return z*z + y*y + x*x
}
