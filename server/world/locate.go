package world

import (
	"fmt"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// JavaBiomeAtProvider exposes the generator's Java biome source without loading or generating chunks.
type JavaBiomeAtProvider interface {
	JavaBiomeAt(x, y, z int32) string
}

// FindNearestBiome samples generator biomes on Java's locate-biome 32x64 grid.
// The horizontal search is bounded to 6,400 blocks from origin.
func (w *World) FindNearestBiome(name string, origin cube.Pos) (cube.Pos, bool, error) {
	if w == nil {
		return cube.Pos{}, false, fmt.Errorf("locate biome: nil world")
	}
	provider, ok := w.conf.Generator.(JavaBiomeAtProvider)
	if !ok {
		return cube.Pos{}, false, fmt.Errorf("locate biome: generator %T does not expose Java biomes", w.conf.Generator)
	}
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	area := w.Range()
	bestDistance := int64(math.MaxInt64)
	var best cube.Pos
	found := false
	const radius = 6400
	for ring := 0; ring <= radius; ring += 32 {
		visit := func(dx, dz int) {
			x, z := origin[0]+dx, origin[2]+dz
			distance := int64(dx)*int64(dx) + int64(dz)*int64(dz)
			if distance > radius*radius || distance > bestDistance {
				return
			}
			centerY := max(area[0], min(origin[1], area[1]))
			for delta := 0; ; delta += 64 {
				visited := false
				for index, y := range []int{centerY + delta, centerY - delta} {
					if y < area[0] || y > area[1] || delta == 0 && index == 1 {
						continue
					}
					visited = true
					biome := provider.JavaBiomeAt(int32(x), int32(y), int32(z))
					if !strings.Contains(biome, ":") {
						biome = "minecraft:" + biome
					}
					if biome != name {
						continue
					}
					vertical := int64(y - origin[1])
					total := distance + vertical*vertical
					if !found || total < bestDistance || total == bestDistance && posLess(cube.Pos{x, y, z}, best) {
						best, bestDistance, found = cube.Pos{x, y, z}, total, true
					}
				}
				if !visited {
					break
				}
			}
		}
		if ring == 0 {
			visit(0, 0)
		} else {
			for d := -ring; d <= ring; d += 32 {
				visit(d, -ring)
				visit(d, ring)
			}
			for d := -ring + 32; d < ring; d += 32 {
				visit(-ring, d)
				visit(ring, d)
			}
		}
		if found && int64(ring+32)*int64(ring+32) > bestDistance {
			break
		}
	}
	return best, found, nil
}

// FindNearestPOIByType returns the closest loaded POI of the requested Java type.
func (tx *Tx) FindNearestPOIByType(origin cube.Pos, name string, radius int) (cube.Pos, POI, bool) {
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	limit := int64(radius) * int64(radius)
	bestDistance := int64(math.MaxInt64)
	var best cube.Pos
	var value POI
	found := false
	for pos, poi := range tx.World().pois {
		if poi.Type != name {
			continue
		}
		dx, dy, dz := int64(pos[0]-origin[0]), int64(pos[1]-origin[1]), int64(pos[2]-origin[2])
		distance := dx*dx + dy*dy + dz*dz
		if distance > limit {
			continue
		}
		if !found || distance < bestDistance || distance == bestDistance && posLess(pos, best) {
			best, bestDistance, value, found = pos, distance, poi, true
		}
	}
	return best, value, found
}
