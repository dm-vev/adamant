package overworld

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func (g *Overworld) FindNearestMapFeature(tag string, origin cube.Pos, searchRadius int, isReferenced func(world.StructureLocation) bool) (location world.StructureLocation, found bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			location, found = world.StructureLocation{}, false
			err = fmt.Errorf("overworld structure lookup failed: %v", recovered)
		}
	}()
	if searchRadius < 0 {
		return world.StructureLocation{}, false, fmt.Errorf("negative structure search radius %d", searchRadius)
	}
	memberNames, err := structure.StructureTagMembers(tag)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	wanted := make(map[string]bool, len(memberNames))
	for _, name := range memberNames {
		wanted[name] = true
	}
	if len(wanted) == 0 {
		return world.StructureLocation{}, false, nil
	}
	s := g.modernStructures
	if s == nil {
		return world.StructureLocation{}, false, fmt.Errorf("overworld structure planner is unavailable")
	}
	chunkX, chunkZ := int32(origin[0]>>4), int32(origin[2]>>4)

	var ringBest world.StructureLocation
	var ringDistance float64 = math.MaxFloat64
	for _, set := range s.sets {
		if normalizeResource(stringValue(set.placement["type"])) != "concentric_rings" || !setContainsWanted(set, wanted) {
			continue
		}
		for candidate := range s.rings[set.name] {
			start, ok := s.makeStart(set, candidate[0], candidate[1])
			if !ok || !wanted[start.name] {
				continue
			}
			loc := world.StructureLocation{ID: start.name, StartChunk: world.ChunkPos{candidate[0], candidate[1]}, Position: modernLocatePosition(set.placement, candidate[0], candidate[1])}
			if isReferenced != nil && isReferenced(loc) {
				continue
			}
			dx, dy, dz := float64(origin[0]-int(candidate[0]*16+8)), float64(origin[1]-32), float64(origin[2]-int(candidate[1]*16+8))
			distance := dx*dx + dy*dy + dz*dz
			if distance < ringDistance {
				ringDistance, ringBest = distance, loc
			}
		}
	}
	if ringDistance != math.MaxFloat64 {
		location, found = ringBest, true
	}

	for radius := 0; radius <= searchRadius; radius++ {
		radiusFound := false
		bestDistance := math.MaxFloat64
		var best world.StructureLocation
		for _, set := range s.sets {
			placement := set.placement
			if normalizeResource(stringValue(placement["type"])) != "random_spread" || !setContainsWanted(set, wanted) {
				continue
			}
			spacing := int32(number(placement["spacing"]))
			for x := -radius; x <= radius; x++ {
				xEdge := x == -radius || x == radius
				for z := -radius; z <= radius; z++ {
					if !xEdge && z != -radius && z != radius {
						continue
					}
					sectorX, sectorZ := chunkX+spacing*int32(x), chunkZ+spacing*int32(z)
					candidate := s.randomSpreadChunk(set, sectorX, sectorZ)
					if !s.frequencyAllows(set, candidate[0], candidate[1]) {
						continue
					}
					start, ok := s.makeStart(set, candidate[0], candidate[1])
					if !ok || !wanted[start.name] {
						continue
					}
					loc := world.StructureLocation{ID: start.name, StartChunk: world.ChunkPos{candidate[0], candidate[1]}, Position: modernLocatePosition(placement, candidate[0], candidate[1])}
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
		}
		if radiusFound {
			if !found || bestDistance < ringDistance {
				location = best
			}
			return location, true, nil
		}
	}
	return location, found, nil
}

func (g *Overworld) FindStructureAt(tag string, position cube.Pos) (location world.StructureLocation, found bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			location, found = world.StructureLocation{}, false
			err = fmt.Errorf("overworld structure-at lookup failed: %v", recovered)
		}
	}()
	members, err := structure.StructureTagMembers(tag)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	wanted := make(map[string]bool, len(members))
	for _, member := range members {
		wanted[member] = true
	}
	s := g.modernStructures
	if s == nil {
		return world.StructureLocation{}, false, fmt.Errorf("overworld structure planner is unavailable")
	}
	chunkX, chunkZ := int32(position[0]>>4), int32(position[2]>>4)
	for _, set := range s.sets {
		if !setContainsWanted(set, wanted) {
			continue
		}
		if normalizeResource(stringValue(set.placement["type"])) == "concentric_rings" {
			for candidate := range s.rings[set.name] {
				start, ok := s.makeStart(set, candidate[0], candidate[1])
				if ok && wanted[start.name] && structureStartContains(start, position) {
					return world.StructureLocation{ID: start.name, StartChunk: world.ChunkPos{candidate[0], candidate[1]}, Position: modernLocatePosition(set.placement, candidate[0], candidate[1])}, true, nil
				}
			}
			continue
		}
		if normalizeResource(stringValue(set.placement["type"])) != "random_spread" {
			continue
		}
		for dz := int32(-32); dz <= 32; dz++ {
			for dx := int32(-32); dx <= 32; dx++ {
				candidate, ok := s.candidate(set, chunkX+dx, chunkZ+dz)
				if !ok {
					continue
				}
				start, ok := s.makeStart(set, candidate[0], candidate[1])
				if ok && wanted[start.name] && structureStartContains(start, position) {
					return world.StructureLocation{ID: start.name, StartChunk: world.ChunkPos{candidate[0], candidate[1]}, Position: modernLocatePosition(set.placement, candidate[0], candidate[1])}, true, nil
				}
			}
		}
	}
	return world.StructureLocation{}, false, nil
}

func structureStartContains(start modernStructureStart, position cube.Pos) bool {
	x, y, z := int32(position[0]), int32(position[1]), int32(position[2])
	return x >= start.box.Min[0] && x <= start.box.Max[0] && y >= start.box.Min[1] && y <= start.box.Max[1] && z >= start.box.Min[2] && z <= start.box.Max[2]
}

func setContainsWanted(set modernStructureSet, wanted map[string]bool) bool {
	for _, entry := range set.structures {
		if wanted[entry.name] {
			return true
		}
	}
	return false
}

func modernLocatePosition(placement map[string]any, chunkX, chunkZ int32) cube.Pos {
	offset, _ := placement["locate_offset"].([]any)
	var x, y, z int32
	if len(offset) == 3 {
		x, y, z = int32(number(offset[0])), int32(number(offset[1])), int32(number(offset[2]))
	}
	return cube.Pos{int(chunkX*16 + x), int(y), int(chunkZ*16 + z)}
}
