package entity

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestHorseArmourProtects checks horse armour lowers the damage armour
// reduces as CombatRules.getDamageAfterAbsorb does with its body defence and
// toughness, leaves fall damage alone, and netherite armour also resists
// knockback.
func TestHorseArmourProtects(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		hit := func(armour world.Item, src world.DamageSource) float64 {
			horse := tx.AddEntity(NewHorse(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 100, 0}})).(*Horse)
			if armour != nil {
				if err := horse.gearInventory().SetItem(1, item.NewStack(armour, 1)); err != nil {
					t.Fatal(err)
				}
			}
			dealt, _ := horse.Hurt(10, src)
			return dealt
		}
		attack := AttackDamageSource{}
		if got := hit(nil, attack); got != 10 {
			t.Fatalf("bare horse took %v of a 10 damage hit, want 10", got)
		}
		// Diamond: armour 11, toughness 2: max(11-10/2.5, 2.2) = 7, 10*(1-7/25).
		if got, want := hit(item.DiamondHorseArmour{}, attack), 10*(1-7.0/25); math.Abs(got-want) > 1e-5 {
			t.Fatalf("horse in diamond armour took %v, want %v", got, want)
		}
		// Netherite: armour 19, toughness 3: 19-10/2.75 = 15.36...
		if got, want := hit(item.NetheriteHorseArmour{}, attack), 10*(1-float64(float32(19)-float32(10)/2.75)/25); math.Abs(got-want) > 1e-4 {
			t.Fatalf("horse in netherite armour took %v, want %v", got, want)
		}
		if got := hit(item.DiamondHorseArmour{}, FallDamageSource{}); got != 10 {
			t.Fatalf("armoured horse took %v of 10 fall damage, want all of it", got)
		}

		pushed := func(armour world.Item) float64 {
			horse := tx.AddEntity(NewHorse(world.EntitySpawnOpts{Position: mgl64.Vec3{5, 100, 0}})).(*Horse)
			if armour != nil {
				_ = horse.gearInventory().SetItem(1, item.NewStack(armour, 1))
			}
			horse.KnockBack(mgl64.Vec3{4, 100, 0}, 0.4, 0.4)
			return horse.Velocity()[0]
		}
		if bare, netherite := pushed(nil), pushed(item.NetheriteHorseArmour{}); math.Abs(netherite-bare*0.9) > 1e-9 {
			t.Fatalf("netherite-armoured horse pushed %v, want 90%% of %v", netherite, bare)
		}
	})
}
