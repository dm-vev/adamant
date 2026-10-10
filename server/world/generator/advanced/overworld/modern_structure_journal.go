package overworld

import (
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

// journalStructureWorld lets source piece reads observe their own deferred writes.
// Direct level reads are not clipped; StructurePiece.getBlock reads are clipped.
type journalStructureWorld struct {
	env       javastructure.ProcessorEnvironment
	clip      javastructure.Bounds
	writes    map[[3]int32]uint32
	placement *javastructure.Placement
}

func newJournalStructureWorld(env javastructure.ProcessorEnvironment, clip javastructure.Bounds, placement *javastructure.Placement) *journalStructureWorld {
	if env.JavaBlockStateID == nil {
		panic("overworld: structure piece requires canonical Java state queries")
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
	id, err := w.env.JavaBlockStateID(x, y, z)
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

func (w *journalStructureWorld) shape(x, y, z int32) javadata.GenerationShape {
	shape, err := javadata.BlockGenerationShapeID(w.stateID(x, y, z))
	if err != nil {
		panic(err)
	}
	return shape
}

func (w *journalStructureWorld) set(x, y, z int32, state world.BlockState) bool {
	if !w.inside(x, y, z) || y < int32(w.env.Range.Min()) || y > int32(w.env.Range.Max()) {
		return false
	}
	id, err := javadata.BlockStateID(state.Name, state.Properties)
	if err != nil {
		panic(err)
	}
	pos := [3]int32{x, y, z}
	if w.writes == nil {
		w.writes = make(map[[3]int32]uint32)
	}
	w.writes[pos] = id
	w.placement.Blocks = append(w.placement.Blocks, javastructure.Block{Pos: pos, State: state})
	return true
}

var structureJournalAirID = func() uint32 {
	id, err := javadata.BlockStateID("minecraft:air", nil)
	if err != nil {
		panic(err)
	}
	return id
}()
