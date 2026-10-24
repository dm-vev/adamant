package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// axolotlTestWorld returns a world with a walled pool of water depth deep
// from x -6 to 6, with dry stone east of it up to x 12.
func axolotlTestWorld(t *testing.T, depth int) *world.World {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -8; x <= 12; x++ {
			for z := -8; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 60, z}, block.Stone{}, nil)
				for y := 61; y <= 63; y++ {
					if x > -7 && x < 7 && z > -7 && z < 7 {
						if y < 61+depth {
							tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
						}
					} else {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
				if x <= -7 || z <= -7 || z >= 7 {
					for y := 64; y <= 66; y++ {
						tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
					}
				}
			}
		}
	})
	return w
}

func axolotlOf(tx *world.Tx, h *world.EntityHandle) *Axolotl {
	e, _ := h.Entity(tx)
	return e.(*Axolotl)
}

// TestAxolotlsFightDrowned checks AxolotlAttackablesSensor and the FIGHT
// activity: an axolotl kills a drowned in its pool, and then waits two
// minutes before it hunts again. The drowned stands still on the bottom, as
// an axolotl cannot bite what floats above it.
func TestAxolotlsFightDrowned(t *testing.T) {
	w := axolotlTestWorld(t, 3)
	var axolotl, drowned *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		axolotl = tx.AddEntity(NewAxolotl(world.EntitySpawnOpts{Position: mgl64.Vec3{-3.5, 61, .5}})).H()
		d := tx.AddEntity(NewDrowned(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 61, .5}}))
		drowned = d.H()
		m, _ := mobOf(d)
		m.AI.NoAI = true
	})
	for range 2400 {
		stepTicks(t, w, 1)
		gone := false
		<-w.Exec(func(tx *world.Tx) { _, alive := drowned.Entity(tx); gone = !alive })
		if gone {
			stepTicks(t, w, 2)
			<-w.Exec(func(tx *world.Tx) {
				if !axolotlOf(tx, axolotl).behaviour().br.has(memHasHuntingCooldown) {
					t.Fatal("axolotl that stopped fighting has no hunting cooldown")
				}
			})
			return
		}
	}
	t.Fatal("axolotl did not kill a drowned in its pool in two minutes")
}

// TestAxolotlsPlayDead checks Axolotl.hurtServer and PlayDead: an axolotl
// hurt in water by a mob sooner or later plays dead, regenerating, and may
// not be attacked meanwhile.
func TestAxolotlsPlayDead(t *testing.T) {
	w := axolotlTestWorld(t, 3)
	var axolotl, attacker *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		axolotl = tx.AddEntity(NewAxolotl(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 61, .5}})).H()
		// A cow, unlike a zombie, does not burn up in the sun.
		attacker = tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{9.5, 64, .5}})).H()
	})
	stepTicks(t, w, 2)
	for range 40 {
		playing := false
		<-w.Exec(func(tx *world.Tx) {
			a := axolotlOf(tx, axolotl)
			// Below half health, a hit makes the axolotl play dead with a
			// chance of one in three.
			a.behaviour().health.AddHealth(6 - a.Health())
			c, _ := attacker.Entity(tx)
			a.Hurt(.5, AttackDamageSource{Attacker: c})
		})
		stepTicks(t, w, 25)
		<-w.Exec(func(tx *world.Tx) {
			a := axolotlOf(tx, axolotl)
			if !a.PlayingDead() {
				return
			}
			playing = true
			if !hasEffect(a.Effects(), effect.Regeneration) {
				t.Fatal("axolotl playing dead does not regenerate")
			}
			if canBeSeenAsEnemy(a) {
				t.Fatal("axolotl playing dead may be attacked")
			}
		})
		if playing {
			return
		}
	}
	t.Fatal("axolotl hurt forty times in water never played dead")
}

// TestAxolotlsDryOut checks Axolotl.handleAirSupply: an axolotl out of water
// for five minutes takes damage.
func TestAxolotlsDryOut(t *testing.T) {
	w := axolotlTestWorld(t, 3)
	var axolotl *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		axolotl = tx.AddEntity(NewAxolotl(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 64, .5}})).H()
		axolotlOf(tx, axolotl).behaviour().airTicks = axolotlMaxAir - 10
	})
	hurt := false
	for range 40 {
		stepTicks(t, w, 1)
		<-w.Exec(func(tx *world.Tx) {
			a := axolotlOf(tx, axolotl)
			hurt = hurt || a.Health() < a.MaxHealth() && !pathWater(tx, cube.PosFromVec3(a.Position()))
		})
	}
	if !hurt {
		t.Fatal("axolotl out of water for five minutes did not dry out")
	}
}

// TestAxolotlsFindWater checks TryFindLiquid: an axolotl on land heads for
// the water nearby. It crawls a block in four seconds and may dither at the
// water's edge, so the test only follows it there.
func TestAxolotlsFindWater(t *testing.T) {
	w := axolotlTestWorld(t, 3)
	var axolotl *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		axolotl = tx.AddEntity(NewAxolotl(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 64, .5}})).H()
	})
	for range 1200 {
		stepTicks(t, w, 1)
		near := false
		<-w.Exec(func(tx *world.Tx) {
			a := axolotlOf(tx, axolotl)
			v, ok := a.behaviour().br.get(memWalkTarget)
			near = ok && pathWater(tx, v.(walkTarget).target.currentBlockPos(tx)) && a.Position()[0] < 7.5
		})
		if near {
			return
		}
	}
	t.Fatal("axolotl four blocks from water did not head for it in a minute")
}

func hasEffect(effects []effect.Effect, t effect.Type) bool {
	for _, e := range effects {
		if e.Type() == t {
			return true
		}
	}
	return false
}
