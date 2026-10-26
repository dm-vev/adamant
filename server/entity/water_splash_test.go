package entity

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestWaterSplashHurtsExtinguishesAndRehydrates checks a splash of water as
// AbstractThrownPotion.affectEntitiesAround: within four blocks it hurts a
// blaze and puts out a burning zombie, a blaze further away is unharmed, and
// an axolotl out of water gets 1800 ticks of air back.
func TestWaterSplashHurtsExtinguishesAndRehydrates(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		at := mgl64.Vec3{0.5, 100, 0.5}
		near := tx.AddEntity(NewBlaze(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{1, 0, 0})})).(*Blaze)
		far := tx.AddEntity(NewBlaze(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{4.5, 0, 0})})).(*Blaze)
		zombie := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{0, 0, 1})}))
		axolotl := tx.AddEntity(NewAxolotl(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{-2, 0, 0})})).(*Axolotl)
		axolotl.behaviour().airTicks = 5000
		burning := zombie.(interface {
			SetOnFire(time.Duration)
			OnFireDuration() time.Duration
		})
		burning.SetOnFire(5 * time.Second)

		thrower := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{20, 0, 0})}))
		splash := tx.AddEntity(NewSplashPotion(world.EntitySpawnOpts{Position: at}, potion.Water(), thrower)).(*Ent)
		potionSplash(1, potion.Water(), false)(splash, tx, nil)

		if near.Health() != near.MaxHealth()-1 {
			t.Fatalf("blaze beside the splash has %v health, want %v", near.Health(), near.MaxHealth()-1)
		}
		if far.Health() != far.MaxHealth() {
			t.Fatalf("blaze 4.5 blocks away has %v health, want it unharmed", far.Health())
		}
		if burning.OnFireDuration() > 0 {
			t.Fatal("burning zombie beside the splash was not extinguished")
		}
		if got := axolotl.behaviour().airTicks; got != 3200 {
			t.Fatalf("axolotl out of water for 5000 ticks has %d after the splash, want 3200", got)
		}
	})
}

// TestSplashPotionScalesByBoxDistance follows ThrownSplashPotion.onHitAsPotion:
// the effect scales by the distance between the potion's box and the
// entity's, so a cow 4.4 blocks from the impact, its side 3.825 blocks from
// the potion, still gets (1-3.825/4)*3600 ticks of Strength, while one 5
// blocks away is out of reach.
func TestSplashPotionScalesByBoxDistance(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		at := mgl64.Vec3{0.5, 100, 0.5}
		near := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{4.4, 0, 0})})).(*Cow)
		far := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{0, 0, 5})})).(*Cow)
		thrower := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{20, 0, 0})}))
		splash := tx.AddEntity(NewSplashPotion(world.EntitySpawnOpts{Position: at}, potion.Strength(), thrower)).(*Ent)
		potionSplash(1, potion.Strength(), false)(splash, tx, nil)

		if effects := near.Effects(); len(effects) != 1 || effects[0].Duration() < 157*time.Second/20 || effects[0].Duration() > 158*time.Second/20 {
			t.Fatalf("cow 4.4 blocks away got %v, want about 158 ticks of Strength", effects)
		}
		if effects := far.Effects(); len(effects) != 0 {
			t.Fatalf("cow 5 blocks away got %v", effects)
		}
	})
}

// TestLingeringCloudFollowsJava follows AreaEffectCloud.serverTick: nothing
// happens for the 10 ticks of waiting, a mob whose box reaches into the
// cloud gets a quarter of the potion's duration though its feet are below
// it, and the cloud shrinks away; dragon's breath ends 600 ticks after its
// wait. A creeper with an effect
// leaves a cloud of it when it explodes, and lingering water none.
func TestLingeringCloudFollowsJava(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{1.5, 100, 0.5}})).(*Cow)
		cloud := tx.AddEntity(newLingeringCloud(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100.5, 0.5}}, potion.Strength(), nil)).(*Ent)
		tick := int64(0)
		for ; tick < 10; tick++ {
			cloud.Tick(tx, tick)
		}
		if len(cow.Effects()) != 0 {
			t.Fatal("the cloud gave effects while it waited")
		}
		cloud.Tick(tx, tick)
		tick++
		if effects := cow.Effects(); len(effects) != 1 || effects[0].Duration() != 45*time.Second {
			t.Fatalf("cow below the cloud got %v, want 900 ticks of Strength", effects)
		}
		_ = tx.RemoveEntity(cow)
		// Used once, the radius of 2.495 falls by 3/600 a tick to under a
		// half on tick 410; the cloud closes on the tick after.
		for ; tick <= 410; tick++ {
			cloud.Tick(tx, tick)
		}
		if cloud.H().Closed() {
			t.Fatal("cloud closed before its radius fell under a half")
		}
		cloud.Tick(tx, tick)
		if !cloud.H().Closed() {
			t.Fatalf("cloud of radius %v still open", cloud.Behaviour().(*AreaEffectCloudBehaviour).Radius())
		}

		// Dragon's breath grows instead and ends 600 ticks after its wait of 20.
		breath := tx.AddEntity(newDragonBreathCloud(mgl64.Vec3{10.5, 100, 0.5}, nil, 3, 600, 4.0/600, 1)).(*Ent)
		for tick = 0; tick <= 620; tick++ {
			breath.Tick(tx, tick)
		}
		if breath.H().Closed() {
			t.Fatal("dragon's breath ended before 620 ticks")
		}
		breath.Tick(tx, tick)
		if !breath.H().Closed() {
			t.Fatal("dragon's breath outlived 600 ticks after its wait")
		}

		creeper := tx.AddEntity(NewCreeper(world.EntitySpawnOpts{Position: mgl64.Vec3{20.5, 100, 0.5}})).(*Creeper)
		creeper.AddEffect(effect.New(effect.Speed, 1, 30*time.Second))
		creeper.s().ignited = true
		for i := 0; i < 40 && !creeper.H().Closed(); i++ {
			creeper.Tick(tx, int64(i))
		}
		var clouds int
		for e := range tx.Entities() {
			if e.H().Type() == AreaEffectCloudType {
				clouds++
				if b := e.(*Ent).Behaviour().(*AreaEffectCloudBehaviour); b.Radius() != 2.5 || len(b.Effects()) != 1 {
					t.Fatalf("creeper cloud radius %v with %v", b.Radius(), b.Effects())
				}
			}
		}
		if clouds != 1 {
			t.Fatalf("%d clouds after the creeper exploded, want 1", clouds)
		}

		thrower := tx.AddEntity(NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{40.5, 100, 0.5}}))
		water := tx.AddEntity(NewLingeringPotion(world.EntitySpawnOpts{Position: mgl64.Vec3{30.5, 100, 0.5}}, potion.Water(), thrower)).(*Ent)
		potionSplash(0.25, potion.Water(), true)(water, tx, nil)
		for e := range tx.Entities() {
			if e.H().Type() == AreaEffectCloudType && e.Position()[0] == 30.5 {
				t.Fatal("lingering water left a cloud")
			}
		}
	})
}
