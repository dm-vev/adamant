package end

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func (g *End) FindNearestMapFeature(tag string, origin cube.Pos, searchRadius int, isReferenced func(world.StructureLocation) bool) (location world.StructureLocation, found bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			location, found = world.StructureLocation{}, false
			err = fmt.Errorf("End structure lookup failed: %v", recovered)
		}
	}()
	if searchRadius < 0 {
		return world.StructureLocation{}, false, fmt.Errorf("negative structure search radius %d", searchRadius)
	}
	members, err := structure.StructureTagMembers(tag)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	containsCity := false
	for _, member := range members {
		if member == "minecraft:end_city" {
			containsCity = true
			break
		}
	}
	if !containsCity {
		return world.StructureLocation{}, false, nil
	}
	chunkX, chunkZ := int(origin[0]>>4), int(origin[2]>>4)
	for radius := 0; radius <= searchRadius; radius++ {
		radiusFound := false
		bestDistance := math.MaxFloat64
		var best world.StructureLocation
		for x := -radius; x <= radius; x++ {
			xEdge := x == -radius || x == radius
			for z := -radius; z <= radius; z++ {
				if !xEdge && z != -radius && z != radius {
					continue
				}
				cellX := floorDiv(chunkX+20*x, 20)
				cellZ := floorDiv(chunkZ+20*z, 20)
				startX, startZ := g.endCityStartChunk(cellX, cellZ)
				if !g.isEndCityStart(startX, startZ) {
					continue
				}
				start, err := g.endCityStructure(startX, startZ)
				if err != nil {
					return world.StructureLocation{}, false, err
				}
				if start == nil {
					continue
				}
				loc := world.StructureLocation{ID: "minecraft:end_city", StartChunk: world.ChunkPos{int32(startX), int32(startZ)}, Position: cube.Pos{startX * 16, 0, startZ * 16}}
				if isReferenced != nil && isReferenced(loc) {
					continue
				}
				radiusFound = true
				dx, dy, dz := float64(origin[0]-loc.Position[0]), float64(origin[1]-loc.Position[1]), float64(origin[2]-loc.Position[2])
				distance := dx*dx + dy*dy + dz*dz
				if distance < bestDistance {
					bestDistance, best = distance, loc
				}
			}
		}
		if radiusFound {
			return best, true, nil
		}
	}
	return world.StructureLocation{}, false, nil
}

func (g *End) FindStructureAt(tag string, position cube.Pos) (world.StructureLocation, bool, error) {
	members, err := structure.StructureTagMembers(tag)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	containsCity := false
	for _, member := range members {
		if member == "minecraft:end_city" {
			containsCity = true
			break
		}
	}
	if !containsCity {
		return world.StructureLocation{}, false, nil
	}
	chunkX, chunkZ := int(position[0]>>4), int(position[2]>>4)
	cellX, cellZ := floorDiv(chunkX, 20), floorDiv(chunkZ, 20)
	for dz := -8; dz <= 8; dz++ {
		for dx := -8; dx <= 8; dx++ {
			startX, startZ := g.endCityStartChunk(cellX+dx, cellZ+dz)
			if !g.isEndCityStart(startX, startZ) {
				continue
			}
			start, err := g.endCityStructure(startX, startZ)
			if err != nil {
				return world.StructureLocation{}, false, err
			}
			if start == nil || position[0] < start.bb.minX || position[0] > start.bb.maxX || position[1] < start.bb.minY || position[1] > start.bb.maxY || position[2] < start.bb.minZ || position[2] > start.bb.maxZ {
				continue
			}
			return world.StructureLocation{ID: "minecraft:end_city", StartChunk: world.ChunkPos{int32(startX), int32(startZ)}, Position: cube.Pos{startX * 16, 0, startZ * 16}}, true, nil
		}
	}
	return world.StructureLocation{}, false, nil
}
