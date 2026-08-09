package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPlayerExplosionVelocityAndArmour(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	pos := mgl64.Vec3{1, 2, 3}
	initialVelocity := mgl64.Vec3{0.25, 0.5, -0.25}
	var health [3]float64
	<-w.Exec(func(tx *world.Tx) {
		for armourType := range 3 {
			armour := inventory.NewArmour(nil)
			helm := item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1)
			if armourType == 1 {
				helm = helm.WithEnchantments(item.NewEnchantment(enchantment.BlastProtection, 4))
			} else if armourType == 2 {
				helm = item.NewStack(item.Helmet{Tier: item.ArmourTierNetherite{}}, 1)
			}
			armour.SetHelmet(helm)
			conf := Config{Position: pos, Velocity: initialVelocity, Health: 100, MaxHealth: 100, Armour: armour}
			p := tx.AddEntity(world.EntitySpawnOpts{Position: pos, Velocity: initialVelocity}.New(Type, conf)).(*Player)
			p.Explode(playerTestExplosionSource{pos: pos}, 0.5)
			wantImpulse := 0.5
			if armourType == 2 {
				wantImpulse *= 0.9
			}
			if got, want := p.Velocity(), initialVelocity.Add(mgl64.Vec3{0, wantImpulse, 0}); got != want {
				t.Fatalf("armour type %v velocity = %v, want %v", armourType, got, want)
			}
			health[armourType] = p.Health()
		}
	})
	if health[1] <= health[0] {
		t.Fatalf("Blast Protection health = %v, want greater than unprotected health %v", health[1], health[0])
	}
}

type playerTestExplosionSource struct{ pos mgl64.Vec3 }

func (s playerTestExplosionSource) Position() mgl64.Vec3 { return s.pos }
func (playerTestExplosionSource) Size() float64          { return 1 }
