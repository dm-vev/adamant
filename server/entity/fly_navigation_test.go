package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFlyingPathGoesOverWalls checks FlyNodeEvaluator: a parrot finds a path
// through the air over a wall it cannot walk around, to the very block it
// was asked to fly to.
func TestFlyingPathGoesOverWalls(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	target := cube.Pos{5, 3, 0}
	var path *mobPath
	w.Do(func(tx *world.Tx) {
		for x := -4; x <= 10; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
				if x == 3 {
					for y := 1; y <= 2; y++ {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
			}
		}
		p := tx.AddEntity(NewParrot(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 1, .5}})).(*Parrot)
		p.Tick(tx, 0)
		b := p.b()
		m := b.ai.Mob(p.Ent, p)
		path = m.AI.nav.CreatePath(m, target, 0)
	})
	if path == nil || !path.CanReach() {
		t.Fatalf("parrot found no path over the wall: %v", path)
	}
	top := 0
	for i := range path.NodeCount() {
		top = max(top, path.nodePos(i).Y())
	}
	if top < 3 {
		t.Fatalf("path rose to y %d, want over the wall at y 3", top)
	}
	if end := path.EndNode().Pos(); end != target {
		t.Fatalf("path ends at %v, want the block %v in the air", end, target)
	}
}
