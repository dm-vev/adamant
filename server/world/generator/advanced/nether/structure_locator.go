package nether

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func (g *Nether) FindNearestMapFeature(tag string, origin cube.Pos, searchRadius int, isReferenced func(world.StructureLocation) bool) (location world.StructureLocation, found bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			location, found = world.StructureLocation{}, false
			err = fmt.Errorf("Nether structure lookup failed: %v", recovered)
		}
	}()
	if searchRadius < 0 {
		return world.StructureLocation{}, false, fmt.Errorf("negative structure search radius %d", searchRadius)
	}
	members, err := structure.StructureTagMembers(tag)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	wanted := make(map[string]bool, len(members))
	for _, member := range members {
		wanted[member] = true
	}
	chunkX, chunkZ := int32(origin[0]>>4), int32(origin[2]>>4)
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
				for _, structure := range []string{"minecraft:fortress", "minecraft:bastion_remnant", "minecraft:ruined_portal_nether", "minecraft:nether_fossil"} {
					if !wanted[structure] {
						continue
					}
					spacing := netherLocateSpacing(structure)
					candidate, valid, err := g.netherLocateCandidate(structure, chunkX+spacing*int32(x), chunkZ+spacing*int32(z))
					if err != nil {
						return world.StructureLocation{}, false, err
					}
					if !valid || isReferenced != nil && isReferenced(candidate) {
						continue
					}
					radiusFound = true
					dx, dy, dz := float64(origin[0]-candidate.Position[0]), float64(origin[1]-candidate.Position[1]), float64(origin[2]-candidate.Position[2])
					distance := dx*dx + dy*dy + dz*dz
					if distance < bestDistance {
						bestDistance, best = distance, candidate
					}
				}
			}
		}
		if radiusFound {
			return best, true, nil
		}
	}
	return world.StructureLocation{}, false, nil
}

func (g *Nether) netherLocateCandidate(id string, sectorX, sectorZ int32) (world.StructureLocation, bool, error) {
	var salt, spacing, separation int32
	switch id {
	case "minecraft:fortress", "minecraft:bastion_remnant":
		salt, spacing, separation = 30084232, 27, 4
	case "minecraft:ruined_portal_nether":
		salt, spacing, separation = 34222645, 40, 15
	case "minecraft:nether_fossil":
		salt, spacing, separation = 14357921, 2, 1
	default:
		return world.StructureLocation{}, false, fmt.Errorf("unsupported Nether structure %q", id)
	}
	candidate, err := structure.PotentialChunk(g.seed, sectorX, sectorZ, salt, spacing, separation, structure.Linear)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	sectorX, sectorZ = candidate[0], candidate[1]
	var valid bool
	switch id {
	case "minecraft:fortress", "minecraft:bastion_remnant":
		start, err := g.cachedComplexStart(sectorX, sectorZ)
		if err != nil {
			return world.StructureLocation{}, false, err
		}
		valid = start != nil && start.name == id
	case "minecraft:ruined_portal_nether":
		start, err := g.portalStart(sectorX, sectorZ)
		if err != nil {
			return world.StructureLocation{}, false, err
		}
		valid = start != nil
	case "minecraft:nether_fossil":
		start, err := g.fossilStart(sectorX, sectorZ)
		if err != nil {
			return world.StructureLocation{}, false, err
		}
		valid = start != nil
	}
	return world.StructureLocation{ID: id, StartChunk: world.ChunkPos{sectorX, sectorZ}, Position: cube.Pos{int(sectorX * 16), 0, int(sectorZ * 16)}}, valid, nil
}

func netherLocateSpacing(structure string) int32 {
	switch structure {
	case "minecraft:fortress", "minecraft:bastion_remnant":
		return 27
	case "minecraft:ruined_portal_nether":
		return 40
	case "minecraft:nether_fossil":
		return 2
	default:
		return 1
	}
}

