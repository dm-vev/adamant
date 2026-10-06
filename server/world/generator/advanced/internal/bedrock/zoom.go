package bedrock

import "math"

// BiomeCell selects the eight-block climate cell sampled at the block position.
// Bedrock uses only the low 32 seed bits for this spatial interpolation, but
// retains all 64 seed bits when initialising the climate noises themselves.
func BiomeCell(seed uint32, x, y, z int32) (int32, int32, int32) {
	cx, cy, cz := x>>3, y>>3, z>>3
	fx, fy, fz := float32(x&7)*0.125, float32(y&7)*0.125, float32(z&7)*0.125
	best, selected := float32(math.MaxFloat32), int32(0)
	for corner := int32(0); corner < 8; corner++ {
		dx, dy, dz := corner>>2, corner>>1&1, corner&1
		ox, oy, oz := voronoiOffset(seed, cx+dx, cy+dy, cz+dz)
		ox = float32(ox + float32(fx-float32(dx)))
		oy = float32(oy + float32(fy-float32(dy)))
		oz = float32(oz + float32(fz-float32(dz)))
		distance := float32(float32(float32(ox*ox)+float32(oy*oy)) + float32(oz*oz))
		if distance < best {
			best, selected = distance, corner
		}
	}
	return cx + selected>>2, cy + (selected>>1)&1, cz + selected&1
}

func voronoiOffset(seed uint32, x, y, z int32) (float32, float32, float32) {
	h := uint64(int64((x*3129871)^y) ^ int64(z)*116129781)
	h = (h*h*42317861 + h*11) >> 16
	state := h ^ uint64(seed) ^ 0x5deece66d
	return voronoiJitter(&state), voronoiJitter(&state), voronoiJitter(&state)
}

func voronoiJitter(state *uint64) float32 {
	*state = *state*0x5deece66d + 11
	value := float32((*state >> 24) & 0xffffff)
	return float32(float32(float32(value*0x1p-24)-0.5) * 0.9)
}
