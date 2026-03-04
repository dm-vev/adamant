package genlayer

import (
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld/internal/biome"
)

const (
	climateOceanic  = 0
	climateWarm     = 1
	climateLush     = 2
	climateCold     = 3
	climateFreezing = 4

	specialBitsMask = 0xf00
)

type layerContinent struct {
	baseLayer
}

func (l *layerContinent) GetInts(x, z, w, h int) []int {
	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			r := l.randAt(int64(x+dx), int64(z+dz))
			if r.nextInt(10) == 0 {
				out[dx+dz*w] = 1
			} else {
				out[dx+dz*w] = 0
			}
		}
	}
	if x > -w && x <= 0 && z > -h && z <= 0 {
		out[-z*w-x] = 1
	}
	return out
}

type layerZoom struct {
	baseLayer
	parent Layer
	fuzzy  bool
}

func (l *layerZoom) GetInts(x, z, w, h int) []int {
	px := x >> 1
	pz := z >> 1
	pw := ((x + w) >> 1) - px + 2
	ph := ((z + h) >> 1) - pz + 2

	parent := l.parent.GetInts(px, pz, pw, ph)
	newW := (pw - 1) * 2
	newH := (ph - 1) * 2
	expanded := borrowInts(newW * newH)

	for j := 0; j < ph-1; j++ {
		for i := 0; i < pw-1; i++ {
			v00 := parent[i+j*pw]
			v10 := parent[i+1+j*pw]
			v01 := parent[i+(j+1)*pw]
			v11 := parent[i+1+(j+1)*pw]

			r := l.randAt(int64((i+px)*2), int64((j+pz)*2))
			x2 := 2 * i
			z2 := 2 * j
			expanded[x2+z2*newW] = v00
			expanded[x2+(z2+1)*newW] = selectRandom(&r, v00, v01)
			expanded[x2+1+z2*newW] = selectRandom(&r, v00, v10)
			if l.fuzzy {
				expanded[x2+1+(z2+1)*newW] = selectRandom4(&r, v00, v10, v01, v11)
			} else {
				expanded[x2+1+(z2+1)*newW] = selectModeOrRandom(&r, v00, v10, v01, v11)
			}
		}
	}

	out := borrowInts(w * h)
	offX := x & 1
	offZ := z & 1
	for j := 0; j < h; j++ {
		row := (j + offZ) * newW
		copy(out[j*w:(j+1)*w], expanded[row+offX:row+offX+w])
	}
	releaseInts(parent)
	releaseInts(expanded)
	return out
}

func selectRandom(r *chunkRand, a, b int) int {
	if r.nextInt(2) == 0 {
		return a
	}
	return b
}

func selectRandom4(r *chunkRand, a, b, c, d int) int {
	switch r.nextInt(4) {
	case 0:
		return a
	case 1:
		return b
	case 2:
		return c
	default:
		return d
	}
}

func selectModeOrRandom(r *chunkRand, v00, v10, v01, v11 int) int {
	same00 := 0
	if v00 == v10 {
		same00++
	}
	if v00 == v01 {
		same00++
	}
	if v00 == v11 {
		same00++
	}

	same10 := 0
	if v10 == v01 {
		same10++
	}
	if v10 == v11 {
		same10++
	}

	same01 := 0
	if v01 == v11 {
		same01++
	}

	switch {
	case same00 > same10 && same00 > same01:
		return v00
	case same10 > same00:
		return v10
	case same01 > same00:
		return v01
	default:
		return selectRandom4(r, v00, v10, v01, v11)
	}
}

type layerAddIsland struct {
	baseLayer
	parent Layer
}

