package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

// featureWouldSurvive evaluates the same source survival handlers used by
// generation-time block placement and template shape updates.
func featureWouldSurvive(region *featureRegion, p featurePos, value any) bool {
	state, ok := featureStateBlock(value)
	if !ok {
		panic("overworld: malformed would_survive state")
	}
	id, err := javadata.BlockStateID(state.State.Name, state.State.Properties)
	if err != nil {
		panic(err)
	}
	return world.JavaGenerationBlockCanSurvive(region, id, cube.Pos{int(p.x), int(p.y), int(p.z)})
}