func (g *Nether) FindStructureAt(tag string, position cube.Pos) (world.StructureLocation, bool, error) {
	members, err := structure.StructureTagMembers(tag)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	wanted := make(map[string]bool, len(members))
	for _, member := range members {
		wanted[member] = true
	}
	chunkX, chunkZ := int32(position[0]>>4), int32(position[2]>>4)
	for _, structure := range []string{"minecraft:fortress", "minecraft:bastion_remnant", "minecraft:ruined_portal_nether", "minecraft:nether_fossil"} {
		if !wanted[structure] {
			continue
		}
		spacing := netherLocateSpacing(structure)
		// Cover a 32-chunk start window, rather than building 32 regions in
		// every direction now that lookup projects each region to its start.
		radius := (32 + spacing - 1) / spacing
		for dz := -radius; dz <= radius; dz++ {
			for dx := -radius; dx <= radius; dx++ {
				location, valid, err := g.netherLocateCandidate(structure, chunkX+dx*spacing, chunkZ+dz*spacing)
				if err != nil || !valid {
					if err != nil {
						return world.StructureLocation{}, false, err
					}
					continue
				}
				if netherLocateContains(g, structure, location.StartChunk, position) {
					return location, true, nil
				}
			}
		}
	}
	return world.StructureLocation{}, false, nil
}

func netherLocateContains(g *Nether, structure string, start world.ChunkPos, position cube.Pos) bool {
	cx, cz := int32(start[0]), int32(start[1])
	switch structure {
	case "minecraft:fortress", "minecraft:bastion_remnant":
		complex, err := g.cachedComplexStart(cx, cz)
		if err != nil || complex == nil || complex.name != structure {
			return false
		}
		if complex.fortress != nil {
			return boxContains(complex.box, position)
		}
		for _, piece := range complex.bastion.Pieces {
			if boxContains(piece.Bounds, position) {
				return true
			}
		}
	case "minecraft:ruined_portal_nether":
		portal, err := g.portalStart(cx, cz)
		return err == nil && portal != nil && boxContains(portal.box, position)
	case "minecraft:nether_fossil":
		fossil, err := g.fossilStart(cx, cz)
		return err == nil && fossil != nil && boxContains(fossil.box, position)
	}
	return false
}

func boxContains(box structure.Box, position cube.Pos) bool {
	x, y, z := int32(position[0]), int32(position[1]), int32(position[2])
	return x >= box.Min[0] && x <= box.Max[0] && y >= box.Min[1] && y <= box.Max[1] && z >= box.Min[2] && z <= box.Max[2]
}

// SpawnStructures returns the nether fortresses whose bounding boxes overlap
// the chunk, the only Nether structure overriding natural spawns.
func (g *Nether) SpawnStructures(pos world.ChunkPos) []world.StructureArea {
	const spacing = 27
	sectorX, sectorZ := floorDiv(int32(pos[0]), spacing), floorDiv(int32(pos[1]), spacing)
	var areas []world.StructureArea
	for dz := int32(-1); dz <= 1; dz++ {
		for dx := int32(-1); dx <= 1; dx++ {
			location, valid, err := g.netherLocateCandidate("minecraft:fortress", (sectorX+dx)*spacing, (sectorZ+dz)*spacing)
			if err != nil || !valid {
				continue
			}
			complex, err := g.cachedComplexStart(int32(location.StartChunk[0]), int32(location.StartChunk[1]))
			if err != nil || complex == nil || complex.fortress == nil {
				continue
			}
			minX, minZ := int32(pos[0])*16, int32(pos[1])*16
			if complex.box.Max[0] < minX || complex.box.Min[0] > minX+15 || complex.box.Max[2] < minZ || complex.box.Min[2] > minZ+15 {
				continue
			}
			area := world.StructureArea{ID: "minecraft:fortress", Box: [2]cube.Pos{
				{int(complex.box.Min[0]), int(complex.box.Min[1]), int(complex.box.Min[2])},
				{int(complex.box.Max[0]), int(complex.box.Max[1]), int(complex.box.Max[2])},
			}}
			for _, piece := range complex.fortress.pieces {
				bb := piece.pieceBase().bb
				area.Pieces = append(area.Pieces, [2]cube.Pos{{bb.minX, bb.minY, bb.minZ}, {bb.maxX, bb.maxY, bb.maxZ}})
			}
			areas = append(areas, area)
		}
	}
	return areas
}

func floorDiv(a, b int32) int32 {
	q := a / b
	if (a%b != 0) && (a < 0) != (b < 0) {
		q--
	}
	return q
}
