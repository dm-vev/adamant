package entity

import (
	"testing"
	"time"

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
