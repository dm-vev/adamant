package entity_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spawnHappyGhast spawns a happy ghast at a position with the NBT given.
func spawnHappyGhast(t *testing.T, tx *world.Tx, pos mgl64.Vec3, nbt map[string]any) *world.EntityHandle {
	t.Helper()
	data := map[string]any{"id": "minecraft:happy_ghast"}
	for k, v := range nbt {
		data[k] = v
	}
	h, err := entity.NewFromStructureNBT(data, world.EntitySpawnOpts{Position: pos, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	return tx.AddEntity(h).H()
}

// happyGhastWorld returns a world with ground eight blocks thick, which
// ghastlings do not fly beneath.
func happyGhastWorld(t *testing.T) *world.World {
	t.Helper()
	w := wolfTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -20; x <= 20; x++ {
			for z := -20; z <= 20; z++ {
				for y := 55; y < 63; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
				}
			}
		}
	})
	return w
}

func happyGhast(tx *world.Tx, h *world.EntityHandle) *entity.HappyGhast {
	e, _ := h.Entity(tx)
	return e.(*entity.HappyGhast)
}

// TestHappyGhastsFloatAround checks Ghast.RandomFloatAroundGoal with the
// careful GhastMoveControl: a happy ghast floats about without touching the
// ground.
func TestHappyGhastsFloatAround(t *testing.T) {
	w := happyGhastWorld(t)
	var h *world.EntityHandle
	start := mgl64.Vec3{.5, 70, .5}
	<-w.Exec(func(tx *world.Tx) { h = spawnHappyGhast(t, tx, start, nil) })
	moved := 0.0
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			pos := happyGhast(tx, h).Position()
			moved = max(moved, pos.Sub(start).Len())
			// Careful, it keeps a block away from the ground.
			if math.Abs(pos[0]) < 18 && math.Abs(pos[2]) < 18 && pos[1] < 65 {
				t.Fatalf("happy ghast sank to %v above the ground", pos)
			}
		})
	}
	if moved < 2 {
		t.Fatalf("happy ghast floated at most %.2f blocks in half a minute", moved)
	}
}

// TestHappyGhastsStayStillUnderPlayers checks HappyGhast.scanPlayerAboveGhast
// and the still timeout: a happy ghast with a player on top stops.
func TestHappyGhastsStayStillUnderPlayers(t *testing.T) {
	w := happyGhastWorld(t)
	var h, p *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = spawnHappyGhast(t, tx, mgl64.Vec3{.5, 70, .5}, nil)
		p = addSurvivalPlayer(tx, mgl64.Vec3{.5, 74.2, .5}).H()
	})
	var before mgl64.Vec3
	for i := range 100 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			g := happyGhast(tx, h)
			// The client of the player stands on the happy ghast.
			pl, _ := p.Entity(tx)
			pl.(*player.Player).Teleport(g.Position().Add(mgl64.Vec3{0, 4.2}))
			if !g.StaysStill() {
				t.Fatalf("happy ghast with a player on top does not stay still after %d ticks", i+1)
			}
			if i > 0 && g.Position() != before {
				t.Fatalf("happy ghast with a player on top moved from %v to %v", before, g.Position())
			}
			before = g.Position()
		})
	}
}

// TestHappyGhastsFollowSnowballs checks TemptGoal.ForNonPathfinders: a happy
// ghast floats towards a player holding a snowball and stays near them.
func TestHappyGhastsFollowSnowballs(t *testing.T) {
	w := happyGhastWorld(t)
	var h *world.EntityHandle
	player := mgl64.Vec3{14.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = spawnHappyGhast(t, tx, mgl64.Vec3{.5, 70, .5}, nil)
		p := addSurvivalPlayer(tx, player)
		p.SetHeldItems(item.NewStack(item.Snowball{}, 16), item.Stack{})
	})
	for i := range 800 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			pos := happyGhast(tx, h).Position()
			// Floating about on its own, it strays from the player within
			// half a minute.
			if d := (mgl64.Vec2{pos[0] - player[0], pos[2] - player[2]}).Len(); i >= 200 && d >= 8 {
				t.Fatalf("happy ghast tempted by a snowball %.1f blocks from the player after %d ticks", d, i+1)
			}
		})
	}
}

