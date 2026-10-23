package overworld

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type endFeatureStateIDs struct {
	air, bedrock, endStone, obsidian, portal, gateway uint32
	wallTorches                                       [4]uint32
	ironBars                                          [16]uint32
}

var modernEndFeatureIDs = func() endFeatureStateIDs {
	literal := func(name string, properties map[string]any) uint32 {
		id, err := gamedata.BlockStateID("minecraft:"+name, properties)
		if err != nil {
			panic(err)
		}
		return id
	}
	ids := endFeatureStateIDs{
		air: literal("air", nil), bedrock: literal("bedrock", nil),
		endStone: literal("end_stone", nil), obsidian: literal("obsidian", nil),
		portal: literal("end_portal", nil), gateway: literal("end_gateway", nil),
	}
	for i, facing := range [...]string{"north", "east", "south", "west"} {
		ids.wallTorches[i] = literal("wall_torch", map[string]any{"facing": facing})
	}
	for mask := range 16 {
		ids.ironBars[mask] = literal("iron_bars", map[string]any{
			"north": mask&1 != 0, "east": mask&2 != 0,
			"south": mask&4 != 0, "west": mask&8 != 0,
		})
	}
	return ids
}()

func endFeatureBlockName(region *featureRegion, pos cube.Pos) string {
	id, err := region.BlockStateID(pos)
	if err != nil {
		panic(err)
	}
	name, ok := gamedata.BlockStateName(id)
	if !ok {
		panic("overworld: invalid End feature state")
	}
	return name
}

func placeModernEndIsland(region *featureRegion, origin featurePos, random *vanilla.Random) bool {
	centre := cube.Pos{int(origin.x), int(origin.y), int(origin.z)}
	size := float32(random.NextInt(3)) + 4
	for y := 0; size > 0.5; y-- {
		low, high := int(math.Floor(float64(-size))), int(math.Ceil(float64(size)))
		radiusSquared := (size + 1) * (size + 1)
		for x := low; x <= high; x++ {
			for z := low; z <= high; z++ {
				if float32(x*x+z*z) <= radiusSquared {
					region.SetBlockState(centre.Add(cube.Pos{x, y, z}), modernEndFeatureIDs.endStone, 3)
				}
			}
		}
		size -= float32(random.NextInt(2)) + 0.5
	}
	return true
}

func placeModernEndPlatform(region *featureRegion, origin featurePos) bool {
	centre := cube.Pos{int(origin.x), int(origin.y), int(origin.z)}
	for z := -2; z <= 2; z++ {
		for x := -2; x <= 2; x++ {
			for y := -1; y < 3; y++ {
				pos := centre.Add(cube.Pos{x, y, z})
				state, name := modernEndFeatureIDs.air, "minecraft:air"
				if y == -1 {
					state, name = modernEndFeatureIDs.obsidian, "minecraft:obsidian"
				}
				if endFeatureBlockName(region, pos) != name {
					region.SetBlockState(pos, state, 3)
				}
			}
		}
	}
	return true
}

func placeModernEndPodium(region *featureRegion, config map[string]any, origin featurePos) bool {
	centre := cube.Pos{int(origin.x), int(origin.y), int(origin.z)}
	active := config["active"] == true
	set := func(pos cube.Pos, state uint32) {
		if active {
			name, _ := gamedata.BlockStateName(state)
			previous := endFeatureBlockName(region, pos)
			if previous == name {
				return
			}
			// WorldGenRegion.destroyBlock does not produce survival drops.
			if previous != "minecraft:air" && previous != "minecraft:cave_air" && previous != "minecraft:void_air" {
				region.SetBlockState(pos, modernEndFeatureIDs.air, 3)
			}
		}
		region.SetBlockState(pos, state, 3)
	}
	for z := -4; z <= 4; z++ {
		for y := -1; y <= 32; y++ {
			for x := -4; x <= 4; x++ {
				distanceSquared := float64(x*x + y*y + z*z)
				if distanceSquared >= 3.5*3.5 {
					continue
				}
				inner := distanceSquared < 2.5*2.5
				state := modernEndFeatureIDs.air
				switch {
				case y < 0 && inner, y == 0 && !inner:
					state = modernEndFeatureIDs.bedrock
				case y < 0:
					state = modernEndFeatureIDs.endStone
				case y == 0 && active:
					state = modernEndFeatureIDs.portal
				}
				set(centre.Add(cube.Pos{x, y, z}), state)
			}
		}
	}
	for y := range 4 {
		set(centre.Add(cube.Pos{0, y, 0}), modernEndFeatureIDs.bedrock)
	}
	for i, offset := range [...]cube.Pos{{0, 2, -1}, {1, 2, 0}, {0, 2, 1}, {-1, 2, 0}} {
		set(centre.Add(offset), modernEndFeatureIDs.wallTorches[i])
	}
	return true
}

func placeModernEndGateway(region *featureRegion, config map[string]any, origin featurePos) bool {
	centre := cube.Pos{int(origin.x), int(origin.y), int(origin.z)}
	for z := -1; z <= 1; z++ {
		for y := -2; y <= 2; y++ {
			for x := -1; x <= 1; x++ {
				sameX, sameY, sameZ := x == 0, y == 0, z == 0
				end := y == -2 || y == 2
				state := modernEndFeatureIDs.air
				switch {
				case sameX && sameY && sameZ:
					state = modernEndFeatureIDs.gateway
				case sameY:
				case end && sameX && sameZ, !end && (sameX || sameZ):
					state = modernEndFeatureIDs.bedrock
				}
				pos := centre.Add(cube.Pos{x, y, z})
				region.SetBlockState(pos, state, 3)
				if state == modernEndFeatureIDs.gateway {
					if raw, hasExit := config["exit"]; hasExit && endFeatureBlockName(region, pos) == "minecraft:end_gateway" {
						exit := raw.([]any)
						data := map[string]any{
							"id": "minecraft:end_gateway", "Age": int64(0),
							"exit_portal":   []int32{intProviderValue(exit[0]), intProviderValue(exit[1]), intProviderValue(exit[2])},
							"ExactTeleport": config["exact"].(bool),
						}
						if existing := region.templateActorData(pos); existing != nil {
							data["Age"] = existing["Age"]
						}
						featureRecordBlockEntity(region, featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}, data)
					}
				}
			}
		}
	}
	return true
}

