package advanced

import (
	"errors"
	"io/fs"
	"math"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

// VanillaStructureTemplate returns a structure template of the vanilla data
// pack, like minecraft:igloo/top, as StructureTemplateManager loads it from
// resources. Names without a namespace are in the minecraft namespace.
func VanillaStructureTemplate(w *world.World, name string) (*world.StructureTemplate, bool, error) {
	path := strings.TrimPrefix(name, "minecraft:")
	if strings.Contains(path, ":") || path == "" {
		return nil, false, nil
	}
	source, err := structure.Load(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	everywhere := structure.Bounds{Min: [3]int32{math.MinInt32, math.MinInt32, math.MinInt32}, Max: [3]int32{math.MaxInt32, math.MaxInt32, math.MaxInt32}}
	placement, err := source.Place(0, [3]int32{}, structure.None, structure.NoMirror, everywhere, 0)
	if err != nil {
		return nil, false, err
	}
	reg := w.BlockRegistry()
	actors := map[cube.Pos]map[string]any{}
	for _, payload := range placement.BlockEntityPayloads() {
		actors[payload.Pos] = payload.Data
	}
	t := world.NewStructureTemplate([3]int{int(source.Size[0]), int(source.Size[1]), int(source.Size[2])})
	for _, b := range placement.Blocks {
		pos := cube.Pos{int(b.Pos[0]), int(b.Pos[1]), int(b.Pos[2])}
		native, err := world.ResolveState(reg, b.State)
		if err != nil {
			return nil, false, err
		}
		block := b.NativeBlock
		if block == nil {
			block = reg.BlockByRuntimeIDOrAir(native.Block)
		}
		liquid, _ := reg.BlockByRuntimeIDOrAir(native.Liquid).(world.Liquid)
		t.SetBlock(pos, block, liquid, actors[pos])
	}
	handles, err := placement.NewEntityHandles(entity.NewFromStructureNBT)
	if err != nil {
		return nil, false, err
	}
	for _, h := range handles {
		t.AddEntity(w.EntityNBT(h))
	}
	return t, true, nil
}
