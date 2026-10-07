package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

func placeModernVines(region *featureRegion, origin featurePos) bool {
	if !featureRegionAir(region, origin) {
		return false
	}
	// Direction.values(), omitting DOWN, determines attachment precedence.
	for _, side := range [...]struct {
		delta    featurePos
		face     cube.Face
		property string
	}{
		{featurePos{0, 1, 0}, cube.FaceUp, "up"},
		{featurePos{0, 0, -1}, cube.FaceNorth, "north"},
		{featurePos{0, 0, 1}, cube.FaceSouth, "south"},
		{featurePos{-1, 0, 0}, cube.FaceWest, "west"},
		{featurePos{1, 0, 0}, cube.FaceEast, "east"},
	} {
		p := featurePos{origin.x + side.delta.x, origin.y + side.delta.y, origin.z + side.delta.z}
		name, properties := region.State(p)
		shape, err := javadata.BlockGenerationShape(name, properties)
		if err != nil {
			panic(err)
		}
		if shape.Attachment&(1<<uint(side.face.Opposite())) == 0 {
			continue
		}
		state, err := world.ResolveState(region.c.BlockRegistry(), world.BlockState{Name: "minecraft:vine", Properties: map[string]any{side.property: true}})
		if err != nil {
			panic(err)
		}
		region.Set(origin, state)
		return true
	}
	return false
}
