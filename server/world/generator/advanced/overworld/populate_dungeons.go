package overworld

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/mc112"
)

func (g *Overworld) populateDungeons(tx *world.Tx, r *mc112.Rand, chunkX, chunkZ int, origin cube.Pos) {
	// Matches the default dungeonChance setting used by ChunkGeneratorSettings in Java 1.12.
	const dungeonChance = 8

	for i := 0; i < dungeonChance; i++ {
		x := origin[0] + int(r.Intn(16))
		y := int(r.Intn(256))
		z := origin[2] + int(r.Intn(16))
		g.generateDungeon(tx, r, chunkX, chunkZ, cube.Pos{x, y, z})
	}
}

func (g *Overworld) generateDungeon(tx *world.Tx, r *mc112.Rand, chunkX, chunkZ int, pos cube.Pos) bool {
	if pos.OutOfBounds(tx.Range()) {
		return false
	}
	if pos[0]>>4 != chunkX || pos[2]>>4 != chunkZ {
		return false
	}
	roomX := int(r.Intn(2)) + 2
	roomZ := int(r.Intn(2)) + 2

	minX, maxX := pos[0]-roomX-1, pos[0]+roomX+1
	minZ, maxZ := pos[2]-roomZ-1, pos[2]+roomZ+1
	if minX>>4 != chunkX || maxX>>4 != chunkX || minZ>>4 != chunkZ || maxZ>>4 != chunkZ {
		return false
	}

	var openings int
	for x := minX; x <= maxX; x++ {
		for y := pos[1] - 1; y <= pos[1]+3; y++ {
			for z := minZ; z <= maxZ; z++ {
				p := cube.Pos{x, y, z}
				if p.OutOfBounds(tx.Range()) {
					return false
				}

				rid := world.BlockRuntimeID(tx.Block(p))
				solid := dungeonSolid(rid)

				if y == pos[1]-1 && !solid {
					return false
				}
				if y == pos[1]+3 && !solid {
					return false
				}

				if y == pos[1] && (x == minX || x == maxX || z == minZ || z == maxZ) {
					if dungeonAir(world.BlockRuntimeID(tx.Block(p))) && dungeonAir(world.BlockRuntimeID(tx.Block(p.Side(cube.FaceUp)))) {
						openings++
					}
				}
			}
		}
	}
	if openings < 1 || openings > 5 {
		return false
	}

	cobble := block.Cobblestone{}
	mossy := block.Cobblestone{Mossy: true}
	air := (world.Block)(nil)

	for x := minX; x <= maxX; x++ {
		for y := pos[1] + 3; y >= pos[1]-1; y-- {
			for z := minZ; z <= maxZ; z++ {
				p := cube.Pos{x, y, z}
				if p.OutOfBounds(tx.Range()) {
					continue
				}
				if p[0]>>4 != chunkX || p[2]>>4 != chunkZ {
					continue
				}

				boundaryXZ := x == minX || x == maxX || z == minZ || z == maxZ
				switch {
				case y == pos[1]-1:
					// Floor: preserve voids (e.g., caves) by only placing where the block below is solid.
					if dungeonSolid(world.BlockRuntimeID(tx.Block(p.Side(cube.FaceDown)))) {
						tx.SetBlock(p, cobble, populationSetOpts)
					} else {
						tx.SetBlock(p, air, populationSetOpts)
					}

				case y == pos[1]+3:
					// Ceiling.
					if boundaryXZ || dungeonSolid(world.BlockRuntimeID(tx.Block(p))) {
						tx.SetBlock(p, cobble, populationSetOpts)
					}

				case boundaryXZ:
					// Keep openings where the room connects to caves.
					if y == pos[1] && dungeonAir(world.BlockRuntimeID(tx.Block(p))) && dungeonAir(world.BlockRuntimeID(tx.Block(p.Side(cube.FaceUp)))) {
						tx.SetBlock(p, air, populationSetOpts)
						continue
					}
					if r.Intn(4) == 0 {
						tx.SetBlock(p, mossy, populationSetOpts)
					} else {
						tx.SetBlock(p, cobble, populationSetOpts)
					}

				default:
					tx.SetBlock(p, air, populationSetOpts)
				}
			}
		}
	}

	// Up to two chests.
	for i := 0; i < 2; i++ {
		for tries := 0; tries < 3; tries++ {
			x := pos[0] + int(r.Intn(int32(roomX*2+1))) - roomX
			y := pos[1]
			z := pos[2] + int(r.Intn(int32(roomZ*2+1))) - roomZ
			p := cube.Pos{x, y, z}
			if p.OutOfBounds(tx.Range()) {
				continue
			}
			if p[0]>>4 != chunkX || p[2]>>4 != chunkZ {
				continue
			}
			if !dungeonAir(world.BlockRuntimeID(tx.Block(p))) {
				continue
			}

			solidSides := 0
			for _, face := range cube.HorizontalFaces() {
				if dungeonSolid(world.BlockRuntimeID(tx.Block(p.Side(face)))) {
					solidSides++
				}
			}
			if solidSides != 1 {
				continue
			}

			ch := block.NewChest()
			ch.Facing = cube.FaceNorth.Direction()
			tx.SetBlock(p, ch, populationSetOpts)
			break
		}
	}

	// Spawner in the centre of the room. The mob type is stored in a block entity in vanilla, but the server
	// doesn't implement spawners yet, so place the block state only.
	if spawner, ok := world.BlockByName("minecraft:mob_spawner", nil); ok {
		tx.SetBlock(pos, spawner, populationSetOpts)
	}

	return true
}

func dungeonAir(rid uint32) bool {
	name, _, ok := chunk.RuntimeIDToState(rid)
	return ok && name == "minecraft:air"
}

func dungeonSolid(rid uint32) bool {
	name, _, ok := chunk.RuntimeIDToState(rid)
	if !ok {
		return false
	}
	switch name {
	case "minecraft:air", "minecraft:water", "minecraft:flowing_water", "minecraft:lava", "minecraft:flowing_lava":
		return false
	default:
		return true
	}
}
