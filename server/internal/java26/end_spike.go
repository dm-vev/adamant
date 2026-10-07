package java26

import "math"

// EndSpike is the seed-dependent EndSpikeFeature geometry shared by terrain
// decoration and the dragon respawn ritual.
type EndSpike struct {
	X, Z, Radius, Height int
	Guarded bool
}

// EndSpikes follows EndSpikeFeature's low-16-bit legacy seed and size shuffle.
func EndSpikes(seed int64) (spikes [10]EndSpike) {
	random := NewLegacyRandom(seed)
	random = NewLegacyRandom(random.NextLong() & 65535)
	var sizes [10]int
	for i := range sizes {
		sizes[i] = i
	}
	for n := len(sizes); n > 1; n-- {
		j := int(random.NextInt(int32(n)))
		sizes[n-1], sizes[j] = sizes[j], sizes[n-1]
	}
	for i, size := range sizes {
		angle := 2 * (-math.Pi + (math.Pi/10)*float64(i))
		spikes[i] = EndSpike{
			X: int(math.Floor(42 * math.Cos(angle))),
			Z: int(math.Floor(42 * math.Sin(angle))),
			Radius: 2 + size/3,
			Height: 76 + size*3,
			Guarded: size == 1 || size == 2,
		}
	}
	return spikes
}

// EndSpikeMaterial leaves the existing block unchanged at zero.
type EndSpikeMaterial uint8

const (
	EndSpikeUnchanged EndSpikeMaterial = iota
	EndSpikeAir
	EndSpikeObsidian
	EndSpikeBedrock
	EndSpikeFire
	EndSpikeIronBars
)

// BlockAt returns the final block after the cylinder, cage and crystal pedestal
// passes. Iron-bar connections use north/east/south/west bits 1/2/4/8.
func (s *EndSpike) BlockAt(x, y, z, minY int) (EndSpikeMaterial, uint8) {
	dx, dz := x-s.X, z-s.Z
	if dx < -s.Radius || dx > s.Radius || dz < -s.Radius || dz > s.Radius || y < minY || y > s.Height+10 {
		return EndSpikeUnchanged, 0
	}
	if dx == 0 && dz == 0 {
		if y == s.Height {
			return EndSpikeBedrock, 0
		}
		if y == s.Height+1 {
			return EndSpikeFire, 0
		}
	}
	if s.Guarded && dx >= -2 && dx <= 2 && dz >= -2 && dz <= 2 && y >= s.Height && y <= s.Height+3 {
		xSide, zSide, top := dx == -2 || dx == 2, dz == -2 || dz == 2, y == s.Height+3
		if xSide || zSide || top {
			var connections uint8
			if (xSide || top) && dz != -2 { connections |= 1 }
			if (zSide || top) && dx != 2 { connections |= 2 }
			if (xSide || top) && dz != 2 { connections |= 4 }
			if (zSide || top) && dx != -2 { connections |= 8 }
			return EndSpikeIronBars, connections
		}
	}
	if dx*dx+dz*dz <= s.Radius*s.Radius+1 && y < s.Height {
		return EndSpikeObsidian, 0
	}
	if y > 65 {
		return EndSpikeAir, 0
	}
	return EndSpikeUnchanged, 0
}
