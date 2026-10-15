package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func (r *featureRegion) setProjectionWithFlags(pos featurePos, state world.NativeState, flags int) bool {
	if pos.y < r.MinY() || pos.y > r.MaxY() || !r.inTarget(pos) {
		return false
	}
	if _, _, ok := r.c.BlockRegistry().RuntimeIDToState(state.Block); !ok {
		panic("overworld: item-frame projection has unknown native block ID")
	}
	if _, _, ok := r.c.BlockRegistry().RuntimeIDToState(state.Liquid); !ok {
		panic("overworld: item-frame projection has unknown liquid ID")
	}
	previous := r.nativeState(pos)
	// Keep the logical source state visible to later Java pieces and shape
	// callbacks while publishing the native-only entity projection in the chunk.
	r.overlay[featureKey(pos)] = previous
	r.noteOverlay(pos)
	r.nativeProjections[cube.Pos{int(pos.x), int(pos.y), int(pos.z)}] = struct{}{}
	world.SetNativeState(r.c, uint8(pos.x&15), int16(pos.y), uint8(pos.z&15), state)
	if flags&16 == 0 {
		r.markBlockPostProcessing(pos, state)
	}
	return true
}

func (r *featureRegion) refreshTemplateActorState(pos cube.Pos, state world.BlockState) {
	entry, exists := r.blockActorRecords[pos]
	if !exists {
		return
	}
	entry.State = state
	native, adapted, err := javastructure.NativeBlockForJavaState(state)
	if err != nil {
		panic(err)
	}
	if adapted {
		entry.NativeBlock = native
	}
	r.blockActorRecords[pos] = entry
	r.refreshTemplateActor(pos)
}

func (r *featureRegion) recordTemplateActor(entry javastructure.Block) {
	if len(entry.NBT) == 0 && entry.NativeBlock == nil {
		return
	}
	if entry.NBT != nil {
		data := make(map[string]any, len(entry.NBT))
		for key, value := range entry.NBT {
			data[key] = value
		}
		entry.NBT = data
	}
	pos := cube.Pos{int(entry.Pos[0]), int(entry.Pos[1]), int(entry.Pos[2])}
	r.blockActorRecords[pos] = entry
	r.refreshTemplateActor(pos)
}

func featureRecordJavaBlockEntity(region *featureRegion, p featurePos, data map[string]any) {
	if p.y < region.MinY() || p.y > region.MaxY() {
		return
	}
	state, err := world.JavaState(region.c.BlockRegistry(), region.nativeState(p))
	if err != nil {
		panic(err)
	}
	region.recordTemplateActor(javastructure.Block{Pos: [3]int32{p.x, p.y, p.z}, State: state, NBT: data})
}

func (r *featureRegion) refreshTemplateActor(pos cube.Pos) {
	entry, exists := r.blockActorRecords[pos]
	if !exists {
		delete(r.blockActorData, pos)
		return
	}
	payloads := (javastructure.Placement{Blocks: []javastructure.Block{entry}}).BlockEntityPayloads()
	found := false
	for _, payload := range payloads {
		if payload.Pos == pos {
			r.blockActorData[pos] = payload.Data
			found = true
			break
		}
	}
	if !found {
		delete(r.blockActorData, pos)
	}
	p := featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	if r.inTarget(p) {
		r.replaceBlockEntityPayload(pos, payloads)
	} else if r.recording != nil {
		r.recording.actor(entry, r.nativeState(p))
	}
}

func (r *featureRegion) replaceBlockEntityPayload(pos cube.Pos, payloads []chunk.BlockEntity) {
	kept := r.result.BlockEntities[:0]
	for _, actor := range r.result.BlockEntities {
		if actor.Pos != pos {
			kept = append(kept, actor)
		}
	}
	r.result.BlockEntities = kept
	for _, actor := range payloads {
		if actor.Pos == pos {
			r.result.BlockEntities = append(r.result.BlockEntities, actor)
		}
	}
}

func (r *featureRegion) templateActorData(pos cube.Pos) map[string]any {
	if data := r.blockActorData[pos]; data != nil {
		return data
	}
	for i := len(r.result.BlockEntities) - 1; i >= 0; i-- {
		if r.result.BlockEntities[i].Pos == pos {
			return r.result.BlockEntities[i].Data
		}
	}
	return nil
}

func (r *featureRegion) hasChestBlockEntity(pos cube.Pos) bool {
	data := r.templateActorData(pos)
	return data != nil && data["id"] == "Chest"
}

func (r *featureRegion) setBlockEntityLootTable(pos cube.Pos, table string, seed int64) {
	if entry, exists := r.blockActorRecords[pos]; exists {
		if entry.NBT == nil {
			entry.NBT = make(map[string]any, 2)
		}
		entry.NBT["LootTable"], entry.NBT["LootTableSeed"] = table, seed
		r.blockActorRecords[pos] = entry
		r.refreshTemplateActor(pos)
		return
	}
	if data := r.templateActorData(pos); data != nil {
		data["LootTable"], data["LootTableSeed"] = table, seed
	}
}

func hasHiveOccupantList(value any) bool {
	switch occupants := value.(type) {
	case []any:
		return len(occupants) != 0
	case []map[string]any:
		return len(occupants) != 0
	default:
		return false
	}
}
