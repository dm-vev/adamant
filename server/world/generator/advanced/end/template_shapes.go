package end

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type endBlockActor struct {
	record javastructure.Block
	data   map[string]any
}

type endJavaShapeAccess struct {
	endFeatureOrigin
	result *world.GenerationResult
	random *java26.Random
}

func (a endJavaShapeAccess) JavaBlockStateID(pos cube.Pos) (uint32, error) {
	chunkX, chunkZ := floorDiv(pos[0], 16), floorDiv(pos[2], 16)
	if !a.region.containsChunk(chunkX, chunkZ) {
		return 0, fmt.Errorf("end generation shape state outside loaded region at %v", pos)
	}
	state := a.region.nativeState(pos[0], pos[1], pos[2])
	id := state.CanonicalJavaID(a.region.chunks[a.region.index(chunkX, chunkZ)].BlockRegistry())
	if id == 0 {
		return 0, fmt.Errorf("end generation shape state at %v is native-only", pos)
	}
	return id, nil
}

func (a endJavaShapeAccess) Range() cube.Range {
	return a.region.chunks[a.region.index(a.region.targetX, a.region.targetZ)].Range()
}

func endWorldgenRegionRandom(seed int64, chunkX, chunkZ int) java26.Random {
	initial := java26.NewLegacyRandom(seed)
	root := initial.ForkPositional()
	named := root.FromHashOf("minecraft:worldgen_region_random")
	factory := named.ForkPositional()
	return factory.At(int32(chunkX<<4), 0, int32(chunkZ<<4))
}

func (a endJavaShapeAccess) JavaRandom() *java26.Random {
	return a.random
}
func (endJavaShapeAccess) Light(cube.Pos) uint8 {
	// End has skylight. Before light initialization, SkyLightSectionStorage
	// returns 15 for absent visible sections; block light defaults to zero.
	return 15
}
func (endJavaShapeAccess) JavaFluidTickDelay(name string) int32 {
	switch name {
	case "minecraft:water":
		return 5
	case "minecraft:lava":
		// End uses the non-ultrawarm delay in LavaFluid.getTickDelay.
		return 30
	default:
		panic("end generation shape requested unknown fluid tick delay: " + name)
	}
}

// ProtoChunkTicks.schedule persists generated callbacks with delay zero.
func (a endJavaShapeAccess) ScheduleJavaTick(pos cube.Pos, kind chunk.ScheduledTickKind, name string, _ int32) {
	tick := chunk.ScheduledTick{Pos: pos, Kind: kind, Type: name}
	if _, exists := a.region.scheduledTicks[tick]; exists {
		return
	}
	a.region.scheduledTicks[tick] = struct{}{}
	if pos[0]>>4 == a.region.targetX && pos[2]>>4 == a.region.targetZ && pos[1] >= a.region.minY && pos[1] <= a.region.maxY {
		a.result.ScheduleTick(tick)
	}
}

func (a endJavaShapeAccess) HasJavaBlockTick(pos cube.Pos, name string) bool {
	_, exists := a.region.scheduledTicks[chunk.ScheduledTick{Pos: pos, Kind: chunk.BlockTick, Type: name}]
	return exists
}

func (a endJavaShapeAccess) JavaSignal(pos cube.Pos, face cube.Face) int {
	stateID, err := a.JavaBlockStateID(pos)
	if err != nil {
		panic(err)
	}
	name, ok := javadata.BlockStateName(stateID)
	if !ok {
		panic(fmt.Sprintf("invalid End Java signal state ID %d at %v", stateID, pos))
	}
	if name != "minecraft:repeater" && name != "minecraft:comparator" {
		return 0
	}
	facing, ok := javadata.BlockStateString(stateID, "facing")
	if !ok || endFace(facing) != face.Opposite() {
		return 0
	}
	powered, ok := javadata.BlockStateBool(stateID, "powered")
	if !ok {
		panic(fmt.Sprintf("Java signal state %d has no powered property", stateID))
	}
	if !powered {
		return 0
	}
	if name == "minecraft:repeater" {
		return 15
	}
	if actor, exists := a.region.blockActors[pos]; exists {
		for _, key := range [...]string{"OutputSignal", "output_signal"} {
			if output, ok := actor.data[key]; ok {
				return intValue(output)
			}
		}
	}
	return 0
}

