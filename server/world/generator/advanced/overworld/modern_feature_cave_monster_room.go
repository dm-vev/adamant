package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

var monsterRoomMobs = [...]string{
	"minecraft:skeleton",
	"minecraft:zombie",
	"minecraft:zombie",
	"minecraft:spider",
}

func placeModernMonsterRoom(region *featureRegion, origin featurePos, random *java26.Random) bool {
	xr := random.NextInt(2) + 2
	zr := random.NextInt(2) + 2
	minX, maxX := -xr-1, xr+1
	minZ, maxZ := -zr-1, zr+1

	holeCount := int32(0)
	for dx := minX; dx <= maxX; dx++ {
		for dy := int32(-1); dy <= 4; dy++ {
			for dz := minZ; dz <= maxZ; dz++ {
				p := featurePos{origin.x + dx, origin.y + dy, origin.z + dz}
				solid := featureRegionSolid(region, p)
				if dy == -1 && !solid {
					return false
				}
				if dy == 4 && !solid {
					return false
				}
				if (dx == minX || dx == maxX || dz == minZ || dz == maxZ) && dy == 0 && monsterRoomEmpty(region, p) && monsterRoomEmpty(region, featurePos{p.x, p.y + 1, p.z}) {
					holeCount++
				}
			}
		}
	}
	if holeCount < 1 || holeCount > 5 {
		return false
	}

	air := featureRequireState(region, "minecraft:cave_air")
	mossyCobblestone := featureRequireState(region, "minecraft:mossy_cobblestone")
	cobblestone := featureRequireState(region, "minecraft:cobblestone")
	for dx := minX; dx <= maxX; dx++ {
		for dy := int32(3); dy >= -1; dy-- {
			for dz := minZ; dz <= maxZ; dz++ {
				p := featurePos{origin.x + dx, origin.y + dy, origin.z + dz}
				name, _ := region.State(p)
				if dx == minX || dy == -1 || dz == minZ || dx == maxX || dy == 4 || dz == maxZ {
					if p.y >= region.MinY() && !featureRegionSolid(region, featurePos{p.x, p.y - 1, p.z}) {
						region.Set(p, air)
					} else if featureRegionSolid(region, p) && normalizeJavaResource(name) != "chest" {
						wallRID := cobblestone
						if dy == -1 && random.NextInt(4) != 0 {
							wallRID = mossyCobblestone
						}
						monsterRoomSafeSet(region, p, wallRID)
					}
				} else if normalizeJavaResource(name) != "chest" && normalizeJavaResource(name) != "spawner" {
					monsterRoomSafeSet(region, p, air)
				}
			}
		}
	}

	for range 2 {
		for range 3 {
			x := origin.x + random.NextInt(xr*2+1) - xr
			z := origin.z + random.NextInt(zr*2+1) - zr
			p := featurePos{x, origin.y, z}
			if !monsterRoomEmpty(region, p) {
				continue
			}

			wallCount := 0
			for _, d := range [...]featurePos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
				if featureRegionSolid(region, featurePos{p.x + d.x, p.y, p.z + d.z}) {
					wallCount++
				}
			}
			if wallCount != 1 {
				continue
			}

			facing := monsterRoomChestFacing(region, p)
			chestRID, ok := featureStateRID(region.c, map[string]any{"id": "minecraft:chest", "properties": map[string]any{"facing": facing, "type": "single", "waterlogged": false}})
			if !ok {
				panic("overworld: unresolved Java monster room chest block state")
			}
			if monsterRoomSafeSet(region, p, chestRID) {
				// RandomizableContainer.setBlockEntityLootTable consumes a seed from the feature stream.
				lootSeed := random.NextLong()
				monsterRoomAppendBlockEntity(region, p, map[string]any{
					"id": "minecraft:chest", "LootTable": "minecraft:chests/simple_dungeon", "LootTableSeed": lootSeed,
				})
				break
			}
		}
	}

	spawnerRID := featureRequireState(region, "minecraft:spawner")
	monsterRoomSafeSet(region, origin, spawnerRID)
	if name, _ := region.State(origin); normalizeJavaResource(name) == "spawner" {
		mob := monsterRoomMobs[random.NextInt(int32(len(monsterRoomMobs)))]
		monsterRoomAppendBlockEntity(region, origin, map[string]any{
			"id": "minecraft:mob_spawner",
			"Delay": int16(20), "MinSpawnDelay": int16(200), "MaxSpawnDelay": int16(800),
			"SpawnCount": int16(4), "MaxNearbyEntities": int16(6), "RequiredPlayerRange": int16(16), "SpawnRange": int16(4),
			"SpawnData": map[string]any{"entity": map[string]any{"id": mob}},
		})
	}
	return true
}

func monsterRoomEmpty(region *featureRegion, p featurePos) bool {
	name, _ := region.State(p)
	return normalizeJavaResource(name) == "air" || normalizeJavaResource(name) == "cave_air" || normalizeJavaResource(name) == "void_air"
}

func monsterRoomSafeSet(region *featureRegion, p featurePos, state world.NativeState) bool {
	name, _ := region.State(p)
	if javaBlockTagContains("features_cannot_replace", normalizeJavaResource(name)) {
		return false
	}
	return region.Set(p, state)
}

func monsterRoomAppendBlockEntity(region *featureRegion, p featurePos, data map[string]any) {
	if !featureInsideTarget(region.target, p) || p.y < region.MinY() || p.y > region.MaxY() {
		return
	}
	data["x"], data["y"], data["z"] = p.x, p.y, p.z
	region.result.BlockEntities = append(region.result.BlockEntities, chunk.BlockEntity{
		Pos: cube.Pos{int(p.x), int(p.y), int(p.z)}, Data: data,
	})
}

func monsterRoomChestFacing(region *featureRegion, p featurePos) string {
	locked:="north";var solidNeighbor string;found:=false
	for _,direction:=range [...]string{"north","east","south","west"}{
		neighbor:=monsterRoomNeighbor(p,direction);name,_:=region.State(neighbor)
		if normalizeJavaResource(name)=="chest"{return locked}
		if featureRegionSolid(region,neighbor){if found{solidNeighbor="";break};solidNeighbor=direction;found=true}
	}
	if solidNeighbor!=""{return monsterRoomOpposite(solidNeighbor)}
	if monsterRoomSolidAtDirection(region,p,locked){locked=monsterRoomOpposite(locked)}
	if monsterRoomSolidAtDirection(region,p,locked){locked=monsterRoomClockwise(locked)}
	if monsterRoomSolidAtDirection(region,p,locked){locked=monsterRoomOpposite(locked)}
	return locked
}
func monsterRoomNeighbor(p featurePos,direction string)featurePos{switch direction{case "north":p.z--;case "east":p.x++;case "south":p.z++;case "west":p.x--};return p}
func monsterRoomOpposite(direction string)string{switch direction{case "north":return "south";case "east":return "west";case "south":return "north";default:return "east"}}
func monsterRoomClockwise(direction string)string{switch direction{case "north":return "east";case "east":return "south";case "south":return "west";default:return "north"}}

func monsterRoomSolidAtDirection(region *featureRegion, p featurePos, direction string) bool {
	switch direction {
	case "north":
		p.z--
	case "east":
		p.x++
	case "south":
		p.z++
	case "west":
		p.x--
	}
	return featureRegionSolid(region, p)
}
