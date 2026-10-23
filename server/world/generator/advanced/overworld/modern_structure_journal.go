package overworld

import (
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// journalStructureWorld lets source piece reads observe their own deferred writes.
// Direct level reads are not clipped; StructurePiece.getBlock reads are clipped.
type journalStructureWorld struct {
	env       structure.ProcessorEnvironment
	clip      structure.Bounds
	writes    map[[3]int32]uint32
	placement *structure.Placement
}

func newJournalStructureWorld(env structure.ProcessorEnvironment, clip structure.Bounds, placement *structure.Placement) *journalStructureWorld {
	if env.BlockStateID == nil {
		panic("overworld: structure piece requires canonical state queries")
	}
	placement.StructurePieceWrites = true
	return &journalStructureWorld{env: env, clip: clip, placement: placement}
}

func (w *journalStructureWorld) inside(x, y, z int32) bool {
	return x >= w.clip.Min[0] && x <= w.clip.Max[0] && y >= w.clip.Min[1] && y <= w.clip.Max[1] && z >= w.clip.Min[2] && z <= w.clip.Max[2]
}

func (w *journalStructureWorld) stateID(x, y, z int32) uint32 {
	if id, ok := w.writes[[3]int32{x, y, z}]; ok {
		return id
	}
	id, err := w.env.BlockStateID(x, y, z)
	if err != nil {
		panic(err)
	}
	return id
}

func (w *journalStructureWorld) getBlockID(x, y, z int32) uint32 {
	if !w.inside(x, y, z) {
		return structureJournalAirID
	}
	return w.stateID(x, y, z)
}

func (w *journalStructureWorld) shape(x, y, z int32) gamedata.GenerationShape {
	shape, err := gamedata.BlockGenerationShapeID(w.stateID(x, y, z))
	if err != nil {
		panic(err)
	}
	return shape
}

func (w *journalStructureWorld) set(x, y, z int32, state world.BlockState) bool {
	if !w.inside(x, y, z) {
		return false
	}
	return w.setUnclipped([3]int32{x, y, z}, state)
}

func (w *journalStructureWorld) setUnclipped(pos [3]int32, state world.BlockState) bool {
	id, err := gamedata.BlockStateID(state.Name, state.Properties)
	if err != nil {
		panic(err)
	}
	x, y, z := pos[0], pos[1], pos[2]
	w.placement.Blocks = append(w.placement.Blocks, structure.Block{Pos: pos, State: state})
	if y < int32(w.env.Range.Min()) || y > int32(w.env.Range.Max()) ||
		(w.env.CanWriteBlock != nil && !w.env.CanWriteBlock(x, y, z)) {
		return false
	}
	if w.writes == nil {
		w.writes = make(map[[3]int32]uint32)
	}
	w.writes[pos] = id
	return true
}

var structureJournalAirID = func() uint32 {
	id, err := gamedata.BlockStateID("minecraft:air", nil)
	if err != nil {
		panic(err)
	}
	return id
}()