func (l *layerAddIsland) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			nw := parent[(dx+0)+(dz+0)*pw]
			ne := parent[(dx+2)+(dz+0)*pw]
			sw := parent[(dx+0)+(dz+2)*pw]
			se := parent[(dx+2)+(dz+2)*pw]
			center := parent[(dx+1)+(dz+1)*pw]

			v := center
			switch center {
			case 0:
				if nw != 0 || ne != 0 || sw != 0 || se != 0 {
					r := l.randAt(int64(x+dx), int64(z+dz))
					chosen := 1
					inc := 1
					if nw != 0 {
						if r.nextInt(inc) == 0 {
							chosen = nw
						}
						inc++
					}
					if ne != 0 {
						if r.nextInt(inc) == 0 {
							chosen = ne
						}
						inc++
					}
					if sw != 0 {
						if r.nextInt(inc) == 0 {
							chosen = sw
						}
						inc++
					}
					if se != 0 {
						if r.nextInt(inc) == 0 {
							chosen = se
						}
						inc++
					}
					if r.nextInt(3) == 0 {
						v = chosen
					} else if chosen == 4 {
						v = 4
					} else {
						v = 0
					}
				}
			case 4:
				v = 4
			default:
				if nw == 0 || ne == 0 || sw == 0 || se == 0 {
					r := l.randAt(int64(x+dx), int64(z+dz))
					if r.nextInt(5) == 0 {
						if center == 4 {
							v = 4
						} else {
							v = 0
						}
					}
				}
			}
			out[dx+dz*w] = v
		}
	}
	releaseInts(parent)
	return out
}

type layerRemoveTooMuchOcean struct {
	baseLayer
	parent Layer
}

func (l *layerRemoveTooMuchOcean) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			center := parent[(dx+1)+(dz+1)*pw]
			if center == climateOceanic &&
				parent[(dx+1)+(dz+0)*pw] == climateOceanic &&
				parent[(dx+2)+(dz+1)*pw] == climateOceanic &&
				parent[(dx+0)+(dz+1)*pw] == climateOceanic &&
				parent[(dx+1)+(dz+2)*pw] == climateOceanic {
				r := l.randAt(int64(x+dx), int64(z+dz))
				if r.nextInt(2) == 0 {
					center = 1
				}
			}
			out[dx+dz*w] = center
		}
	}
	releaseInts(parent)
	return out
}

type layerAddSnow struct {
	baseLayer
	parent Layer
}

func (l *layerAddSnow) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			v := parent[(dx+1)+(dz+1)*pw]
			if !biome.IsShallowOcean(v) {
				r := l.randAt(int64(x+dx), int64(z+dz))
				switch r.nextInt(6) {
				case 0:
					v = climateFreezing
				case 1:
					v = climateCold
				default:
					v = climateWarm
				}
			}
			out[dx+dz*w] = v
		}
	}
	releaseInts(parent)
	return out
}

type layerCoolWarm struct {
	baseLayer
	parent Layer
}

func (l *layerCoolWarm) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			v := parent[(dx+1)+(dz+1)*pw]
			if v == climateWarm {
				n := parent[(dx+1)+(dz+0)*pw]
				e := parent[(dx+2)+(dz+1)*pw]
				wv := parent[(dx+0)+(dz+1)*pw]
				s := parent[(dx+1)+(dz+2)*pw]
				if n == climateCold || n == climateFreezing || e == climateCold || e == climateFreezing || wv == climateCold || wv == climateFreezing || s == climateCold || s == climateFreezing {
					v = climateLush
				}
			}
			out[dx+dz*w] = v
		}
	}
	releaseInts(parent)
	return out
}

type layerHeatIce struct {
	baseLayer
	parent Layer
}

func (l *layerHeatIce) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			v := parent[(dx+1)+(dz+1)*pw]
			if v == climateFreezing {
				n := parent[(dx+1)+(dz+0)*pw]
				e := parent[(dx+2)+(dz+1)*pw]
				wv := parent[(dx+0)+(dz+1)*pw]
				s := parent[(dx+1)+(dz+2)*pw]
				if n == climateWarm || n == climateLush || e == climateWarm || e == climateLush || wv == climateWarm || wv == climateLush || s == climateWarm || s == climateLush {
					v = climateCold
				}
			}
			out[dx+dz*w] = v
		}
	}
	releaseInts(parent)
	return out
}

type layerSpecial struct {
	baseLayer
	parent Layer
}

