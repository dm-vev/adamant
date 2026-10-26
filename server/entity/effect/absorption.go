package effect

import (
	"github.com/df-mc/dragonfly/server/world"
	"image/color"
)

// Absorption is a lasting effect that increases the health of an entity over
// the maximum. Once this extra health is lost, it will not regenerate.
var Absorption absorption

type absorption struct {
	nopLasting
}

// Start tops the entity's absorption up to four points a level, as
// AbsorptionMobEffect.onEffectStarted.
func (absorption) Start(e world.Entity, lvl int) {
	if i, ok := e.(interface {
		Absorption() float64
		SetAbsorption(health float64)
	}); ok {
		i.SetAbsorption(max(i.Absorption(), 4*float64(lvl)))
	}
}

// Restart tops the absorption up when the effect is added again but the
// existing one is kept, as LivingEntity.addEffect starts every effect added.
func (a absorption) Restart(e world.Entity, lvl int) {
	a.Start(e, lvl)
}

// End ...
func (absorption) End(e world.Entity, _ int) {
	if i, ok := e.(interface {
		SetAbsorption(health float64)
	}); ok {
		i.SetAbsorption(0)
	}
}

// RGBA ...
func (absorption) RGBA() color.RGBA {
	return color.RGBA{R: 0x25, G: 0x52, B: 0xa5, A: 0xff}
}
