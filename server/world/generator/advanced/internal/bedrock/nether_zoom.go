package bedrock

// NetherBiomeZoomSeed applies the Nether Voronoi layer's world-seed mix.
// Its increment is GenLayerVoronoi's layer salt 10 expanded by three LCG steps.
func NetherBiomeZoomSeed(seed uint64) uint64 {
	const (
		multiplier = uint64(0x5851f42d4c957f2d)
		increment  = uint64(0x14057b7ef767814f)
		stream     = uint64(0x86bab6d7626f4bd8)
	)

	state := (seed*multiplier+increment)*seed + stream
	state = (state*multiplier+increment)*state + stream
	return (state*multiplier+increment)*state + stream
}

// NetherBiomeCell maps a block position to the Nether climate-cell position
// selected by the native two-dimensional 4x Voronoi zoomer.
func NetherBiomeCell(seed uint64, blockX, blockZ int32) (int32, int32) {

	x, z := blockX, blockZ
	x -= 2
	z -= 2
	cellX, cellZ := x>>2, z>>2
	localX, localZ := float32(x-cellX*4), float32(z-cellZ*4)
	startSalt := NetherBiomeZoomSeed(seed)
	startSeed := netherZoomStep(startSalt, 0)

	ax, az := netherZoomOffset(startSeed, startSalt, cellX*4, cellZ*4)
	bx, bz := netherZoomOffset(startSeed, startSalt, (cellX+1)*4, cellZ*4)
	cx, cz := netherZoomOffset(startSeed, startSalt, cellX*4, (cellZ+1)*4)
	dx, dz := netherZoomOffset(startSeed, startSalt, (cellX+1)*4, (cellZ+1)*4)

	ax, az = netherZoomDistance(localX, localZ, ax, az, 0, 0)
	bx, bz = netherZoomDistance(localX, localZ, bx, bz, 4, 0)
	cx, cz = netherZoomDistance(localX, localZ, cx, cz, 0, 4)
	dx, dz = netherZoomDistance(localX, localZ, dx, dz, 4, 4)
	da := float32(ax*ax + az*az)
	db := float32(bx*bx + bz*bz)
	dc := float32(cx*cx + cz*cz)
	dd := float32(dx*dx + dz*dz)

	switch {
	case da < db && da < dc && da < dd:
		return cellX, cellZ
	case db < da && db < dc && db < dd:
		return cellX + 1, cellZ
	case dc < da && dc < db && dc < dd:
		return cellX, cellZ + 1
	default:
		return cellX + 1, cellZ + 1
	}
}

func netherZoomStep(state, addend uint64) uint64 {
	return state*(state*0x5851f42d4c957f2d+0x14057b7ef767814f) + addend
}

func netherZoomOffset(startSeed, startSalt uint64, x, z int32) (float32, float32) {
	state := startSeed + uint64(int64(x))
	state = netherZoomStep(state, uint64(int64(z)))
	state = netherZoomStep(state, uint64(int64(x)))
	state = netherZoomStep(state, uint64(int64(z)))

	jitterX := netherZoomJitter(state)
	zState := netherZoomStep(state, startSalt)
	jitterZ := netherZoomJitter(zState)
	return jitterX, jitterZ
}

func netherZoomJitter(state uint64) float32 {
	value := float32((state >> 24) & 1023)
	return float32(float32(value*0x1p-10-float32(0.5)) * float32(3.6))
}

func netherZoomDistance(localX, localZ, jitterX, jitterZ, cornerX, cornerZ float32) (float32, float32) {
	dx := float32(localX - float32(cornerX+jitterX))
	dz := float32(localZ - float32(cornerZ+jitterZ))
	return dx, dz
}