func (l *layerSpecial) GetInts(x, z, w, h int) []int {
	out := l.parent.GetInts(x, z, w, h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			v := out[dx+dz*w]
			if v == climateOceanic {
				continue
			}
			r := l.randAt(int64(x+dx), int64(z+dz))
			if r.nextInt(13) == 0 {
				k := 1 + r.nextInt(15)
				v |= (k << 8) & specialBitsMask
				out[dx+dz*w] = v
			}
		}
	}
	return out
}

type layerAddMushroom struct {
	baseLayer
	parent Layer
}

func (l *layerAddMushroom) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			center := parent[(dx+1)+(dz+1)*pw]
			if center == 0 &&
				parent[(dx+0)+(dz+0)*pw] == 0 &&
				parent[(dx+2)+(dz+0)*pw] == 0 &&
				parent[(dx+0)+(dz+2)*pw] == 0 &&
				parent[(dx+2)+(dz+2)*pw] == 0 {
				r := l.randAt(int64(x+dx), int64(z+dz))
				if r.nextInt(100) == 0 {
					center = int(biome.MushroomFields)
				}
			}
			out[dx+dz*w] = center
		}
	}
	releaseInts(parent)
	return out
}

type layerDeepOcean struct {
	baseLayer
	parent Layer
}

func (l *layerDeepOcean) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			center := parent[(dx+1)+(dz+1)*pw]
			if biome.IsShallowOcean(center) {
				oceans := 0
				if biome.IsShallowOcean(parent[(dx+1)+(dz+0)*pw]) {
					oceans++
				}
				if biome.IsShallowOcean(parent[(dx+2)+(dz+1)*pw]) {
					oceans++
				}
				if biome.IsShallowOcean(parent[(dx+0)+(dz+1)*pw]) {
					oceans++
				}
				if biome.IsShallowOcean(parent[(dx+1)+(dz+2)*pw]) {
					oceans++
				}
				if oceans >= 4 {
					center = int(biome.DeepOcean)
				}
			}
			out[dx+dz*w] = center
		}
	}
	releaseInts(parent)
	return out
}

type layerBiome struct {
	baseLayer
	parent Layer
}

var (
	warmBiomes = []int{int(biome.Desert), int(biome.Desert), int(biome.Desert), int(biome.Savanna), int(biome.Savanna), int(biome.Plains)}
	lushBiomes = []int{int(biome.Forest), int(biome.DarkForest), int(biome.Mountains), int(biome.Plains), int(biome.BirchForest), int(biome.Swamp)}
	coldBiomes = []int{int(biome.Forest), int(biome.Mountains), int(biome.Taiga), int(biome.Plains)}
	snowBiomes = []int{int(biome.SnowyTundra), int(biome.SnowyTundra), int(biome.SnowyTundra), int(biome.SnowyTaiga)}
)

func (l *layerBiome) GetInts(x, z, w, h int) []int {
	out := l.parent.GetInts(x, z, w, h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			id := out[dx+dz*w]
			highBits := id & specialBitsMask
			id &^= specialBitsMask

			if biome.IsOceanic(id) || id == int(biome.MushroomFields) {
				out[dx+dz*w] = id
				continue
			}

			r := l.randAt(int64(x+dx), int64(z+dz))
			var v int
			switch id {
			case climateWarm:
				if highBits != 0 {
					if r.nextInt(3) == 0 {
						v = int(biome.BadlandsPlateau)
					} else {
						v = int(biome.WoodedBadlandsPlateau)
					}
				} else {
					v = warmBiomes[r.nextInt(len(warmBiomes))]
				}
			case climateLush:
				if highBits != 0 {
					v = int(biome.Jungle)
				} else {
					v = lushBiomes[r.nextInt(len(lushBiomes))]
				}
			case climateCold:
				if highBits != 0 {
					v = int(biome.GiantTreeTaiga)
				} else {
					v = coldBiomes[r.nextInt(len(coldBiomes))]
				}
			case climateFreezing:
				v = snowBiomes[r.nextInt(len(snowBiomes))]
			default:
				v = int(biome.MushroomFields)
			}
			out[dx+dz*w] = v
		}
	}
	return out
}

