package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func placeModernLake(region *featureRegion, config map[string]any, origin featurePos, random *vanilla.Random) bool {
	if origin.y <= region.MinY()+4 {
		return false
	}
	origin = featurePos{origin.x - 8, origin.y - 4, origin.z - 8}
	var grid [16 * 16 * 8]bool
	spots := random.NextInt(4) + 4
	for range spots {
		xRadius := random.NextDouble()*6 + 3
		yRadius := random.NextDouble()*4 + 2
		zRadius := random.NextDouble()*6 + 3
		xCenter := random.NextDouble()*(16-xRadius-2) + 1 + xRadius/2
		yCenter := random.NextDouble()*(8-yRadius-4) + 2 + yRadius/2
		zCenter := random.NextDouble()*(16-zRadius-2) + 1 + zRadius/2
		for x := 1; x < 15; x++ {
			for z := 1; z < 15; z++ {
				for y := 1; y < 7; y++ {
					dx := (float64(x) - xCenter) / (xRadius / 2)
					dy := (float64(y) - yCenter) / (yRadius / 2)
					dz := (float64(z) - zCenter) / (zRadius / 2)
					if dx*dx+dy*dy+dz*dz < 1 {
						grid[(x*16+z)*8+y] = true
					}
				}
			}
		}
	}
	fluidState, fluidActor, ok := region.SampleNativeState(config["fluid"], origin, random)
	if !ok {
		panic("overworld: unresolved lake fluid provider")
	}
	airState := featureRequireState(region, "minecraft:air")
	caveAirState := featureRequireState(region, "minecraft:cave_air")
	fluidRID := fluidState.Liquid
	if fluidRID == airState.Block {
		fluidRID = fluidState.Block
	}
	fluidName, _, found := region.c.BlockRegistry().RuntimeIDToState(fluidRID)
	if !found {
		panic("overworld: invalid lake fluid runtime ID")
	}
	fluidIsWater := featureFluidTagContains("minecraft:water", normalizeResource(fluidName))
	canPlace, ok := config["can_place_feature"].(map[string]any)
	if !ok {
		panic("overworld: malformed lake placement predicate")
	}
	canAirFluid, ok := config["can_replace_with_air_or_fluid"].(map[string]any)
	if !ok {
		panic("overworld: malformed lake air/fluid predicate")
	}
	canBarrier, ok := config["can_replace_with_barrier"].(map[string]any)
	if !ok {
		panic("overworld: malformed lake barrier predicate")
	}
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				if grid[(x*16+z)*8+y] || !lakeGridEdge(&grid, x, y, z) {
					continue
				}
				p := featurePos{origin.x + int32(x), origin.y + int32(y), origin.z + int32(z)}
				stateNative := region.nativeState(p)
				blockName, _, stateExists := region.c.BlockRegistry().RuntimeIDToState(stateNative.Block)
				if y >= 4 && stateExists && isLiquidBlock(blockName) {
					return false
				}
				if y < 4 && !featureRegionSolid(region, p) && stateNative != fluidState {
					return false
				}
				if !featurePredicate(region, p, canPlace) {
					return false
				}
			}
		}
	}
	for x := range 16 {
		for z := range 16 {
			for y := range 8 {
				if !grid[(x*16+z)*8+y] {
					continue
				}
				p := featurePos{origin.x + int32(x), origin.y + int32(y), origin.z + int32(z)}
				if !featurePredicate(region, p, canAirFluid) {
					continue
				}
				if y >= 4 {
					region.setWithFlags(p, caveAirState, 2)
					region.scheduleTick(chunk.ScheduledTick{
						Pos: cube.Pos{int(p.x), int(p.y), int(p.z)}, Kind: chunk.BlockTick, Type: "minecraft:cave_air",
					})
					region.markAboveForPostProcessing(p)
				} else {
					featureSetNativeBlockFlag2(region, p, fluidState, fluidActor)
				}
			}
		}
	}
	barrierProvider, ok := config["barrier"].(map[string]any)
	if !ok {
		panic("overworld: malformed lake barrier provider")
	}
	barrierState, barrierActor, ok := region.SampleNativeState(barrierProvider, origin, random)
	if !ok {
		panic("overworld: unresolved lake barrier provider")
	}
	barrierCanonical, err := world.CanonicalState(region.c.BlockRegistry(), barrierState)
	if err != nil {
		panic(err)
	}
	if normalizeResource(barrierCanonical.Name) != "air" && normalizeResource(barrierCanonical.Name) != "cave_air" && normalizeResource(barrierCanonical.Name) != "void_air" {
		for x := range 16 {
			for z := range 16 {
				for y := range 8 {
					if grid[(x*16+z)*8+y] || !lakeGridEdge(&grid, x, y, z) {
						continue
					}
					if y >= 4 && random.NextInt(2) == 0 {
						continue
					}
					p := featurePos{origin.x + int32(x), origin.y + int32(y), origin.z + int32(z)}
					if featureRegionSolid(region, p) && featurePredicate(region, p, canBarrier) {
						featureSetNativeBlockFlag2(region, p, barrierState, barrierActor)
						region.markAboveForPostProcessing(p)
					}
				}
			}
		}
	}
	if fluidIsWater {
		for x := range 16 {
			for z := range 16 {
				p := featurePos{origin.x + int32(x), origin.y + 4, origin.z + int32(z)}
				if region.biomeShouldFreeze(p, false) && featurePredicate(region, p, canAirFluid) {
					ice := featureRequireState(region, "minecraft:ice")
					region.setWithFlags(p, ice, 2)
				}
			}
		}
	}
	return true
}

func lakeGridEdge(grid *[16 * 16 * 8]bool, x, y, z int) bool {
	return x < 15 && grid[((x+1)*16+z)*8+y] || x > 0 && grid[((x-1)*16+z)*8+y] || z < 15 && grid[(x*16+z+1)*8+y] || z > 0 && grid[(x*16+z-1)*8+y] || y < 7 && grid[(x*16+z)*8+y+1] || y > 0 && grid[(x*16+z)*8+y-1]
}

type featureRegionBlockSource struct{ region *featureRegion }

func (s featureRegionBlockSource) Block(pos cube.Pos) world.Block {
	return s.region.Block(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])})
}
func featureRegionSolid(region *featureRegion, p featurePos) bool {
	state := region.nativeState(p)
	solid, ok := gamedata.BlockStateIsSolid(state.CanonicalID(region.c.BlockRegistry()))
	return ok && solid
}
