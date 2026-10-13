package entity_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// addMob adds a freshly spawned mob of the Java type passed at pos.
func addMob(t *testing.T, tx *world.Tx, id string, pos mgl64.Vec3) entity.Living {
	t.Helper()
	h, err := entity.NewFromJavaNBT(map[string]any{"id": id}, world.EntitySpawnOpts{Position: pos, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	return tx.AddEntity(h).(entity.Living)
}

// countEntities counts the entities of the type passed in the world.
func countEntities(tx *world.Tx, id string) (n int) {
	for e := range tx.Entities() {
		if e.H().Type().EncodeEntity() == id {
			n++
		}
	}
	return n
}

// killWith kills a zombie on a stone floor at the origin while it has the
// effect passed.
func killWith(t *testing.T, w *world.World, t2 effect.LastingType) {
	t.Helper()
	<-w.Exec(func(tx *world.Tx) {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		zombie := addMob(t, tx, "minecraft:zombie", mgl64.Vec3{0.5, 64, 0.5})
		zombie.AddEffect(effect.New(t2, 1, time.Minute))
		zombie.Hurt(1000, entity.VoidDamageSource{})
	})
}

func TestInfestedAndOozingImmunities(t *testing.T) {
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		cases := []struct {
			id     string
			effect effect.LastingType
			takes  bool
		}{
			{"minecraft:silverfish", effect.Infested, false},
			{"minecraft:slime", effect.Oozing, false},
			{"minecraft:silverfish", effect.Oozing, true},
			{"minecraft:slime", effect.Infested, true},
			{"minecraft:zombie", effect.Infested, true},
		}
		for i, c := range cases {
			mob := addMob(t, tx, c.id, mgl64.Vec3{float64(i * 4), 64, 0})
			mob.AddEffect(effect.New(c.effect, 1, time.Minute))
			has := false
			for _, e := range mob.Effects() {
				has = has || e.Type() == c.effect
			}
			if has != c.takes {
				t.Errorf("%s with %T: has effect %v, want %v", c.id, c.effect, has, c.takes)
			}
		}
	})
}

func TestOozingReleasesTwoSlimesLimitedByCramming(t *testing.T) {
	for _, c := range []struct {
		cramming, nearby, want int
	}{{24, 0, 2}, {2, 1, 1}, {1, 1, 0}, {0, 3, 2}} {
		w := newDeathTestWorld(t)
		if err := w.SetGameRule("max_entity_cramming", c.cramming); err != nil {
			t.Fatal(err)
		}
		<-w.Exec(func(tx *world.Tx) {
			for range c.nearby {
				addMob(t, tx, "minecraft:slime", mgl64.Vec3{1.5, 64, 0.5})
			}
		})
		killWith(t, w, effect.Oozing)
		<-w.Exec(func(tx *world.Tx) {
			if got := countEntities(tx, "minecraft:slime") - c.nearby; got != c.want {
				t.Errorf("cramming %d with %d slimes nearby: %d slimes released, want %d", c.cramming, c.nearby, got, c.want)
			}
		})
	}
}

func TestWeavingSpreadsCobwebsUnlessMobGriefingIsOff(t *testing.T) {
	for _, griefing := range []bool{true, false} {
		w := newDeathTestWorld(t)
		if err := w.SetGameRule("mob_griefing", griefing); err != nil {
			t.Fatal(err)
		}
		killWith(t, w, effect.Weaving)
		<-w.Exec(func(tx *world.Tx) {
			webs := 0
			for x := -1; x <= 1; x++ {
				for y := 63; y <= 65; y++ {
					for z := -1; z <= 1; z++ {
						if _, ok := tx.Block(cube.Pos{x, y, z}).(block.Cobweb); ok {
							if y != 64 {
								t.Fatalf("cobweb at %v without a floor below", cube.Pos{x, y, z})
							}
							webs++
						}
					}
				}
			}
			if griefing && (webs < 2 || webs > 3) || !griefing && webs != 0 {
				t.Fatalf("mob griefing %v: %d cobwebs", griefing, webs)
			}
		})
	}
}

func TestWindChargedDeathBlowsNearbyMobsAway(t *testing.T) {
	w := newDeathTestWorld(t)
	var bystander *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		bystander = addMob(t, tx, "minecraft:pig", mgl64.Vec3{2.5, 64, 0.5}).H()
	})
	killWith(t, w, effect.WindCharged)
	<-w.Exec(func(tx *world.Tx) {
		e, _ := bystander.Entity(tx)
		if vel := e.(interface{ Velocity() mgl64.Vec3 }).Velocity(); vel[0] <= 0 || vel[1] <= 0 {
			t.Fatalf("pig next to the gust has velocity %v, want away and up", vel)
		}
	})
}

func TestInfestedReleasesSilverfishWhenHurt(t *testing.T) {
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		golem := addMob(t, tx, "minecraft:iron_golem", mgl64.Vec3{0.5, 64, 0.5})
		golem.AddEffect(effect.New(effect.Infested, 1, time.Minute))
		// Each hurt has a 10% chance: 200 hurts all failing is ~1e-9 likely.
		for range 200 {
			golem.Hurt(0.01, entity.VoidDamageSource{})
		}
		if n := countEntities(tx, "minecraft:silverfish"); n < 2 || n > 120 {
			t.Fatalf("200 hurts released %d silverfish", n)
		}
	})
}

func TestTrialPotionsGiveTheirEffects(t *testing.T) {
	cases := []struct {
		potion potion.Potion
		effect effect.LastingType
		javaID string
	}{
		{potion.WindCharging(), effect.WindCharged, "minecraft:wind_charged"},
		{potion.Weaving(), effect.Weaving, "minecraft:weaving"},
		{potion.Oozing(), effect.Oozing, "minecraft:oozing"},
		{potion.Infestation(), effect.Infested, "minecraft:infested"},
	}
	for _, c := range cases {
		effects := c.potion.Effects()
		if len(effects) != 1 || effects[0].Type() != c.effect || effects[0].Duration() != 3*time.Minute {
			t.Errorf("%s gives %v", c.javaID, effects)
		}
		if p, ok := potion.FromJavaID(c.javaID); !ok || p != c.potion || p.JavaID() != c.javaID {
			t.Errorf("Java potion %s maps to %v (%v)", c.javaID, p, ok)
		}
	}
}

func TestEffectNetworkIDs(t *testing.T) {
	for id, want := range map[int]effect.Type{
		31: effect.TrialOmen, 32: effect.RaidOmen, 33: effect.WindCharged, 34: effect.Weaving,
		35: effect.Oozing, 36: effect.Infested, 37: effect.BreathOfTheNautilus,
	} {
		if got, ok := effect.ByID(id); !ok || got != want {
			t.Errorf("effect %d is %T, want %T", id, got, want)
		}
	}
}
