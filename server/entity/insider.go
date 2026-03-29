package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// CheckEntityInsiders checks if the entity is colliding with any EntityInsider blocks.
func CheckEntityInsiders(tx *world.Tx, box cube.BBox, e world.Entity) {
	low, high := cube.PosFromVec3(box.Min()), cube.PosFromVec3(box.Max())

	for y := low[1]; y <= high[1]; y++ {
		for x := low[0]; x <= high[0]; x++ {
			for z := low[2]; z <= high[2]; z++ {
				blockPos := cube.Pos{x, y, z}
				b := tx.Block(blockPos)
				if collide, ok := b.(block.EntityInsider); ok {
					collide.EntityInside(blockPos, tx, e)
					if _, liquid := b.(world.Liquid); liquid {
						continue
					}
				}

				if l, ok := tx.Liquid(blockPos); ok {
					if collide, ok := l.(block.EntityInsider); ok {
						collide.EntityInside(blockPos, tx, e)
					}
				}
			}
		}
	}
}

// checkEntityInsiders checks if the entity is colliding with any EntityInsider blocks.
func checkEntityInsiders(tx *world.Tx, e world.Entity) {
	CheckEntityInsiders(tx, e.H().Type().BBox(e).Translate(e.Position()).Grow(-0.0001), e)
}
