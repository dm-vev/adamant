package entity

import (
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func benchSpawnMob(b *testing.B, tx *world.Tx, h *world.EntityHandle) (*Mob, world.Entity) {
	b.Helper()
	e := tx.AddEntity(h)
	if ticker, ok := e.(interface{ Tick(*world.Tx, int64) }); ok {
		ticker.Tick(tx, 1)
	}
	var ent *Ent
	switch v := e.(type) {
	case *zombie:
		ent = v.Ent
	case *Cow:
		ent = v.Ent
	case *Parrot:
		ent = v.Ent
	case *dolphin:
		ent = v.Ent
	case *frog:
		ent = v.Ent
	case *Bee:
		ent = v.Ent
	default:
		b.Fatalf("unsupported mob type %T", e)
	}
	ai, _ := ent.mobAI()
	if ai == nil {
		b.Fatalf("entity %T has no MobAI", e)
	}
	m := ai.Mob(ent, e)
	ai.Goals.Remove(m, func(Goal) bool { return true })
	return m, e
}

func fillFlatFloor(tx *world.Tx, minX, maxX, minZ, maxZ, y int, blk world.Block) {
	for x := minX; x <= maxX; x++ {
		for z := minZ; z <= maxZ; z++ {
			tx.SetBlock(cube.Pos{x, y, z}, blk, nil)
		}
	}
}

func BenchmarkPathfindWalkFlat(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -4, 24, -8, 8, 63, block.Grass{})
		m, _ := benchSpawnMob(b, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.onGround = true
		target := cube.Pos{14, 64, 3}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path = nil
			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil || !p.CanReach() {
				b.Fatalf("expected reachable path, got %v", p)
			}
		}
	})
}

func BenchmarkPathfindWalkObstacles(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -2, 16, -6, 6, 63, block.Stone{})
		for x := 3; x <= 7; x += 4 {
			for z := -4; z <= 4; z++ {
				if (x == 3 && z == 3) || (x == 7 && z == -2) {
					continue
				}
				if z%2 == 0 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.WoodFence{Wood: block.OakWood()}, nil)
				} else {
					tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				}
			}
		}
		// Place a closed wooden door in the second gap (zombie with canOpenDoors=true).
		tx.SetBlock(cube.Pos{7, 64, -2}, block.WoodDoor{Wood: block.OakWood(), Facing: cube.East, Open: false}, nil)
		tx.SetBlock(cube.Pos{7, 65, -2}, block.WoodDoor{Wood: block.OakWood(), Facing: cube.East, Open: false, Top: true}, nil)

		m, _ := benchSpawnMob(b, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.onGround = true
		m.AI.nav.SetCanOpenDoors(true)
		target := cube.Pos{10, 64, 0}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path = nil
			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil || !p.CanReach() {
				b.Fatalf("expected reachable path around obstacles, got %v", p)
			}
		}
	})
}

func BenchmarkPathfindWalkStairsElevation(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -2, 18, -6, 6, 63, block.Stone{})
		// Ascending steps x=2..6, plateau x=7..9, descending ledge x=10..12.
		for x := 2; x <= 6; x++ {
			height := x - 1
			for z := -4; z <= 4; z++ {
				for y := 64; y < 64+height; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
				}
			}
		}
		for x := 7; x <= 9; x++ {
			for z := -4; z <= 4; z++ {
				for y := 64; y < 69; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
				}
			}
		}
		for x := 10; x <= 12; x++ {
			for z := -4; z <= 4; z++ {
				for y := 64; y < 67; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
				}
			}
		}

		m, _ := benchSpawnMob(b, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.onGround = true
		target := cube.Pos{14, 64, 0}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path = nil
			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil || !p.CanReach() {
				b.Fatalf("expected reachable path over elevation, got %v", p)
			}
		}
	})
}

func BenchmarkPathfindWalkWaterLava(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -2, 18, -8, 8, 62, block.Stone{})
		fillFlatFloor(tx, -2, 18, -8, 8, 63, block.Stone{})
		// Lava trench across z=-3..3 at x=5..6
		for x := 5; x <= 6; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Lava{Depth: 8, Still: true}, nil)
			}
		}
		// Water channel across z=-6..6 at x=9..10
		for x := 9; x <= 10; x++ {
			for z := -6; z <= 6; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Depth: 8, Still: true}, nil)
			}
		}

		m, _ := benchSpawnMob(b, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 64, 0.5}}))
		m.AI.onGround = true
		m.AI.nav.SetCanFloat(true)
		target := cube.Pos{13, 64, 0}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path = nil
			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil || !p.CanReach() {
				b.Fatalf("expected reachable path around lava/water, got %v", p)
			}
		}
	})
}

func BenchmarkPathfindWalkUnreachable(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -16, 20, -16, 16, 63, block.Stone{})
		// Enclose target at (12, 64, 0) in a 3x3 stone box.
		for x := 11; x <= 13; x++ {
			for z := -1; z <= 1; z++ {
				if x == 12 && z == 0 {
					continue
				}
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				tx.SetBlock(cube.Pos{x, 66, z}, block.Stone{}, nil)
			}
		}

		m, _ := benchSpawnMob(b, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.onGround = true
		target := cube.Pos{12, 64, 0}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path = nil
			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil {
				b.Fatal("expected partial path to closest reachable node")
			}
		}
	})
}

