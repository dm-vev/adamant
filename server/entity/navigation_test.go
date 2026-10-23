package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestGroundPathDoesNotClipDiagonalCorner(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	start, goal := cube.Pos{0, 1, 0}, cube.Pos{1, 1, 1}
	var path []cube.Pos
	w.Do(func(tx *world.Tx) {
		for x := -2; x <= 3; x++ {
			for z := -2; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{1, 1, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{0, 1, 1}, block.Stone{}, nil)
		path = FindGroundPath(tx, start, goal, GroundProfile{MaxVisitedNodes: 256})
	})
	if len(path) < 2 {
		t.Fatalf("route clipped a blocked corner: %v", path)
	}
}

func TestGroundPathAvoidsFire(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	start, goal := cube.Pos{0, 1, 0}, cube.Pos{4, 1, 0}
	var path []cube.Pos
	w.Do(func(tx *world.Tx) {
		for x := -1; x <= 5; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{2, 1, 0}, block.Fire{}, nil)
		tx.SetBlock(cube.Pos{3, 1, 0}, block.Lava{Depth: 8, Still: true}, nil)
		path = FindGroundPath(tx, start, goal, GroundProfile{MaxVisitedNodes: 512})
	})
	for _, p := range path {
		if p == (cube.Pos{2, 1, 0}) || p == (cube.Pos{3, 1, 0}) {
			t.Fatalf("route crossed fire or lava: %v", path)
		}
	}
}

func TestGroundPathRespectsMaximumFall(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	start, goal := cube.Pos{0, 5, 0}, cube.Pos{1, 1, 0}
	var path []cube.Pos
	w.Do(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 4, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 0, 0}, block.Stone{}, nil)
		path = FindGroundPath(tx, start, goal, GroundProfile{MaxVisitedNodes: 128, MaxFall: 3})
	})
	if len(path) != 0 {
		t.Fatalf("found route with a fall beyond maximum: %v", path)
	}
}

func TestNavigatorCachesAndReplansFarTargets(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	var n Navigator
	start, first, far := cube.Pos{0, 1, 0}, cube.Pos{2, 1, 0}, cube.Pos{10, 1, 0}
	w.Do(func(tx *world.Tx) {
		for x := -1; x <= 12; x++ {
			tx.SetBlock(cube.Pos{x, 0, 0}, block.Stone{}, nil)
		}
		if !n.MoveTo(tx, start, first, GroundProfile{MaxVisitedNodes: 256}, 1) {
			t.Fatal("initial path unavailable")
		}
		path := append([]cube.Pos(nil), n.Path...)
		if !n.MoveTo(tx, start, first, GroundProfile{MaxVisitedNodes: 256}, 2) || len(path) != len(n.Path) {
			t.Fatal("unchanged target did not reuse path")
		}
		if !n.MoveTo(tx, start, far, GroundProfile{MaxVisitedNodes: 256}, 3) || n.Target != far {
			t.Fatal("far target was not replanned immediately")
		}
	})
}

// TestCowFollowsNavigatorThroughWindingCorridor checks a cow walks a path
// with two turns through a corridor one block wide. The path stays within
// the 16 blocks a cow walks at most, its follow range.
func TestCowFollowsNavigatorThroughWindingCorridor(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	start, goal := cube.Pos{1, 1, 7}, cube.Pos{7, 1, 3}
	reached := false
	w.Do(func(tx *world.Tx) {
		for x := 0; x <= 8; x++ {
			for z := 0; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 0, z}, block.Stone{}, nil)
				if x == 0 || x == 8 || z == 0 || z == 8 {
					tx.SetBlock(cube.Pos{x, 1, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 2, z}, block.Stone{}, nil)
				}
			}
		}
		for _, wall := range []struct{ x, gap int }{{3, 7}, {5, 1}} {
			for z := 1; z < 8; z++ {
				if z == wall.gap {
					continue
				}
				tx.SetBlock(cube.Pos{wall.x, 1, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{wall.x, 2, z}, block.Stone{}, nil)
			}
		}
		handle := NewCow(world.EntitySpawnOpts{Position: start.Vec3Centre()})
		tx.AddEntity(handle)
		ent, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("cow did not spawn")
		}
		cow := ent.(*Cow)
		for tick := int64(1); tick <= 5; tick++ {
			cow.Tick(tx, tick)
		}
		ai := &cow.behaviour().ai
		m := ai.Mob(cow.Ent, cow)
		ai.Goals.Remove(m, func(Goal) bool { return true })
		g := goal.Vec3()
		if !ai.nav.MoveToPos(m, g[0], g[1], g[2], 1) {
			t.Fatal("cow route through winding corridor was not found")
		}
		for tick := int64(6); tick <= 2000; tick++ {
			cow.Tick(tx, tick)
			// The path ends within a block of the target, its reach range.
			if d := cow.Position().Sub(goal.Vec3Centre()); math.Hypot(d[0], d[2]) < 1.5 {
				reached = true
				break
			}
		}
	})
	if !reached {
		t.Fatal("cow did not reach the target through the winding corridor within 2000 ticks")
	}
}
