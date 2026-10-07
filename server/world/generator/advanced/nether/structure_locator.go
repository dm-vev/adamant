package nether

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
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
	members, err := javastructure.StructureTagMembers(tag)
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
				for _, structure := range []string{"minecraft:fortress", "minecraft:bastion_remnant", "minecraft:ruined_portal", "minecraft:nether_fossil"} {
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

func (g *Nether) netherLocateCandidate(structure string, sectorX, sectorZ int32) (world.StructureLocation, bool, error) {
	var salt, spacing, separation int32
	distribution := javastructure.Linear
	stepIndex, step := 0, 0
	switch structure {
	case "minecraft:fortress", "minecraft:bastion_remnant":
		salt, spacing, separation = 30084232, 27, 4
	case "minecraft:ruined_portal":
		salt, spacing, separation, distribution = 34222645, 40, 15, javastructure.Triangular
	case "minecraft:nether_fossil":
		salt, spacing, separation = 14357921, 2, 1
	default:
		return world.StructureLocation{}, false, fmt.Errorf("unsupported Nether structure %q", structure)
	}
	candidate, err := javastructure.PotentialChunk(g.seed, sectorX, sectorZ, salt, spacing, separation, distribution)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	if candidate[0] != sectorX || candidate[1] != sectorZ {
		return world.StructureLocation{}, false, nil
	}
	var valid bool
	switch structure {
	case "minecraft:fortress":
		stepIndex, step = 1, 7
		random := g.featureSource.structureStepRandom(world.ChunkPos{sectorX, sectorZ}, stepIndex, step)
		valid = g.netherBridgeStructure(int(sectorX), int(sectorZ), random) != nil
	case "minecraft:bastion_remnant":
		stepIndex, step = 31, 4
		random := g.featureSource.structureStepRandom(world.ChunkPos{sectorX, sectorZ}, stepIndex, step)
		if random.NextInt(5) >= 2 {
			startX, startZ := sectorX*16, sectorZ*16
			if g.featureSource.JavaBiomeAt(startX, 33, startZ) != "minecraft:basalt_deltas" {
				jigsaw, err := javastructure.BuildJigsaw(javastructure.JigsawOptions{
					Pool: "bastion/starts", Origin: [3]int32{startX, 33, startZ}, MaxDepth: 6,
					MaxDistance: 80, Seed: g.seed, Terrain: javastructure.TerrainNone,
					HeightAt: func(x, z int32) int32 { return g.featureSource.BaseHeight(x, z, false) },
				}, random)
				valid = err == nil && len(jigsaw.Pieces) != 0
				if err != nil {
					return world.StructureLocation{}, false, fmt.Errorf("build Nether bastion at %d,%d: %w", sectorX, sectorZ, err)
				}
			}
		}
	case "minecraft:ruined_portal":
		stepIndex, step = 26, 4
		random := g.featureSource.structureStepRandom(world.ChunkPos{sectorX, sectorZ}, stepIndex, step)
		if random.NextInt(7) == 6 {
			clip := javastructure.Bounds{Min: [3]int32{sectorX * 16, 0, sectorZ * 16}, Max: [3]int32{sectorX*16 + 15, 127, sectorZ*16 + 15}}
			env := g.netherLocateEnvironment(random)
			_, valid = g.featureSource.buildNetherPortal(candidate, clip, env)
		}
	case "minecraft:nether_fossil":
		stepIndex, step = 2, 7
		random := g.featureSource.structureStepRandom(world.ChunkPos{sectorX, sectorZ}, stepIndex, step)
		x, z := sectorX*16+random.NextInt(16), sectorZ*16+random.NextInt(16)
		y := int32(32) + random.NextInt(94)
		for y > netherSeaLevel {
			currentName, _ := g.featureSource.BaseBlock(x, y, z).EncodeBlock()
			y--
			belowName, _ := g.featureSource.BaseBlock(x, y, z).EncodeBlock()
			sturdy := belowName != "minecraft:air" && belowName != "minecraft:lava" && belowName != "minecraft:flowing_lava"
			if currentName == "minecraft:air" && (belowName == "minecraft:soul_sand" || sturdy) {
				break
			}
		}
		valid = y > netherSeaLevel && g.featureSource.JavaBiomeAt(x, y, z) == "minecraft:soul_sand_valley"
	}
	return world.StructureLocation{ID: structure, StartChunk: world.ChunkPos{sectorX, sectorZ}, Position: cube.Pos{int(sectorX * 16), 0, int(sectorZ * 16)}}, valid, nil
}

func (g *Nether) netherLocateEnvironment(random *java26.Random) javastructure.ProcessorEnvironment {
	return javastructure.ProcessorEnvironment{BaseBlock: g.featureSource.BaseBlock, JavaBiomeAt: g.featureSource.JavaBiomeAt, BaseHeight: g.featureSource.BaseHeight, WorldSeed: g.seed, Random: random}
}

func netherLocateSpacing(structure string) int32 {
	switch structure {
	case "minecraft:fortress", "minecraft:bastion_remnant":
		return 27
	case "minecraft:ruined_portal":
		return 40
	case "minecraft:nether_fossil":
		return 2
	default:
		return 1
	}
}

func (g *Nether) FindStructureAt(tag string, position cube.Pos) (world.StructureLocation, bool, error) {
	members, err := javastructure.StructureTagMembers(tag)
	if err != nil {
		return world.StructureLocation{}, false, err
	}
	wanted := make(map[string]bool, len(members))
	for _, member := range members {
		wanted[member] = true
	}
	chunkX, chunkZ := int32(position[0]>>4), int32(position[2]>>4)
	for _, structure := range []string{"minecraft:fortress", "minecraft:bastion_remnant", "minecraft:ruined_portal", "minecraft:nether_fossil"} {
		if !wanted[structure] {
			continue
		}
		spacing := netherLocateSpacing(structure)
		for dz := int32(-32); dz <= 32; dz++ {
			for dx := int32(-32); dx <= 32; dx++ {
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
	case "minecraft:fortress":
		random := g.featureSource.structureStepRandom(start, 1, 7)
		bridge := g.netherBridgeStructure(int(cx), int(cz), random)
		if bridge == nil {
			return false
		}
		return netherBBContains(bridge.bb.minX, bridge.bb.minY, bridge.bb.minZ, bridge.bb.maxX, bridge.bb.maxY, bridge.bb.maxZ, position)
	case "minecraft:bastion_remnant":
		random := g.featureSource.structureStepRandom(start, 31, 4)
		if random.NextInt(5) < 2 || g.featureSource.JavaBiomeAt(cx*16, 33, cz*16) == "minecraft:basalt_deltas" {
			return false
		}
		jigsaw, err := javastructure.BuildJigsaw(javastructure.JigsawOptions{Pool: "bastion/starts", Origin: [3]int32{cx * 16, 33, cz * 16}, MaxDepth: 6, MaxDistance: 80, Seed: g.seed, Terrain: javastructure.TerrainNone, HeightAt: func(x, z int32) int32 { return g.featureSource.BaseHeight(x, z, false) }}, random)
		if err != nil {
			return false
		}
		for _, piece := range jigsaw.Pieces {
			if boxContains(piece.Bounds, position) {
				return true
			}
		}
	case "minecraft:ruined_portal":
		random := g.featureSource.structureStepRandom(start, 26, 4)
		if random.NextInt(7) != 6 {
			return false
		}
		clip := javastructure.Bounds{Min: [3]int32{cx * 16, 0, cz * 16}, Max: [3]int32{cx*16 + 15, 127, cz*16 + 15}}
		placement, valid := g.featureSource.buildNetherPortal([2]int32{cx, cz}, clip, g.netherLocateEnvironment(random))
		if !valid {
			return false
		}
		if len(placement.Blocks) == 0 {
			return false
		}
		minPos, maxPos := placement.Blocks[0].Pos, placement.Blocks[0].Pos
		for _, block := range placement.Blocks[1:] {
			for axis := range 3 {
				minPos[axis] = min(minPos[axis], block.Pos[axis])
				maxPos[axis] = max(maxPos[axis], block.Pos[axis])
			}
		}
		return int32(position[0]) >= minPos[0] && int32(position[0]) <= maxPos[0] && int32(position[1]) >= minPos[1] && int32(position[1]) <= maxPos[1] && int32(position[2]) >= minPos[2] && int32(position[2]) <= maxPos[2]
	case "minecraft:nether_fossil":
		random := g.featureSource.structureStepRandom(start, 2, 7)
		x, z := cx*16+random.NextInt(16), cz*16+random.NextInt(16)
		y := int32(32) + random.NextInt(94)
		for y > netherSeaLevel {
			currentName, _ := g.featureSource.BaseBlock(x, y, z).EncodeBlock()
			y--
			belowName, _ := g.featureSource.BaseBlock(x, y, z).EncodeBlock()
			sturdy := belowName != "minecraft:air" && belowName != "minecraft:lava" && belowName != "minecraft:flowing_lava"
			if currentName == "minecraft:air" && (belowName == "minecraft:soul_sand" || sturdy) {
				break
			}
		}
		if y <= netherSeaLevel || g.featureSource.JavaBiomeAt(x, y, z) != "minecraft:soul_sand_valley" {
			return false
		}
		rotation := javastructure.Rotation(random.NextInt(4))
		template := loadNetherTemplate(fmt.Sprintf("nether_fossils/fossil_%d", random.NextInt(14)+1))
		min, max := netherTemplateBounds(template, [3]int32{x, y, z}, rotation)
		return int32(position[0]) >= min[0] && int32(position[0]) <= max[0] && int32(position[1]) >= min[1] && int32(position[1]) <= max[1] && int32(position[2]) >= min[2] && int32(position[2]) <= max[2]
	}
	return false
}

func netherBBContains(minX, minY, minZ, maxX, maxY, maxZ int, position cube.Pos) bool {
	return int(position[0]) >= minX && int(position[0]) <= maxX && int(position[1]) >= minY && int(position[1]) <= maxY && int(position[2]) >= minZ && int(position[2]) <= maxZ
}

func boxContains(box javastructure.Box, position cube.Pos) bool {
	x, y, z := int32(position[0]), int32(position[1]), int32(position[2])
	return x >= box.Min[0] && x <= box.Max[0] && y >= box.Min[1] && y <= box.Max[1] && z >= box.Min[2] && z <= box.Max[2]
}
