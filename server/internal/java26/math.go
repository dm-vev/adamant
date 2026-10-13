package java26

import "math"

const mthSinScale = 10430.378350470453

var mthSinTable [65536]float32

func init() {
	for i := range mthSinTable {
		mthSinTable[i] = float32(math.Sin(float64(i) / mthSinScale))
	}
	for i := range mthAtanTables[0] {
		angle := math.Asin(float64(i) / 256)
		mthAtanTables[0][i], mthAtanTables[1][i] = angle, math.Cos(angle)
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

var mthAtanTables [2][257]float64

// Atan2 implements Mth.atan2's lookup and inverse-square-root approximation.
func Atan2(y, x float64) float64 {
	squared := x*x + y*y
	if math.IsNaN(squared) {
		return math.NaN()
	}
	negativeY, negativeX := y < 0, x < 0
	if negativeY {
		y = -y
	}
	if negativeX {
		x = -x
	}
	steep := y > x
	if steep {
		x, y = y, x
	}
	half := .5 * squared
	bits := int64(6910469410427058090) - (int64(math.Float64bits(squared)) >> 1)
	inverse := math.Float64frombits(uint64(bits))
	inverse *= 1.5 - half*inverse*inverse
	x, y = x*inverse, y*inverse
	bias := math.Float64frombits(4805340802404319232)
	quantized := bias + y
	index := int(int32(math.Float64bits(quantized)))
	sine := quantized - bias
	delta := y*mthAtanTables[1][index] - x*sine
	angle := mthAtanTables[0][index] + (6+delta*delta)*delta*.16666666666666666
	if steep {
		angle = math.Pi/2 - angle
	}
	if negativeX {
		angle = math.Pi - angle
	}
	if negativeY {
		angle = -angle
	}
	return angle
}
