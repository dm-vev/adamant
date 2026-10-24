package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dragonTestFight is a fight with a fixed number of living crystals.
type dragonTestFight struct{ crystals int }

func (dragonTestFight) UpdateDragon(*world.Tx, world.Entity)    {}
func (dragonTestFight) SetDragonKilled(*world.Tx, world.Entity) {}
func (dragonTestFight) OnCrystalDestroyed(*world.Tx, world.Entity, cube.Pos, world.DamageSource) {
}
func (dragonTestFight) HasPreviouslyKilledDragon() bool { return false }
func (f dragonTestFight) AliveCrystals() int            { return f.crystals }

// dragonTestExplosion is an explosion of a size at a position.
type dragonTestExplosion struct {
	pos  mgl64.Vec3
	size float64
}

func (e dragonTestExplosion) Position() mgl64.Vec3 { return e.pos }
func (e dragonTestExplosion) Size() float64        { return e.size }

// dragonTestWorld returns a world with a bedrock podium top at the origin.
func dragonTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Bedrock{}, nil)
			}
		}
	})
	return w
}

func addTestDragon(tx *world.Tx, pos mgl64.Vec3, phase entity.DragonPhase, fight world.DragonFight) *entity.EnderDragon {
	return tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(entity.EnderDragonType, entity.EnderDragonBehaviourConfig{Phase: phase, Fight: fight})).(*entity.EnderDragon)
}

// TestDragonPartsAreJavaSized checks the parts are as wide as deep and stand
// on their positions, as EnderDragonParts do.
func TestDragonPartsAreJavaSized(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dragon := addTestDragon(tx, mgl64.Vec3{10, 80, 30}, entity.DragonHoldingPattern, nil)
		parts := dragon.PartBoxes()
		for i, want := range [8][2]float64{{1, 1}, {3, 3}, {5, 3}, {2, 2}, {2, 2}, {2, 2}, {4, 2}, {4, 2}} {
			box := parts[i].Box
			if box.Width() != want[0] || box.Length() != want[0] || box.Height() != want[1] {
				t.Fatalf("%s box is %vx%vx%v, want %vx%vx%v", parts[i].Name, box.Width(), box.Height(), box.Length(), want[0], want[1], want[0])
			}
		}
		if body := parts[2].Box; body.Min()[1] != 80 {
			t.Fatalf("body box starts at y=%v, want the dragon's y 80", body.Min()[1])
		}
	})
}

// TestDragonPathKeepsToInnerRingsWithoutCrystals checks findPath and
// findClosestNode leave the outer ring out once no crystal is alive.
func TestDragonPathKeepsToInnerRingsWithoutCrystals(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dragon := addTestDragon(tx, mgl64.Vec3{60, 100, 0}, entity.DragonHoldingPattern, dragonTestFight{})
		nodes := entity.DragonNodes(tx, dragon)
		start := entity.DragonClosestNode(tx, dragon)
		if start < 12 {
			t.Fatalf("closest node %d is on the outer ring without crystals", start)
		}
		path, ok := entity.DragonFindPath(tx, dragon, start, 20, nil)
		if !ok || len(path) < 2 || path[0] != nodes[start] || path[len(path)-1] != nodes[20] {
			t.Fatalf("no path from node %d to the centre ring: %v", start, path)
		}
		inner := map[cube.Pos]bool{}
		for i := 12; i < 24; i++ {
			inner[nodes[i]] = true
		}
		for _, node := range path {
			if !inner[node] {
				t.Fatalf("path node %v is on the outer ring", node)
			}
		}

		withCrystals := addTestDragon(tx, mgl64.Vec3{60, 100, 0}, entity.DragonHoldingPattern, dragonTestFight{crystals: 10})
		if n := entity.DragonClosestNode(tx, withCrystals); n >= 12 {
			t.Fatalf("closest node with crystals = %d, want the outer ring near (60, 0)", n)
		}
		final := cube.Pos{0, 70, 0}
		path, ok = entity.DragonFindPath(tx, withCrystals, 0, 6, &final)
		if !ok || path[len(path)-1] != final {
			t.Fatalf("path did not end at the final node: %v", path)
		}
	})
}

