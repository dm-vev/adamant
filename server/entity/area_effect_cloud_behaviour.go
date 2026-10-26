package entity

import (
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"time"
)

// AreaEffectCloudBehaviourConfig contains optional parameters for an area
// effect cloud entity.
type AreaEffectCloudBehaviourConfig struct {
	Potion potion.Potion
	// Radius specifies the initial radius of the cloud. Defaults to 3.0.
	Radius float64
	// RadiusUseGrowth is the value that is added to the radius every time the
	// effect cloud is used/consumed. This is typically a negative value. (-0.5)
	RadiusUseGrowth float64
	// RadiusTickGrowth is the value added to the radius every tick. This is
	// typically a negative value. (-0.005)
	RadiusTickGrowth float64
	// Duration specifies the initial duration of the cloud. Defaults to 30s.
	Duration time.Duration
	// DurationUseGrowth is the duration that is added to the effect cloud every
	// time it is used/consumed. This is 0 in normal situations.
	DurationUseGrowth time.Duration
	// ReapplicationDelay specifies the delay with which the effects from the
	// cloud can be re-applied to users.
	ReapplicationDelay time.Duration
	// WaitTime is how long the cloud waits after it spawns before it
	// spreads effects and its duration starts.
	WaitTime time.Duration
	// Effects are applied besides those of Potion, as the effects a creeper
	// leaves when it explodes.
	Effects []effect.Effect
	// DurationScale scales the duration of lasting effects. Zero means 1.
	DurationScale float64
	// Owner is the entity the cloud came from, if any, as the ender dragon
	// whose breath may be bottled.
	Owner *world.EntityHandle
	// Particle is the Bedrock particle type the cloud shows, or 0 for that
	// of its potion.
	Particle int32
}

func (conf AreaEffectCloudBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates an AreaEffectCloudBehaviour using the parameter in conf and t.
func (conf AreaEffectCloudBehaviourConfig) New() *AreaEffectCloudBehaviour {
	if conf.Radius == 0 {
		conf.Radius = 3.0
	}
	if conf.Duration == 0 {
		conf.Duration = time.Second * 30
	}
	if conf.DurationScale == 0 {
		conf.DurationScale = 1
	}
	conf.Effects = append([]effect.Effect(nil), conf.Effects...)
	return &AreaEffectCloudBehaviour{
		conf:       conf,
		stationary: StationaryBehaviourConfig{}.New(),
		duration:   conf.Duration,
		radius:     conf.Radius,
		targets:    make(map[*world.EntityHandle]time.Duration),
	}
}

// AreaEffectCloudBehaviour is the cloud that is created when: lingering
// potions are thrown; creepers with potion effects explode; dragon fireballs
// hit the ground.
type AreaEffectCloudBehaviour struct {
	conf AreaEffectCloudBehaviourConfig

	stationary *StationaryBehaviour

	duration time.Duration
	radius   float64
	targets  map[*world.EntityHandle]time.Duration
}

// PortalTravelComputer returns the interdimensional travel state for the behaviour.
func (a *AreaEffectCloudBehaviour) PortalTravelComputer() *PortalTravelComputer {
	return a.stationary.PortalTravelComputer()
}

// Radius returns the current radius of the area effect cloud.
func (a *AreaEffectCloudBehaviour) Radius() float64 {
	return a.radius
}

// SetRadius sets the radius of the cloud, which disappears once it is under
// one half.
func (a *AreaEffectCloudBehaviour) SetRadius(r float64) {
	a.radius = r
	if a.radius < .5 {
		a.stationary.close = true
	}
}

// Owner returns the entity the cloud came from, or nil.
func (a *AreaEffectCloudBehaviour) Owner() *world.EntityHandle {
	return a.conf.Owner
}

// Particle returns the Bedrock particle type the cloud shows, or 0 for that
// of its potion.
func (a *AreaEffectCloudBehaviour) Particle() int32 {
	return a.conf.Particle
}

// Effects returns the effects the area effect cloud provides.
func (a *AreaEffectCloudBehaviour) Effects() []effect.Effect {
	return append(a.conf.Potion.Effects(), a.conf.Effects...)
}

// Tick ports AreaEffectCloud.serverTick: after its wait time the cloud
// shrinks or grows every tick, ends once its duration has passed, and every
// five ticks gives its effects to the living entities in reach.
func (a *AreaEffectCloudBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	a.stationary.Tick(e, tx)
	if a.stationary.close {
		return nil
	}
	if e.Age()-a.conf.WaitTime >= a.duration {
		a.stationary.close = true
		return nil
	}
	if e.Age() < a.conf.WaitTime {
		return nil
	}

	pos := e.Position()
	if a.conf.RadiusTickGrowth != 0 {
		if a.radius += a.conf.RadiusTickGrowth; a.radius < 0.5 {
			a.stationary.close = true
			return nil
		}
		a.viewState(e, tx)
	}

	if (e.Age()/(time.Second/20))%5 != 0 {
		return nil
	}
	for target, expiration := range a.targets {
		if e.Age() >= expiration {
			delete(a.targets, target)
		}
	}
	if a.applyEffects(pos, e, tx) {
		a.viewState(e, tx)
	}
	return nil
}

// viewState shows viewers the cloud's new radius.
func (a *AreaEffectCloudBehaviour) viewState(e *Ent, tx *world.Tx) {
	viewers := tx.Viewers(e.Position())
	for _, v := range viewers {
		v.ViewEntityState(e)
	}
	tx.ReleaseViewers(viewers)
}

// applyEffects gives the cloud's effects to the living entities whose box
// meets the cloud's and whose position lies within its radius, unless they
// were given them within the reapplication delay. Instant effects apply at
// half strength. It reports if the cloud changed.
func (a *AreaEffectCloudBehaviour) applyEffects(pos mgl64.Vec3, ent *Ent, tx *world.Tx) bool {
	effects := a.Effects()
	if len(effects) == 0 {
		clear(a.targets)
		return false
	}
	box := ent.H().Type().BBox(ent).Translate(pos)
	var update bool
	// EntitiesWithin goes by position, so the search reaches 4 blocks
	// further for the boxes of large mobs.
	for e := range tx.EntitiesWithin(box.Grow(4)) {
		l, living := e.(Living)
		if _, target := a.targets[e.H()]; !living || target || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
			continue
		}
		delta := e.Position().Sub(pos)
		if delta[0]*delta[0]+delta[2]*delta[2] > a.radius*a.radius {
			continue
		}
		a.targets[e.H()] = ent.Age() + a.conf.ReapplicationDelay
		for _, eff := range effects {
			if lasting, ok := eff.Type().(effect.LastingType); ok {
				l.AddEffect(effect.New(lasting, eff.Level(), time.Duration(float64(eff.Duration())*a.conf.DurationScale)))
				continue
			}
			l.AddEffect(effect.NewInstantWithPotency(eff.Type(), eff.Level(), 0.5))
		}
		update = true
		if a.conf.RadiusUseGrowth != 0 {
			if a.radius += a.conf.RadiusUseGrowth; a.radius < 0.5 {
				a.stationary.close = true
				return true
			}
		}
		if a.conf.DurationUseGrowth != 0 {
			if a.duration += a.conf.DurationUseGrowth; a.duration <= 0 {
				a.stationary.close = true
				return true
			}
		}
	}
	return update
}
