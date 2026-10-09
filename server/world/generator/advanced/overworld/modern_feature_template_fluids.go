package overworld

import (
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// applyTemplateFluids matches StructureTemplate's post-write waterlogging pass.
// Native secondary layers retain the actual fluid kind and source/flowing state.
func applyTemplateFluids(region *featureRegion, blocks []javastructure.Block) []javastructure.Block {
	return applyTemplateFluidsForStructure(region, blocks, false)
}

func applyTemplateFluidsForStructure(region *featureRegion, blocks []javastructure.Block, structurePieceWrites bool) []javastructure.Block {
	payload, _ := applyTemplateFluidsForStructureWrites(region, blocks, structurePieceWrites, 3)
	return payload
}

func applyTemplateFluidsForStructureWrites(region *featureRegion, blocks []javastructure.Block, structurePieceWrites bool, flags int) ([]javastructure.Block, []cube.Pos) {
	registry, ok := region.c.BlockRegistry().(world.BlockRegistry)
	if !ok {
		panic("overworld: Java feature region registry cannot resolve native blocks")
	}
	var toFill []featurePos
	var locked []featurePos
	var payloadBlocks []javastructure.Block
	var written []cube.Pos
	applyWaterlogging := len(blocks) == 0 || blocks[0].LiquidSettings != javastructure.IgnoreWaterlogging
	for i := range blocks {
		entry := &blocks[i]
		pos := featurePos{entry.Pos[0], entry.Pos[1], entry.Pos[2]}
		if pos.y < region.MinY() || pos.y > region.MaxY() || absFeatureInt(pos.x>>4-region.writeOriginX) > 1 || absFeatureInt(pos.z>>4-region.writeOriginZ) > 1 {
			continue
		}
		entry.State.Properties = javadata.BlockPropertiesWithDefaults(entry.State.Name, entry.State.Properties)
		previous := region.nativeState(pos)
		state, err := entry.ResolveNative(registry, previous)
		if err != nil {
			panic(err)
		}
		if !region.setWithFlags(pos, state, flags) {
			continue
		}
		written = append(written, cube.Pos{int(pos.x), int(pos.y), int(pos.z)})
		if structurePieceWrites && featureInsideTarget(region.target, pos) {
			state := region.nativeState(pos)
			for _, rid := range [2]uint32{state.Block, state.Liquid} {
				if !registry.LiquidBlock(rid) {
					continue
				}
				fluidBlock, ok := registry.BlockByRuntimeID(rid)
				if !ok {
					panic("overworld: unknown structure fluid runtime ID")
				}
				fluid, ok := fluidBlock.(world.Liquid)
				if !ok {
					panic("overworld: structure liquid block has no fluid type")
				}
				typeName := fluid.LiquidType()
				if !strings.Contains(typeName, ":") {
					typeName = "minecraft:" + typeName
				}
				region.reserveJavaTick(chunk.ScheduledTick{
					Pos: cube.Pos{int(pos.x), int(pos.y), int(pos.z)}, Kind: chunk.FluidTick, Type: typeName,
				})
				break
			}
			if structurePieceShapeCheckBlock(state, registry) {
				region.result.MarkForPostProcessing(cube.Pos{int(pos.x), int(pos.y), int(pos.z)}, region.c.Range())
			}
		}
		if !applyWaterlogging {
			if entry.NativeBlock != nil || len(entry.NBT) != 0 {
				payloadBlocks = append(payloadBlocks, *entry)
			}
			continue
		}
		if templateBlockStateIsSource(entry.State) {
			locked = append(locked, pos)
			if entry.NativeBlock != nil || len(entry.NBT) != 0 {
				payloadBlocks = append(payloadBlocks, *entry)
			}
			continue
		}
		if templateContainerState(entry.State) {
			previousFluid, _ := templateFluid(registry, previous)
			previousSource := previousFluid != nil && previousFluid.LiquidDepth() == 8 && !previousFluid.LiquidFalling()
			if previousSource && templateWaterFluid(previousFluid) && !templateDoubleSlab(registry, state) && templateContainerWaterloggable(entry.State) {
				wet := templateWaterloggedBlock(*entry)
				state, err = wet.ResolveNative(registry, previous)
				if err != nil {
					panic(err)
				}
				if !region.setWithFlags(pos, state, flags) {
					continue
				}
				*entry = wet
				if featureInsideTarget(region.target, pos) {
					region.reserveJavaTick(chunk.ScheduledTick{
						Pos: cube.Pos{int(pos.x), int(pos.y), int(pos.z)}, Kind: chunk.FluidTick, Type: "minecraft:water",
					})
				}
			}
			// Java queues EMPTY and flowing fluid too; only source fluids skip.
			if !previousSource {
				toFill = append(toFill, pos)
			}
		}
		if entry.NativeBlock != nil || len(entry.NBT) != 0 {
			payloadBlocks = append(payloadBlocks, *entry)
		}
	}

	// Java checks UP, NORTH, EAST, SOUTH, WEST and repeats while a source
	// successfully fills a remaining LiquidBlockContainer. Only template state
	// sources are locked; fluids created during this pass may propagate.
	for filled := true; filled && len(toFill) != 0; {
		filled = false
		remaining := toFill[:0]
		for _, pos := range toFill {
			current := region.nativeState(pos)
			if templateFluidIsSource(registry, current) {
				continue
			}
			currentJava, err := world.JavaState(registry, current)
			if err != nil {
				panic(err)
			}
			if !templateContainerState(currentJava) {
				remaining = append(remaining, pos)
				continue
			}
			foundSource, waterSource := false, false
			for _, d := range [...]featurePos{{0, 1, 0}, {0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
				neighbor := featurePos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
				if neighbor.y < region.MinY() || neighbor.y > region.MaxY() || templatePositionLocked(locked, neighbor) {
					continue
				}
				state := region.nativeState(neighbor)
				if templateFluidIsSource(registry, state) {
					foundSource = true
					waterSource = templateWaterFluidState(registry, state)
					break
				}
			}
			if !foundSource {
				remaining = append(remaining, pos)
				continue
			}
			// Java removes the candidate and continues the pass after calling
			// placeLiquid, even when a specialized container rejects the source.
			filled = true
			if waterSource && templateContainerWaterloggable(currentJava) && !templateDoubleSlab(registry, current) {
				wet := templateWaterloggedState(currentJava)
				state, err := world.ResolveState(registry, wet)
				if err != nil {
					panic(err)
				}
				region.Set(pos, state)
				if featureInsideTarget(region.target, pos) {
					region.reserveJavaTick(chunk.ScheduledTick{
						Pos: cube.Pos{int(pos.x), int(pos.y), int(pos.z)}, Kind: chunk.FluidTick, Type: "minecraft:water",
					})
				}
			}
		}
		toFill = remaining
	}

	// Refresh payload state after propagation so wet/lit properties never trail
	// the block actually left in the target chunk.
	surviving := payloadBlocks[:0]
	for _, entry := range payloadBlocks {
		pos := featurePos{entry.Pos[0], entry.Pos[1], entry.Pos[2]}
		current := region.nativeState(pos)
		if templateContainerWaterloggable(entry.State) && !templateBool(entry.State.Properties["waterlogged"]) && templateFluidIsSource(registry, current) && templateWaterFluidState(registry, current) {
			entry = templateWaterloggedBlock(entry)
		}
		expected, err := entry.ResolveNative(registry, current)
		if err != nil {
			panic(err)
		}
		if expected.Block == current.Block {
			surviving = append(surviving, entry)
		}
	}
	return surviving, written
}
func applyTemplatePostShapeBlocks(region *featureRegion, blocks []javastructure.Block, flags int) {
	registry := region.c.BlockRegistry()
	for _, entry := range blocks {
		pos := featurePos{entry.Pos[0], entry.Pos[1], entry.Pos[2]}
		if pos.y < region.MinY() || pos.y > region.MaxY() ||
			absFeatureInt(pos.x>>4-region.writeOriginX) > 1 ||
			absFeatureInt(pos.z>>4-region.writeOriginZ) > 1 {
			continue
		}
		state, err := entry.ResolveNative(registry, region.nativeState(pos))
		if err != nil {
			panic(err)
		}
		if !region.setWithFlags(pos, state, flags) {
			continue
		}
		region.recordTemplateActor(entry)
	}
}

func structurePieceShapeCheckBlock(state world.NativeState, registry world.BlockRegistry) bool {
	name, _, ok := registry.RuntimeIDToState(state.Block)
	if !ok {
		panic("overworld: unknown structure block runtime ID")
	}
	switch normalizeJavaResource(name) {
	case "nether_brick_fence", "torch", "wall_torch", "oak_fence", "spruce_fence", "dark_oak_fence", "pale_oak_fence", "acacia_fence", "birch_fence", "jungle_fence", "ladder", "iron_bars":
		return true
	default:
		return false
	}
}

func templateContainerState(state world.BlockState) bool {
	if _, ok := state.Properties["waterlogged"]; ok {
		return true
	}
	return templateRejectingContainer(state.Name)
}

func templateRejectingContainer(name string) bool {
	switch normalizeJavaResource(name) {
	case "kelp", "kelp_plant", "seagrass", "tall_seagrass":
		return true
	default:
		return false
	}
}

func templateContainerWaterloggable(state world.BlockState) bool {
	_, ok := state.Properties["waterlogged"]
	return ok
}

func templateWaterloggedBlock(entry javastructure.Block) javastructure.Block {
	entry.State = templateWaterloggedState(entry.State)
	switch native := entry.NativeBlock.(type) {
	case block.Campfire:
		native.Extinguished = true
		entry.NativeBlock = native
	case block.Candle:
		native.Lit = false
		entry.NativeBlock = native
	}
	if entry.NativeBlock != nil {
		native, adapted, err := javastructure.NativeBlockForJavaState(entry.State)
		if err != nil {
			panic(err)
		}
		if adapted {
			entry.NativeBlock = native
		}
	}
	return entry
}

func templateWaterloggedState(state world.BlockState) world.BlockState {
	state.Properties = cloneFeatureProperties(state.Properties)
	state.Properties["waterlogged"] = true
	name := normalizeJavaResource(state.Name)
	if name == "campfire" || name == "soul_campfire" || name == "candle" || strings.HasSuffix(name, "_candle") {
		state.Properties["lit"] = false
	}
	return state
}

func templateBool(value any) bool {
	v, ok := value.(bool)
	if ok {
		return v
	}
	text, _ := value.(string)
	return text == "true"
}

func templateBlockStateIsSource(state world.BlockState) bool {
	if waterlogged, ok := state.Properties["waterlogged"]; ok && templateBool(waterlogged) {
		return true
	}
	switch normalizeJavaResource(state.Name) {
	case "bubble_column", "kelp", "kelp_plant", "seagrass", "tall_seagrass":
		return true
	case "water", "lava":
		if raw, ok := state.Properties["level"]; ok {
			switch value := raw.(type) {
			case int:
				return value == 0
			case int32:
				return value == 0
			case int64:
				return value == 0
			case string:
				level, err := strconv.Atoi(value)
				return err == nil && level == 0
			default:
				return false
			}
		}
		return true
	default:
		return false
	}
}

func templateDoubleSlab(registry world.BlockRegistry, state world.NativeState) bool {
	b, ok := registry.BlockByRuntimeID(state.Block)
	if !ok {
		return false
	}
	slab, ok := b.(block.Slab)
	return ok && slab.Double
}

func templateFluid(registry world.BlockRegistry, state world.NativeState) (world.Liquid, bool) {
	rid := state.Liquid
	if rid == registry.AirRuntimeID() && registry.LiquidBlock(state.Block) {
		rid = state.Block
	}
	if rid == registry.AirRuntimeID() || !registry.LiquidBlock(rid) {
		return nil, false
	}
	b, ok := registry.BlockByRuntimeID(rid)
	if !ok {
		return nil, false
	}
	fluid, ok := b.(world.Liquid)
	return fluid, ok
}

func templateWaterFluid(fluid world.Liquid) bool {
	_, ok := fluid.(block.Water)
	return ok
}

func templateFluidIsSource(registry world.BlockRegistry, state world.NativeState) bool {
	fluid, ok := templateFluid(registry, state)
	return ok && fluid.LiquidDepth() == 8 && !fluid.LiquidFalling()
}

func templateWaterFluidState(registry world.BlockRegistry, state world.NativeState) bool {
	fluid, ok := templateFluid(registry, state)
	return ok && templateWaterFluid(fluid)
}

func templatePositionLocked(locked []featurePos, pos featurePos) bool {
	for _, lockedPos := range locked {
		if lockedPos == pos {
			return true
		}
	}
	return false
}