// TestDragonLandsAndRoarsAtNearbyPlayer runs a dragon from landing through
// scanning, roaring and breathing its flame, as the sitting phases do.
func TestDragonLandsAndRoarsAtNearbyPlayer(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dragon := addTestDragon(tx, mgl64.Vec3{.5, 64.2, .5}, entity.DragonLanding, dragonTestFight{})
		entity.SetDragonFlameCount(dragon, 3)
		dragon.Tick(tx, 1)
		if dragon.Phase() != entity.DragonSittingScanning || entity.DragonFlameCount(dragon) != 0 {
			t.Fatalf("landed dragon is in phase %d with flame count %d, want scanning with 0", dragon.Phase(), entity.DragonFlameCount(dragon))
		}
		addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 12.5})
		tick := int64(2)
		advance := func(phase entity.DragonPhase, limit int) {
			for range limit {
				if dragon.Phase() != phase {
					return
				}
				dragon.Tick(tx, tick)
				tick++
			}
			t.Fatalf("dragon stayed in phase %d for %d ticks", phase, limit)
		}
		advance(entity.DragonSittingScanning, 40)
		if dragon.Phase() != entity.DragonSittingAttacking {
			t.Fatalf("scanning dragon went to phase %d, want sitting attacking", dragon.Phase())
		}
		advance(entity.DragonSittingAttacking, 50)
		if dragon.Phase() != entity.DragonSittingFlaming || entity.DragonFlameCount(dragon) != 1 {
			t.Fatalf("roaring dragon went to phase %d with flame count %d", dragon.Phase(), entity.DragonFlameCount(dragon))
		}
		for range 12 {
			dragon.Tick(tx, tick)
			tick++
		}
		flame := entity.DragonFlame(dragon)
		if flame == nil {
			t.Fatal("flaming dragon breathed no cloud")
		}
		cloud, ok := flame.Entity(tx)
		if !ok {
			t.Fatal("dragon's breath was not added")
		}
		aec := cloud.(*entity.Ent).Behaviour().(*entity.AreaEffectCloudBehaviour)
		if aec.Radius() != 5 || aec.Owner() != dragon.H() || aec.Particle() == 0 {
			t.Fatalf("flame cloud radius %v owner %v particle %d", aec.Radius(), aec.Owner(), aec.Particle())
		}
		dragon.SetPhase(entity.DragonTakeoff)
		if _, ok := flame.Entity(tx); ok {
			t.Fatal("ending the flaming phase left its flame")
		}
	})
}

// TestDragonDamageRules checks the head and neck take full damage, other
// parts a quarter, arrows bounce off a sitting dragon, and a second hit
// within half a second only hurts as far as it exceeds the first.
func TestDragonDamageRules(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		attacker := addSurvivalPlayer(tx, mgl64.Vec3{20.5, 64, .5})
		src := entity.AttackDamageSource{Attacker: attacker}
		hit := func(part string, amount float64) float64 {
			dragon := addTestDragon(tx, mgl64.Vec3{.5, 100, .5}, entity.DragonHoldingPattern, nil)
			dealt, _ := dragon.HurtPart(part, amount, src)
			return dealt
		}
		if got := hit("neck", 8); got != 8 {
			t.Fatalf("neck took %v of 8, want all of it", got)
		}
		if got := hit("wing1", 8); got != 3 {
			t.Fatalf("wing took %v of 8, want 8/4+1", got)
		}
		dragon := addTestDragon(tx, mgl64.Vec3{.5, 100, .5}, entity.DragonHoldingPattern, nil)
		dragon.HurtPart("head", 6, src)
		if dealt, _ := dragon.HurtPart("head", 4, src); dealt != 0 {
			t.Fatalf("weaker hit within the cooldown dealt %v", dealt)
		}
		if dealt, _ := dragon.HurtPart("head", 10, src); dealt != 4 {
			t.Fatalf("stronger hit within the cooldown dealt %v, want 10-6", dealt)
		}
		other := addTestDragon(tx, mgl64.Vec3{.5, 100, .5}, entity.DragonHoldingPattern, nil)
		if dealt, _ := other.HurtPart("head", 50, entity.AttackDamageSource{}); dealt != 0 {
			t.Fatalf("damage from no player dealt %v", dealt)
		}

		sitting := addTestDragon(tx, mgl64.Vec3{.5, 64, .5}, entity.DragonSittingScanning, nil)
		arrow := tx.AddEntity(entity.NewArrow(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 66, 3}}, attacker))
		if dealt, _ := sitting.HurtPart("head", 9, entity.ProjectileDamageSource{Projectile: arrow, Owner: attacker}); dealt != 0 {
			t.Fatalf("arrow hurt a sitting dragon for %v", dealt)
		}
		if arrow.(*entity.Ent).OnFireDuration() <= 0 {
			t.Fatal("arrow bouncing off a sitting dragon was not set on fire")
		}
	})
}