// TestGhastlingsFollowPlayers checks BabyFollowAdult with
// NEAREST_VISIBLE_PLAYER: a ghastling flies up to a player nearby.
func TestGhastlingsFollowPlayers(t *testing.T) {
	w := happyGhastWorld(t)
	var h *world.EntityHandle
	player := mgl64.Vec3{6.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = spawnHappyGhast(t, tx, mgl64.Vec3{.5, 66, .5}, map[string]any{"Age": int32(-24000)})
		addSurvivalPlayer(tx, player)
	})
	// Ghastlings fly about a block a second, and may first fly ten blocks
	// away.
	for range 1200 {
		w.AdvanceTick()
		near := false
		<-w.Exec(func(tx *world.Tx) { near = happyGhast(tx, h).Position().Sub(player).Len() < 3.5 })
		if near {
			return
		}
	}
	t.Fatal("ghastling did not follow a player six blocks away in a minute")
}

// TestGhastlingsGrowUpOnSnowballs checks Animal.mobInteract for happy
// ghasts: a snowball lets a ghastling grow up a tenth of its remaining time
// faster.
func TestGhastlingsGrowUpOnSnowballs(t *testing.T) {
	w := happyGhastWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		h := spawnHappyGhast(t, tx, mgl64.Vec3{.5, 66, .5}, map[string]any{"Age": int32(-24000)})
		p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
		if !feed(t, tx, p, h, item.Snowball{}) {
			t.Fatal("ghastling did not eat a snowball")
		}
		if age := happyGhast(tx, h).AgeTicks(); age != -21600 {
			t.Fatalf("ghastling age after a snowball=%d, want -21600", age)
		}
	})
}

// TestHappyGhastsHealSlowly checks HappyGhast.continuousHeal: outside rain
// and clouds a happy ghast heals a point every 30 seconds.
func TestHappyGhastsHealSlowly(t *testing.T) {
	w := happyGhastWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = spawnHappyGhast(t, tx, mgl64.Vec3{.5, 70, .5}, map[string]any{"Health": float32(10)})
	})
	for range 600 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if health := happyGhast(tx, h).Health(); health != 11 {
			t.Fatalf("happy ghast health after 30 seconds=%v, want 11", health)
		}
	})
}

// TestHappyGhastsFlyWhereRidersLook checks HappyGhast.getRiddenInput and
// travel: a player riding a harnessed happy ghast flies it forward.
func TestHappyGhastsFlyWhereRidersLook(t *testing.T) {
	w := happyGhastWorld(t)
	var h, p *world.EntityHandle
	start := mgl64.Vec3{.5, 70, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = spawnHappyGhast(t, tx, start, nil)
		pl := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 3.5})
		p = pl.H()
		g := happyGhast(tx, h)
		if !g.EquipHarness(tx, pl, item.Harness{Colour: item.ColourWhite()}) {
			t.Fatal("happy ghast did not take a harness")
		}
		if !g.Mount(tx, pl) {
			t.Fatal("player did not get on a harnessed happy ghast")
		}
	})
	for range 100 {
		<-w.Exec(func(tx *world.Tx) {
			pl, _ := p.Entity(tx)
			happyGhast(tx, h).SetVehicleInputFrom(pl, 0, 1)
		})
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		pos := happyGhast(tx, h).Position()
		if pos[2]-start[2] < 5 {
			t.Fatalf("ridden happy ghast went from %v to %v in five seconds, want five blocks forward", start, pos)
		}
	})
}

// TestHappyGhastsRiseWhenRidersJump checks the jump of
// HappyGhast.getRiddenInput: a happy ghast whose rider jumps flies up, and
// the jump does not take the rider off it.
func TestHappyGhastsRiseWhenRidersJump(t *testing.T) {
	w := happyGhastWorld(t)
	var h, p *world.EntityHandle
	start := mgl64.Vec3{.5, 70, .5}
	<-w.Exec(func(tx *world.Tx) {
		h = spawnHappyGhast(t, tx, start, nil)
		pl := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 3.5})
		p = pl.H()
		g := happyGhast(tx, h)
		g.EquipHarness(tx, pl, item.Harness{Colour: item.ColourWhite()})
		g.Mount(tx, pl)
	})
	for range 100 {
		<-w.Exec(func(tx *world.Tx) {
			g := happyGhast(tx, h)
			if !g.RideJump() {
				t.Fatal("happy ghast does not take the jump of its rider")
			}
			g.SetVehicleJumping(true)
		})
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		pos := happyGhast(tx, h).Position()
		if pos[1]-start[1] < 3 {
			t.Fatalf("happy ghast whose rider jumps went from %v to %v in five seconds, want three blocks up", start, pos)
		}
		pl, _ := p.Entity(tx)
		if pl.(entity.Rider).Riding() != h {
			t.Fatal("jumping took the rider off the happy ghast")
		}
	})
}
