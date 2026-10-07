package java26

import "math"

const mthSinScale = 10430.378350470453

var mthSinTable [65536]float32

func init() {
	for i := range mthSinTable {
		mthSinTable[i] = float32(math.Sin(float64(i) / mthSinScale))
	}
}

// Sin implements Java 26.3 Mth.sin, including its double-precision index and
// float-valued lookup table. Callers must preserve source argument precision.
func Sin(angle float64) float32 {
	return mthSinTable[mthIndex(angle*mthSinScale)]
}

// Cos implements Java 26.3 Mth.cos using the quarter-period table offset.
func Cos(angle float64) float32 {
	return mthSinTable[mthIndex(angle*mthSinScale+16384)]
}

func mthIndex(value float64) uint16 {
	// Java's double-to-long narrowing saturates infinities and out-of-range
	// values, and maps NaN to zero, before the low sixteen bits are selected.
	if math.IsNaN(value) || value <= -0x1p63 {
		return 0
	}
	if value >= 0x1p63 {
		return 65535
	}
	return uint16(int64(value))
}