func (a endJavaShapeAccess) JavaHiveHasOccupants(pos cube.Pos) bool {
	if actor, exists := a.region.blockActors[pos]; exists {
		for _, key := range [...]string{"bees", "Occupants"} {
			switch occupants := actor.data[key].(type) {
			case []any:
				if len(occupants) != 0 {
					return true
				}
			case []map[string]any:
				if len(occupants) != 0 {
					return true
				}
			}
		}
	}
	return false
}

func (a endJavaShapeAccess) SetJavaBlockState(pos cube.Pos, stateID uint32, flags int) bool {
	chunkX, chunkZ := floorDiv(pos[0], 16), floorDiv(pos[2], 16)
	if pos[1] < a.region.minY || pos[1] > a.region.maxY || !a.region.containsChunk(chunkX, chunkZ) ||
		!a.inWriteRadius(pos[0], pos[2]) {
		return false
	}
	name, properties, ok := javadata.BlockStateFromID(stateID)
	if !ok {
		panic(fmt.Sprintf("end generation shape produced unknown Java state ID %d", stateID))
	}
	oldID, err := a.JavaBlockStateID(pos)
	if err != nil {
		panic(err)
	}
	oldName, ok := javadata.BlockStateName(oldID)
	if !ok {
		panic(fmt.Sprintf("invalid previous End Java state ID %d at %v", oldID, pos))
	}
	registry := a.region.chunks[a.region.index(chunkX, chunkZ)].BlockRegistry()
	native, err := world.ResolveState(registry, world.BlockState{Name: name, Properties: properties})
	if err != nil {
		panic(fmt.Errorf("end generation shape cannot project %s: %w", name, err))
	}
	if oldName != name {
		delete(a.region.blockActors, pos)
		a.removeBlockEntity(pos)
	}
	if !a.region.setLayers(pos[0], pos[1], pos[2], native) {
		return false
	}
	if actor, exists := a.region.blockActors[pos]; exists {
		actor.record.State = world.BlockState{Name: name, Properties: properties}
		nativeActor, adapted, err := javastructure.NativeBlockForJavaState(actor.record.State)
		if err != nil {
			panic(err)
		}
		if adapted {
			actor.record.NativeBlock = nativeActor
		}
		a.recordBlockActor(actor.record)
	}
	if flags&16 == 0 {
		a.markPostProcessing(pos, stateID)
	}
	return true
}

func (a endJavaShapeAccess) recordBlockActor(record javastructure.Block) {
	if len(record.NBT) == 0 && record.NativeBlock == nil {
		return
	}
	for _, payload := range (javastructure.Placement{Blocks: []javastructure.Block{record}}).BlockEntityPayloads() {
		a.region.blockActors[payload.Pos] = endBlockActor{record: record, data: payload.Data}
		if payload.Pos[0]>>4 == a.region.targetX && payload.Pos[2]>>4 == a.region.targetZ {
			a.removeBlockEntity(payload.Pos)
			a.result.BlockEntities = append(a.result.BlockEntities, payload)
		}
	}
}

func (a endJavaShapeAccess) removeBlockEntity(pos cube.Pos) {
	retained := a.result.BlockEntities[:0]
	for _, actor := range a.result.BlockEntities {
		if actor.Pos != pos {
			retained = append(retained, actor)
		}
	}
	a.result.BlockEntities = retained
}

func (a endJavaShapeAccess) markPostProcessing(pos cube.Pos, stateID uint32) {
	name, ok := javadata.BlockStateName(stateID)
	if !ok {
		return
	}
	switch name {
	case "minecraft:brown_mushroom", "minecraft:red_mushroom":
		a.markOwnedPostProcessing(pos)
	case "minecraft:soul_sand", "minecraft:magma_block":
		above := pos.Side(cube.FaceUp)
		id, err := a.JavaBlockStateID(above)
		if err != nil {
			panic(err)
		}
		aboveName, _ := javadata.BlockStateName(id)
		if aboveName != "minecraft:air" && aboveName != "minecraft:cave_air" && aboveName != "minecraft:void_air" {
			a.markOwnedPostProcessing(above)
		}
	}
}

func (a endJavaShapeAccess) markOwnedPostProcessing(pos cube.Pos) {
	if pos[0]>>4 == a.region.targetX && pos[2]>>4 == a.region.targetZ {
		a.result.MarkForPostProcessing(pos, a.Range())
	}
}

func endFace(name string) cube.Face {
	switch name {
	case "north":
		return cube.FaceNorth
	case "east":
		return cube.FaceEast
	case "south":
		return cube.FaceSouth
	case "west":
		return cube.FaceWest
	default:
		return cube.FaceDown
	}
}

func intValue(value any) int {
	switch n := value.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	default:
		return 0
	}
}
