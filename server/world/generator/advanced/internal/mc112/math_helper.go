package mc112

import "math"

const (
	sinTableSize       = 65536
	sinTableIndexScale = float32(sinTableSize) / (2 * math.Pi) // 10430.378...
)

var sinTable [sinTableSize]float32

func init() {
	for i := 0; i < sinTableSize; i++ {
		sinTable[i] = float32(math.Sin(float64(i) * 2 * math.Pi / sinTableSize))
	}
}

// Sin returns MathHelper.sin(value) from Minecraft Java Edition 1.12.
func Sin(value float32) float32 {
	return sinTable[int(value*sinTableIndexScale)&(sinTableSize-1)]
}

// Cos returns MathHelper.cos(value) from Minecraft Java Edition 1.12.
func Cos(value float32) float32 {
	return sinTable[int(value*sinTableIndexScale+sinTableSize/4)&(sinTableSize-1)]
}

// Floor matches MathHelper.floor for double values.
func Floor(value float64) int {
	i := int(value)
	if value < float64(i) {
		return i - 1
	}
	return i
}