// TestDragonFliesThroughBlocks checks the dragon's head, neck and body break
// what is not dragon immune while mob griefing is on, and stop at end stone.
func TestDragonFliesThroughBlocks(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dragon := addTestDragon(tx, mgl64.Vec3{.5, 80, .5}, entity.DragonHovering, nil)
		body := dragon.PartBoxes()[2].Box
		stone := cube.PosFromVec3(body.Min().Add(body.Max()).Mul(.5))
		tx.SetBlock(stone, block.Stone{}, nil)
		endStone := stone.Add(cube.Pos{0, 1, 0})
		tx.SetBlock(endStone, block.EndStone{}, nil)
		dragon.Tick(tx, 1)
		if _, ok := tx.Block(stone).(block.Air); !ok {
			t.Fatalf("dragon did not fly through stone at %v: %T", stone, tx.Block(stone))
		}
		if _, ok := tx.Block(endStone).(block.EndStone); !ok {
			t.Fatal("dragon broke dragon immune end stone")
		}
		if !entity.DragonInWall(dragon) {
			t.Fatal("end stone in the dragon's body did not slow it")
		}
	})
}

// TestGlassBottleCollectsDragonBreath checks a glass bottle takes dragon's
// breath from a cloud the dragon left, shrinking it, as BottleItem.use does.
func TestGlassBottleCollectsDragonBreath(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dragon := addTestDragon(tx, mgl64.Vec3{.5, 100, .5}, entity.DragonHovering, nil)
		user := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
		cloud := tx.AddEntity(entity.NewDragonBreathCloud(mgl64.Vec3{1.5, 64, .5}, dragon.H(), 3, 600, 0, 0))
		ctx := &item.UseContext{}
		if !(item.GlassBottle{}).Use(tx, user, ctx) {
			t.Fatal("bottle did not take dragon's breath")
		}
		if ctx.NewItem.Empty() || ctx.NewItem.Item() != (item.DragonBreath{}) {
			t.Fatalf("bottle became %v, want dragon's breath", ctx.NewItem)
		}
		if r := cloud.(*entity.Ent).Behaviour().(*entity.AreaEffectCloudBehaviour).Radius(); r != 2.5 {
			t.Fatalf("cloud radius %v after bottling, want 2.5", r)
		}
		_ = dragon.CloseIn(tx)
		if (item.GlassBottle{}).Use(tx, user, &item.UseContext{}) {
			t.Fatal("bottle took dragon's breath from a cloud whose dragon is gone")
		}
	})
}

// TestExplosionHurtsDragonHead checks an explosion at a sitting dragon's
// head hurts the head part in full.
func TestExplosionHurtsDragonHead(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dragon := addTestDragon(tx, mgl64.Vec3{.5, 100, .5}, entity.DragonSittingScanning, nil)
		head := dragon.PartBoxes()[0].Box
		dragon.Explode(dragonTestExplosion{head.Min().Add(head.Max()).Mul(.5), 5}, 0)
		if lost := 200 - dragon.Health(); lost < 30 {
			t.Fatalf("explosion at the head took %v health, want the full damage of a head hit", lost)
		}
	})
}

// TestDragonCirclesTheIsland checks a dragon in its holding pattern flies
// from node to node above the island.
func TestDragonCirclesTheIsland(t *testing.T) {
	w := dragonTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		dragon := addTestDragon(tx, mgl64.Vec3{0, 128, 0}, entity.DragonHoldingPattern, dragonTestFight{crystals: 10})
		start := dragon.Position()
		farthest := 0.0
		for tick := int64(1); tick <= 600; tick++ {
			dragon.Tick(tx, tick)
			pos := dragon.Position()
			for _, v := range pos {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("dragon position became %v at tick %d", pos, tick)
				}
			}
			if pos[1] < 60 || pos[1] > 200 {
				t.Fatalf("dragon left the sky above the island: %v at tick %d", pos, tick)
			}
			farthest = math.Max(farthest, math.Hypot(pos[0]-start[0], pos[2]-start[2]))
		}
		if farthest < 30 {
			t.Fatalf("dragon flew at most %v blocks from its spawn in 30 seconds", farthest)
		}
	})
}
