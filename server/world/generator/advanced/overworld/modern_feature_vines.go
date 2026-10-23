package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
)

var modernVineStateIDs = func() [5]uint32 {
	defaultID, err := gamedata.BlockStateID("minecraft:vine", nil)
	if err != nil {
		panic(err)
	}
	var ids [5]uint32
	for i, property := range [...]string{"up", "north", "south", "west", "east"} {
		ids[i], err = gamedata.BlockStateWithProperty(defaultID, property, true)
		if err != nil {
			panic(err)
		}
	}
	return ids
}()

func placeModernVines(region *featureRegion, origin featurePos) bool {
	if !featureRegionAir(region, origin) {
		return false
	}
	// Direction.values(), omitting DOWN, determines attachment precedence.
	for i, side := range [...]struct {
		delta featurePos
		face  cube.Face
	}{
		{featurePos{0, 1, 0}, cube.FaceUp},
		{featurePos{0, 0, -1}, cube.FaceNorth},
		{featurePos{0, 0, 1}, cube.FaceSouth},
		{featurePos{-1, 0, 0}, cube.FaceWest},
		{featurePos{1, 0, 0}, cube.FaceEast},
	} {
		p := featurePos{origin.x + side.delta.x, origin.y + side.delta.y, origin.z + side.delta.z}
		id := region.nativeState(p).CanonicalID(region.c.BlockRegistry())
		shape, err := gamedata.BlockGenerationShapeID(id)
		if err != nil {
			panic(err)
		}
		if shape.Attachment&(1<<uint(side.face.Opposite())) == 0 {
			continue
		}
		state := world.ResolveStateID(region.c.BlockRegistry(), modernVineStateIDs[i])
		region.setWithFlags(origin, state, 2)
		return true
	}
	return false
}
