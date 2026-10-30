package block

import (
	"iter"
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// redstoneEntitiesInBox follows the native BlockSource actor-query chunk
// ranges, then tests actual body bounds. Typed queries scan the box's chunks;
// untyped queries expand the chunk range by two blocks. Neither loads terrain.
func redstoneEntitiesInBox(tx *world.Tx, box cube.BBox, typed bool) iter.Seq[world.Entity] {
	queryMin, queryMax := box.Min(), box.Max()
	if !typed {
		queryMin = queryMin.Sub(mgl64.Vec3{2, 0, 2})
		queryMax = queryMax.Add(mgl64.Vec3{2, 0, 2})
	}
	minPos, maxPos := cube.PosFromVec3(queryMin), cube.PosFromVec3(queryMax)
	minX, minZ := minPos[0]>>4, minPos[2]>>4
	maxX, maxZ := maxPos[0]>>4, maxPos[2]>>4
	// EntitiesWithin tests origins with strict boundaries. Include boundary
	// origins, then enforce the native integer chunk range below.
	query := cube.Box(float64(minX*16)-0.001, math.Inf(-1), float64(minZ*16)-0.001,
		float64((maxX+1)*16)+0.001, math.Inf(1), float64((maxZ+1)*16)+0.001)
	return func(yield func(world.Entity) bool) {
		for e := range tx.EntitiesWithin(query) {
			origin := cube.PosFromVec3(e.Position())
			if origin[0]>>4 < minX || origin[0]>>4 > maxX || origin[2]>>4 < minZ || origin[2]>>4 > maxZ {
				continue
			}
			if box.IntersectsWith(e.H().Type().BBox(e).Translate(e.Position())) && !yield(e) {
				return
			}
		}
	}
}