type layerRiverInit struct {
	baseLayer
	parent Layer
}

func (l *layerRiverInit) GetInts(x, z, w, h int) []int {
	out := l.parent.GetInts(x, z, w, h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			if out[dx+dz*w] > 0 {
				r := l.randAt(int64(x+dx), int64(z+dz))
				out[dx+dz*w] = r.nextInt(299999) + 2
			} else {
				out[dx+dz*w] = 0
			}
		}
	}
	return out
}

type layerBiomeEdge struct {
	baseLayer
	parent Layer
}

func (l *layerBiomeEdge) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			center := parent[(dx+1)+(dz+1)*pw]
			n := parent[(dx+1)+(dz+0)*pw]
			e := parent[(dx+2)+(dz+1)*pw]
			wv := parent[(dx+0)+(dz+1)*pw]
			s := parent[(dx+1)+(dz+2)*pw]

			idx := dx + dz*w

			if replaceEdge(out, idx, center, n, e, wv, s, int(biome.WoodedBadlandsPlateau), int(biome.Badlands)) ||
				replaceEdge(out, idx, center, n, e, wv, s, int(biome.BadlandsPlateau), int(biome.Badlands)) ||
				replaceEdge(out, idx, center, n, e, wv, s, int(biome.GiantTreeTaiga), int(biome.Taiga)) {
				continue
			}

			switch center {
			case int(biome.Desert):
				if n != int(biome.SnowyTundra) && e != int(biome.SnowyTundra) && wv != int(biome.SnowyTundra) && s != int(biome.SnowyTundra) {
					out[idx] = center
				} else {
					out[idx] = int(biome.WoodedMountains)
				}
			case int(biome.Swamp):
				if n != int(biome.Desert) && e != int(biome.Desert) && wv != int(biome.Desert) && s != int(biome.Desert) &&
					n != int(biome.SnowyTaiga) && e != int(biome.SnowyTaiga) && wv != int(biome.SnowyTaiga) && s != int(biome.SnowyTaiga) &&
					n != int(biome.SnowyTundra) && e != int(biome.SnowyTundra) && wv != int(biome.SnowyTundra) && s != int(biome.SnowyTundra) {
					if n != int(biome.Jungle) && e != int(biome.Jungle) && wv != int(biome.Jungle) && s != int(biome.Jungle) {
						out[idx] = center
					} else {
						out[idx] = int(biome.JungleEdge)
					}
				} else {
					out[idx] = int(biome.Plains)
				}
			default:
				out[idx] = center
			}
		}
	}
	releaseInts(parent)
	return out
}

func replaceEdge(out []int, idx, center, n, e, wv, s, baseID, edgeID int) bool {
	if center != baseID {
		return false
	}
	if biome.AreSimilar(n, baseID) && biome.AreSimilar(e, baseID) && biome.AreSimilar(wv, baseID) && biome.AreSimilar(s, baseID) {
		out[idx] = center
	} else {
		out[idx] = edgeID
	}
	return true
}

type layerHills struct {
	baseLayer
	parent  Layer
	parent2 Layer
}

func (l *layerHills) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	biomes := l.parent.GetInts(px, pz, pw, ph)
	rivers := l.parent2.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			a11 := biomes[(dx+1)+(dz+1)*pw]
			b11 := rivers[(dx+1)+(dz+1)*pw]

			bn := (b11 - 2) % 29
			idx := dx + dz*w

			if bn == 1 && b11 >= 2 && !biome.IsShallowOcean(a11) {
				if m := biome.Mutated(a11); m > 0 {
					out[idx] = m
				} else {
					out[idx] = a11
				}
				continue
			}

			r := l.randAt(int64(x+dx), int64(z+dz))
			r3 := r.nextInt(3)
			if bn != 0 && r3 != 0 {
				out[idx] = a11
				continue
			}

			hill := hillVariant(&r, a11)
			if bn == 0 && hill != a11 {
				if m := biome.Mutated(hill); m > 0 {
					hill = m
				} else {
					hill = a11
				}
			}

			if hill != a11 {
				n := biomes[(dx+1)+(dz+0)*pw]
				e := biomes[(dx+2)+(dz+1)*pw]
				wv := biomes[(dx+0)+(dz+1)*pw]
				s := biomes[(dx+1)+(dz+2)*pw]

				equals := 0
				if biome.AreSimilar(n, a11) {
					equals++
				}
				if biome.AreSimilar(e, a11) {
					equals++
				}
				if biome.AreSimilar(wv, a11) {
					equals++
				}
				if biome.AreSimilar(s, a11) {
					equals++
				}
				if equals >= 3 {
					out[idx] = hill
				} else {
					out[idx] = a11
				}
			} else {
				out[idx] = a11
			}
		}
	}
	releaseInts(biomes)
	releaseInts(rivers)
	return out
}

