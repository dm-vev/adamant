package chunk

import "github.com/df-mc/dragonfly/server/block/cube"

// SpreadReference spreads light as Spread did before it skipped sheets of
// edges by their shared light and read the light of the edges straight from
// the sub chunks: block by block along every edge, reading each block's
// light through the lightArea. Tests compare Spread with it.
func (a *lightArea) SpreadReference() uint16 {
	a.changed = 0
	queue := newLevelQueue()
	for _, lt := range []light{BlockLight, SkyLight} {
		filter := a.nodesNeeded(lt)
		minY, maxY := a.r[0]>>4, a.r[1]>>4
		for cu := 1; cu < a.w; cu++ {
			u := cu << 4
			for cv := 0; cv < a.w; cv++ {
				v := cv << 4
				for cy := minY; cy < maxY; cy++ {
					baseY := cy << 4
					xa, za := cube.Pos{a.baseX + u, baseY, a.baseZ + v}, cube.Pos{a.baseX + v, baseY, a.baseZ + u}
					xb, zb := xa.Side(cube.FaceWest), za.Side(cube.FaceNorth)
					addX, addZ := filter(a.sub(xa), a.sub(xb)), filter(a.sub(za), a.sub(zb))
					for addV := 0; addV < 16; addV++ {
						for y := 0; y < 16; y++ {
							if addX {
								pa, pb := xa.Add(cube.Pos{0, y, addV}), xb.Add(cube.Pos{0, y, addV})
								a.spreadAcross(queue, lt, a.light(pa, lt), a.light(pb, lt), pa, pb)
							}
							if addZ {
								pa, pb := za.Add(cube.Pos{addV, y}), zb.Add(cube.Pos{addV, y})
								a.spreadAcross(queue, lt, a.light(pa, lt), a.light(pb, lt), pa, pb)
							}
						}
					}
				}
			}
		}
	}
	for !queue.empty() {
		a.propagate(queue)
	}
	return a.changed
}

// SharesLight reports whether the sub chunk at index i of c uses the light
// slices shared by all sub chunks with no light or full skylight.
func SharesLight(c *Chunk, i int) (sky, block bool) {
	s, b := &c.sub[i].skyLight[0], &c.sub[i].blockLight[0]
	return s == fullLightPtr || s == noLightPtr, b == noLightPtr
}
