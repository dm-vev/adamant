package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// knockBackable is a living entity that may be knocked back.
type knockBackable interface {
	world.Entity
	Velocity() mgl64.Vec3
	SetVelocity(mgl64.Vec3)
	OnGround() bool
}

// knockBack ports LivingEntity.knockback for an entity with the knockback
// resistance entityKnockbackResistance finds.
func knockBack(e knockBackable, src mgl64.Vec3, force, height float64) {
	knockBackResisted(e, src, force, height, entityKnockbackResistance(e))
}

// knockBackResisted ports LivingEntity.knockback: the resistance takes off its
// share of the push, the entity keeps half its speed and is pushed away from
// src, and it is lifted up to the height only if it stands on the ground.
func knockBackResisted(e knockBackable, src mgl64.Vec3, force, height, resistance float64) {
	scale := 1 - min(1, max(0, resistance))
	if force*scale <= 0 && height*scale <= 0 {
		return
	}
	xd, zd := e.Position()[0]-src[0], e.Position()[2]-src[2]
	for xd*xd+zd*zd < 1e-5 {
		xd, zd = (rand.Float64()-rand.Float64())*.01, (rand.Float64()-rand.Float64())*.01
	}
	l := math.Sqrt(xd*xd + zd*zd)
	push := force * scale
	v := e.Velocity()
	y := v[1]
	if e.OnGround() {
		y = math.Min(height, v[1]/2+height*scale)
	}
	e.SetVelocity(mgl64.Vec3{v[0]/2 + xd/l*push, y, v[2]/2 + zd/l*push})
}

// entityKnockbackResistance returns the knockback resistance attribute of an
// entity.
func entityKnockbackResistance(e world.Entity) float64 {
	if k, ok := e.(interface{ KnockbackResistance() float64 }); ok {
		return k.KnockbackResistance()
	}
	if a, ok := e.(interface{ Armour() *inventory.Armour }); ok {
		return a.Armour().KnockBackResistance()
	}
	return 0
}