func hillVariant(r *chunkRand, id int) int {
	switch biome.ID(id) {
	case biome.Desert:
		return int(biome.DesertHills)
	case biome.Forest:
		return int(biome.WoodedHills)
	case biome.BirchForest:
		return int(biome.BirchForestHills)
	case biome.DarkForest:
		return int(biome.Plains)
	case biome.Taiga:
		return int(biome.TaigaHills)
	case biome.GiantTreeTaiga:
		return int(biome.GiantTreeTaigaHills)
	case biome.SnowyTaiga:
		return int(biome.SnowyTaigaHills)
	case biome.Plains:
		if r.nextInt(3) == 0 {
			return int(biome.WoodedHills)
		}
		return int(biome.Forest)
	case biome.SnowyTundra:
		return int(biome.SnowyMountains)
	case biome.Jungle:
		return int(biome.JungleHills)
	case biome.Ocean:
		return int(biome.DeepOcean)
	case biome.Mountains:
		return int(biome.WoodedMountains)
	case biome.Savanna:
		return int(biome.SavannaPlateau)
	default:
		if biome.AreSimilar(id, int(biome.WoodedBadlandsPlateau)) {
			return int(biome.Badlands)
		}
		if biome.IsDeepOcean(id) {
			if r.nextInt(3) == 0 {
				if r.nextInt(2) == 0 {
					return int(biome.Plains)
				}
				return int(biome.Forest)
			}
		}
		return id
	}
}

type layerSunflower struct {
	baseLayer
	parent Layer
}

func (l *layerSunflower) GetInts(x, z, w, h int) []int {
	out := l.parent.GetInts(x, z, w, h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			if out[dx+dz*w] != int(biome.Plains) {
				continue
			}
			r := l.randAt(int64(x+dx), int64(z+dz))
			if r.nextInt(57) == 0 {
				out[dx+dz*w] = int(biome.SunflowerPlains)
			}
		}
	}
	return out
}

type layerShore struct {
	baseLayer
	parent Layer
}

