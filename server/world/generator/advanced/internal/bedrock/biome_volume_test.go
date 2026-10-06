package bedrock

import "testing"

func TestBiomeVolumeNativeBoundariesAndReuse(t *testing.T) {
	// Interior observations come from the live BDS API. The two vertical
	// corners were decoded from its saved dappled-forest biome column.
	observations := []struct {
		seed    int64
		x, y, z int32
		name    string
	}{
		{1, -16, -60, -16, "minecraft:deep_ocean"},
		{1 << 32, -16, -60, -16, "minecraft:taiga"},
		{9223372036854775807, 4127, 319, -4097, "minecraft:dappled_forest"},
		{9223372036854775807, 4112, -64, -4112, "minecraft:river"},
		{9223372036854775807, -16, -60, -16, "minecraft:deep_dark"},
	}
	ids := make(map[string]uint32, len(climateBiomeNames))
	names := make(map[uint32]string, len(climateBiomeNames))
	for index, name := range climateBiomeNames {
		id := uint32(index + 1000)
		ids[name], names[id] = id, name
	}
	resolve := func(name string) uint32 { return ids[name] }
	var source *BiomeSource
	var volume BiomeVolume
	var seed int64
	var chunkX, chunkZ int32
	for _, point := range observations {
		x, z := point.x>>4, point.z>>4
		if source == nil || seed != point.seed {
			source = NewBiomeSource(point.seed, NewClimate(point.seed), resolve)
			source.Prepare(x, z, &volume)
			seed, chunkX, chunkZ = point.seed, x, z
		} else if x != chunkX || z != chunkZ {
			source.Prepare(x, z, &volume)
			chunkX, chunkZ = x, z
		}
		got := names[volume.Biome(uint8(point.x&15), int16(point.y), uint8(point.z&15))]
		if got != point.name {
			t.Fatalf("seed %d at (%d,%d,%d): got %s, want %s", point.seed, point.x, point.y, point.z, got, point.name)
		}
	}
}