func placeModernChorusPlant(region *featureRegion, origin featurePos, random *vanilla.Random) bool {
	pos := cube.Pos{int(origin.x), int(origin.y), int(origin.z)}
	name := endFeatureBlockName(region, pos)
	if (name != "minecraft:air" && name != "minecraft:cave_air" && name != "minecraft:void_air") ||
		!blockTagContains("minecraft:supports_chorus_plant", endFeatureBlockName(region, pos.Side(cube.FaceDown))) {
		return false
	}
	world.GenerateChorusPlant(region, pos, random, 8)
	return true
}

func placeModernEndSpike(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	rawSpikes := config["spikes"].([]any)
	if len(rawSpikes) == 0 {
		for _, spike := range vanilla.EndSpikes(region.f.seed) {
			placeModernEndSpikeColumn(region, config, origin, spike, random)
		}
	} else {
		for _, raw := range rawSpikes {
			data := raw.(map[string]any)
			spike := vanilla.EndSpike{
				X: endSpikeCoordinate(data, "centerX"), Z: endSpikeCoordinate(data, "centerZ"),
				Radius: endSpikeCoordinate(data, "radius"), Height: endSpikeCoordinate(data, "height"),
				Guarded: data["guarded"] == true,
			}
			placeModernEndSpikeColumn(region, config, origin, spike, random)
		}
	}
	return true
}

func endSpikeCoordinate(data map[string]any, name string) int {
	if raw, exists := data[name]; exists {
		return int(intProviderValue(raw))
	}
	return 0
}

func placeModernEndSpikeColumn(region *featureRegion, config map[string]any, origin featurePos, spike vanilla.EndSpike, random *vanilla.Random) {
	if spike.X>>4 != int(origin.x>>4) || spike.Z>>4 != int(origin.z>>4) {
		return
	}
	// BlockPos.betweenClosed normalizes its two corners before its x/y/z
	// iteration. The radius product retains Java's signed int overflow.
	x0, x1 := int32(spike.X)-int32(spike.Radius), int32(spike.X)+int32(spike.Radius)
	z0, z1 := int32(spike.Z)-int32(spike.Radius), int32(spike.Z)+int32(spike.Radius)
	y0, y1 := region.MinY(), int32(spike.Height)+10
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if z0 > z1 {
		z0, z1 = z1, z0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	radiusSquared := int32(spike.Radius)*int32(spike.Radius) + 1
	width, height, depth := x1-x0+1, y1-y0+1, z1-z0+1
	end := width * height * depth
	for index := int32(0); index != end; index++ {
		slice := index / width
		x, y, z := x0+index%width, y0+slice%height, z0+slice/height
		dx, dz := float64(x)-float64(spike.X), float64(z)-float64(spike.Z)
		pos := cube.Pos{int(x), int(y), int(z)}
		if dx*dx+dz*dz <= float64(radiusSquared) && y < int32(spike.Height) {
			region.SetBlockState(pos, modernEndFeatureIDs.obsidian, 3)
		} else if y > 65 {
			region.SetBlockState(pos, modernEndFeatureIDs.air, 3)
		}
	}
	if spike.Guarded {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				for y := range 4 {
					xSide, zSide, top := x == -2 || x == 2, z == -2 || z == 2, y == 3
					if !xSide && !zSide && !top {
						continue
					}
					xEdge, zEdge := xSide || top, zSide || top
					mask := 0
					if xEdge && z != -2 {
						mask |= 1
					}
					if zEdge && x != 2 {
						mask |= 2
					}
					if xEdge && z != 2 {
						mask |= 4
					}
					if zEdge && x != -2 {
						mask |= 8
					}
					region.SetBlockState(cube.Pos{int(int32(spike.X) + int32(x)), int(int32(spike.Height) + int32(y)), int(int32(spike.Z) + int32(z))}, modernEndFeatureIDs.ironBars[mask], 3)
				}
			}
		}
	}
	yaw := float64(random.NextFloat() * 360)
	pos := cube.Pos{spike.X, int(int32(spike.Height) + 1), spike.Z}
	if region.recordsAt(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}) {
		crystal := entity.EndCrystalBehaviourConfig{ShowBase: true, PermanentlyInvulnerable: config["crystal_invulnerable"] == true}
		if raw, exists := config["crystal_beam_target"]; exists {
			target := raw.([]any)
			beam := mgl64.Vec3{float64(intProviderValue(target[0])), float64(intProviderValue(target[1])), float64(intProviderValue(target[2]))}
			crystal.BeamTarget = &beam
		}
		opts := world.EntitySpawnOpts{
			Position: mgl64.Vec3{float64(spike.X) + 0.5, float64(pos[1]), float64(spike.Z) + 0.5},
			Rotation: cube.Rotation{yaw, 0}, SpawnReason: "STRUCTURE",
		}
		region.addEntity(pos, func() *world.EntityHandle { return entity.NewEndCrystal(opts, crystal) })
	}
	region.SetBlockState(cube.Pos{spike.X, spike.Height, spike.Z}, modernEndFeatureIDs.bedrock, 3)
	region.SetBlockState(pos, world.GenerationFireState(region, pos), 3)
}