func (l *layerShore) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			center := parent[(dx+1)+(dz+1)*pw]
			n := parent[(dx+1)+(dz+0)*pw]
			e := parent[(dx+2)+(dz+1)*pw]
			wv := parent[(dx+0)+(dz+1)*pw]
			s := parent[(dx+1)+(dz+2)*pw]

			idx := dx + dz*w

			if center == int(biome.MushroomFields) {
				if n == int(biome.Ocean) || e == int(biome.Ocean) || wv == int(biome.Ocean) || s == int(biome.Ocean) {
					out[idx] = int(biome.MushroomFieldShore)
				} else {
					out[idx] = center
				}
				continue
			}

			if biome.CategoryOf(center) == biome.CategoryJungle {
				if isJungleCompatible(n) && isJungleCompatible(e) && isJungleCompatible(wv) && isJungleCompatible(s) {
					if biome.IsOceanic(n) || biome.IsOceanic(e) || biome.IsOceanic(wv) || biome.IsOceanic(s) {
						out[idx] = int(biome.Beach)
					} else {
						out[idx] = center
					}
				} else {
					out[idx] = int(biome.JungleEdge)
				}
				continue
			}

			if center == int(biome.Mountains) || center == int(biome.WoodedMountains) {
				if biome.IsOceanic(n) || biome.IsOceanic(e) || biome.IsOceanic(wv) || biome.IsOceanic(s) {
					out[idx] = int(biome.StoneShore)
				} else {
					out[idx] = center
				}
				continue
			}

			if biome.IsSnowy(center) {
				if biome.IsOceanic(n) || biome.IsOceanic(e) || biome.IsOceanic(wv) || biome.IsOceanic(s) {
					out[idx] = int(biome.SnowyBeach)
				} else {
					out[idx] = center
				}
				continue
			}

			if center == int(biome.Badlands) || center == int(biome.WoodedBadlandsPlateau) {
				if !biome.IsOceanic(n) && !biome.IsOceanic(e) && !biome.IsOceanic(wv) && !biome.IsOceanic(s) {
					if biome.IsMesa(n) && biome.IsMesa(e) && biome.IsMesa(wv) && biome.IsMesa(s) {
						out[idx] = center
					} else {
						out[idx] = int(biome.Desert)
					}
				} else {
					out[idx] = center
				}
				continue
			}

			if center != int(biome.Ocean) && center != int(biome.DeepOcean) && center != int(biome.River) && center != int(biome.Swamp) {
				if biome.IsOceanic(n) || biome.IsOceanic(e) || biome.IsOceanic(wv) || biome.IsOceanic(s) {
					out[idx] = int(biome.Beach)
				} else {
					out[idx] = center
				}
				continue
			}

			out[idx] = center
		}
	}
	releaseInts(parent)
	return out
}

func isJungleCompatible(id int) bool {
	c := biome.CategoryOf(id)
	return c == biome.CategoryJungle || id == int(biome.Forest) || c == biome.CategoryTaiga || biome.IsOceanic(id)
}

type layerRiver struct {
	baseLayer
	parent Layer
}

func (l *layerRiver) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			v01 := reduceRiverID(parent[(dx+0)+(dz+1)*pw])
			v11 := reduceRiverID(parent[(dx+1)+(dz+1)*pw])
			v21 := reduceRiverID(parent[(dx+2)+(dz+1)*pw])
			v10 := reduceRiverID(parent[(dx+1)+(dz+0)*pw])
			v12 := reduceRiverID(parent[(dx+1)+(dz+2)*pw])

			if v11 == v01 && v11 == v10 && v11 == v12 && v11 == v21 {
				out[dx+dz*w] = -1
			} else {
				out[dx+dz*w] = int(biome.River)
			}
		}
	}
	releaseInts(parent)
	return out
}

func reduceRiverID(id int) int {
	if id >= 2 {
		return 2 + (id & 1)
	}
	return id
}

type layerSmooth struct {
	baseLayer
	parent Layer
}

func (l *layerSmooth) GetInts(x, z, w, h int) []int {
	px, pz := x-1, z-1
	pw, ph := w+2, h+2
	parent := l.parent.GetInts(px, pz, pw, ph)

	out := borrowInts(w * h)
	for dz := 0; dz < h; dz++ {
		for dx := 0; dx < w; dx++ {
			center := parent[(dx+1)+(dz+1)*pw]
			west := parent[(dx+0)+(dz+1)*pw]
			north := parent[(dx+1)+(dz+0)*pw]

			if center != west || center != north {
				east := parent[(dx+2)+(dz+1)*pw]
				south := parent[(dx+1)+(dz+2)*pw]
				if west == east && north == south {
					r := l.randAt(int64(x+dx), int64(z+dz))
					if r.nextInt(2) == 0 {
						center = north
					} else {
						center = west
					}
				} else {
					if west == east {
						center = west
					}
					if north == south {
						center = north
					}
				}
			}
			out[dx+dz*w] = center
		}
	}
	releaseInts(parent)
	return out
}

type layerRiverMix struct {
	baseLayer
	parent  Layer
	parent2 Layer
}