func BenchmarkPathfindFly(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -4, 16, -6, 6, 63, block.Stone{})
		for z := -5; z <= 5; z++ {
			for y := 64; y <= 65; y++ {
				tx.SetBlock(cube.Pos{4, y, z}, block.Stone{}, nil)
			}
		}

		m, _ := benchSpawnMob(b, tx, NewParrot(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		target := cube.Pos{8, 66, 1}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path = nil
			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil || !p.CanReach() {
				b.Fatalf("expected reachable fly path, got %v", p)
			}
		}
	})
}

func BenchmarkPathfindSwim(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -4, 16, -6, 6, 55, block.Stone{})
		for x := -4; x <= 16; x++ {
			for z := -6; z <= 6; z++ {
				for y := 56; y <= 63; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true}, nil)
				}
			}
		}
		// Underwater pillar obstacle.
		for z := -2; z <= 2; z++ {
			for y := 56; y <= 61; y++ {
				tx.SetBlock(cube.Pos{5, y, z}, block.Stone{}, nil)
			}
		}

		m, _ := benchSpawnMob(b, tx, NewDolphin(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 59, 0.5}}))
		m.AI.waterHeight = 1
		target := cube.Pos{9, 59, 0}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path = nil
			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil || !p.CanReach() {
				b.Fatalf("expected reachable swim path, got %v", p)
			}
		}
	})
}

func BenchmarkPathfindAmphibious(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -4, 16, -6, 6, 61, block.Stone{})
		for x := -4; x <= 16; x++ {
			for z := -6; z <= 6; z++ {
				if x < 4 || x > 6 {
					for y := 62; y <= 63; y++ {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				} else {
					for y := 62; y <= 63; y++ {
						tx.SetBlock(cube.Pos{x, y, z}, block.Water{Depth: 8, Still: true}, nil)
					}
				}
			}
		}

		m, _ := benchSpawnMob(b, tx, NewFrog(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.onGround = true
		target := cube.Pos{8, 64, 0}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path = nil
			p := m.AI.nav.CreatePath(m, target, 0)
			if p == nil || !p.CanReach() {
				b.Fatalf("expected reachable amphibious path, got %v", p)
			}
		}
	})
}

func BenchmarkRandomPosLand(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -24, 24, -24, 24, 63, block.Grass{})
		m, _ := benchSpawnMob(b, tx, NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.onGround = true

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = landRandomPos(m, 10, 7)
		}
	})
}

func BenchmarkRandomPosDefault(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -24, 24, -24, 24, 63, block.Grass{})
		m, _ := benchSpawnMob(b, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.onGround = true

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = defaultRandomPos(m, 10, 7)
		}
	})
}

func BenchmarkRandomPosHover(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -24, 24, -24, 24, 63, block.Grass{})
		m, _ := benchSpawnMob(b, tx, NewBee(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 66, 0.5}}))

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			_, _ = hoverRandomPos(m, 8, 7, 1, 0, 1.57, 3, 1)
		}
	})
}

func BenchmarkNavTickFollowPath(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -4, 24, -8, 8, 63, block.Grass{})
		m, _ := benchSpawnMob(b, tx, NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		m.AI.onGround = true
		p := m.AI.nav.CreatePath(m, cube.Pos{14, 64, 0}, 0)
		if p == nil || !p.CanReach() {
			b.Fatal("expected path")
		}
		m.AI.nav.MoveTo(m, p, 1)

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			m.AI.nav.path.next = 1
			m.AI.nav.tick = 1
			m.AI.nav.lastStuckCheck = 0
			m.AI.nav.tickNavigation(m)
		}
	})
}

func BenchmarkFindGroundPath(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -2, 16, -8, 8, 63, block.Stone{})
		for z := -4; z <= 4; z++ {
			if z == 4 {
				continue
			}
			tx.SetBlock(cube.Pos{6, 64, z}, block.Stone{}, nil)
			tx.SetBlock(cube.Pos{6, 65, z}, block.Stone{}, nil)
		}
		profile := groundProfile{MaxVisitedNodes: 256}
		start, goal := cube.Pos{1, 64, 0}, cube.Pos{12, 64, 0}

		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			path := findGroundPath(tx, start, goal, profile)
			if len(path) == 0 {
				b.Fatal("expected ground path")
			}
		}
	})
}

func BenchmarkNavigatorTick(b *testing.B) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()

	w.Do(func(tx *world.Tx) {
		fillFlatFloor(tx, -2, 16, -2, 2, 63, block.Stone{})
		var nav navigator
		if !nav.MoveTo(tx, cube.Pos{0, 64, 0}, cube.Pos{12, 64, 0}, groundProfile{MaxVisitedNodes: 256}, 1) {
			b.Fatal("expected initial path")
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			nav.Index = 0
			nav.LastStuckCheck = int64(i)
			_, _ = nav.Tick(mgl64.Vec3{0.5, 64, 0.5}, int64(i)+1, 0.6)
		}
	})
}
