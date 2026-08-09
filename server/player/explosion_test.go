package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPlayerExplosionVelocityAndBlastProtection(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()

	pos := mgl64.Vec3{1, 2, 3}
	initialVelocity := mgl64.Vec3{0.25, 0.5, -0.25}
	var health [2]float64
	<-w.Exec(func(tx *world.Tx) {
		for protected := range 2 {
			armour := inventory.NewArmour(nil)
			helm := item.NewStack(item.Helmet{Tier: item.ArmourTierLeather{}}, 1)
			if protected == 1 {
				helm = helm.WithEnchantments(item.NewEnchantment(enchantment.BlastProtection, 4))
			}
			armour.SetHelmet(helm)
			conf := Config{Position: pos, Velocity: initialVelocity, Health: 100, MaxHealth: 100, Armour: armour}
			p := tx.AddEntity(world.EntitySpawnOpts{Position: pos, Velocity: initialVelocity}.New(Type, conf)).(*Player)
			p.Explode(playerTestExplosionSource{pos: pos}, 0.5)
			if got, want := p.Velocity(), initialVelocity.Add(mgl64.Vec3{0, 0.5, 0}); got != want {
				t.Fatalf("protected=%v velocity = %v, want %v", protected == 1, got, want)
			}
			health[protected] = p.Health()
		}
	})
	if health[1] <= health[0] {
		t.Fatalf("Blast Protection health = %v, want greater than unprotected health %v", health[1], health[0])
	}
}

type playerTestExplosionSource struct{ pos mgl64.Vec3 }

func (s playerTestExplosionSource) Position() mgl64.Vec3 { return s.pos }
func (playerTestExplosionSource) Size() float64          { return 1 }