func (l *layerRiverMix) GetInts(x, z, w, h int) []int {
	biomes := l.parent.GetInts(x, z, w, h)
	rivers := l.parent2.GetInts(x, z, w, h)

	for i := range biomes {
		b := biomes[i]
		if rivers[i] == int(biome.River) && b != int(biome.Ocean) && !biome.IsOceanic(b) {
			if b == int(biome.SnowyTundra) {
				biomes[i] = int(biome.FrozenRiver)
			} else if b == int(biome.MushroomFields) || b == int(biome.MushroomFieldShore) {
				biomes[i] = int(biome.MushroomFieldShore)
			} else {
				biomes[i] = int(biome.River)
			}
		}
	}
	releaseInts(rivers)
	return biomes
}

type layerVoronoi114 struct {
	baseLayer
	parent Layer
}

func (l *layerVoronoi114) GetInts(x, z, w, h int) []int {
	x -= 2
	z -= 2
	px := x >> 2
	pz := z >> 2
	pw := ((x + w) >> 2) - px + 2
	ph := ((z + h) >> 2) - pz + 2

	parent := l.parent.GetInts(px, pz, pw, ph)
	out := borrowInts(w * h)

	for pj := 0; pj < ph-1; pj++ {
		j4 := (pz+pj)*4 - z
		for pi := 0; pi < pw-1; pi++ {
			i4 := (px+pi)*4 - x

			v00 := parent[(pi+0)+(pj+0)*pw]
			v10 := parent[(pi+1)+(pj+0)*pw]
			v01 := parent[(pi+0)+(pj+1)*pw]
			v11 := parent[(pi+1)+(pj+1)*pw]

			if v00 == v01 && v00 == v10 && v00 == v11 {
				for jj := 0; jj < 4; jj++ {
					j := j4 + jj
					if j < 0 || j >= h {
						continue
					}
					for ii := 0; ii < 4; ii++ {
						i := i4 + ii
						if i < 0 || i >= w {
							continue
						}
						out[i+j*w] = v00
					}
				}
				continue
			}

			a1, a2 := l.voronoiOffset((pi+px)*4, (pj+pz)*4)
			b1, b2 := l.voronoiOffset((pi+px+1)*4, (pj+pz)*4)
			c1, c2 := l.voronoiOffset((pi+px)*4, (pj+pz+1)*4)
			d1, d2 := l.voronoiOffset((pi+px+1)*4, (pj+pz+1)*4)

			b1 += 40 * 1024
			c2 += 40 * 1024
			d1 += 40 * 1024
			d2 += 40 * 1024

			for jj := 0; jj < 4; jj++ {
				j := j4 + jj
				if j < 0 || j >= h {
					continue
				}
				mj := int64(10240 * jj)
				sja := (mj - int64(a2)) * (mj - int64(a2))
				sjb := (mj - int64(b2)) * (mj - int64(b2))
				sjc := (mj - int64(c2)) * (mj - int64(c2))
				sjd := (mj - int64(d2)) * (mj - int64(d2))

				for ii := 0; ii < 4; ii++ {
					i := i4 + ii
					if i < 0 || i >= w {
						continue
					}
					mi := int64(10240 * ii)
					da := (mi-int64(a1))*(mi-int64(a1)) + sja
					db := (mi-int64(b1))*(mi-int64(b1)) + sjb
					dc := (mi-int64(c1))*(mi-int64(c1)) + sjc
					dd := (mi-int64(d1))*(mi-int64(d1)) + sjd

					switch {
					case da < db && da < dc && da < dd:
						out[i+j*w] = v00
					case db < da && db < dc && db < dd:
						out[i+j*w] = v10
					case dc < da && dc < db && dc < dd:
						out[i+j*w] = v01
					default:
						out[i+j*w] = v11
					}
				}
			}
		}
	}

	releaseInts(parent)
	return out
}

func (l *layerVoronoi114) voronoiOffset(x, z int) (int, int) {
	r := l.randAt(int64(x), int64(z))
	dx := (r.nextInt(1024) - 512) * 36
	dz := (r.nextInt(1024) - 512) * 36
	return dx, dz
}
