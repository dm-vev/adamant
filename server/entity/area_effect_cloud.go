package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"math"
	"time"
)

// NewAreaEffectCloud creates the cloud of a lingering potion and returns it.
func NewAreaEffectCloud(opts world.EntitySpawnOpts, p potion.Potion) *world.EntityHandle {
	return newLingeringCloud(opts, p, nil)
}

// newLingeringCloud returns the cloud ThrownLingeringPotion.onHitAsPotion
// leaves: radius 3 shrinking by 0.5 a use and 3/600 a tick for 600 ticks
// after a wait of 10, a quarter of the potion's durations, effects again
// after AreaEffectCloud's reapplication delay of 20 ticks.
func newLingeringCloud(opts world.EntitySpawnOpts, p potion.Potion, owner *world.EntityHandle) *world.EntityHandle {
	return opts.New(AreaEffectCloudType, AreaEffectCloudBehaviourConfig{
		Potion:             p,
		Radius:             3,
		RadiusUseGrowth:    -0.5,
		RadiusTickGrowth:   -3.0 / 600,
		Duration:           30 * time.Second,
		ReapplicationDelay: time.Second,
		WaitTime:           time.Second / 2,
		DurationScale:      0.25,
		Owner:              owner,
	})
}

// newCreeperCloud returns the cloud Creeper.spawnLingeringCloud leaves of
// the effects of an exploding creeper: radius 2.5 shrinking by 0.5 a use
// and 2.5/300 a tick for 300 ticks after a wait of 10, a quarter of the
// effects' durations.
func newCreeperCloud(pos mgl64.Vec3, effects []effect.Effect) *world.EntityHandle {
	return world.EntitySpawnOpts{Position: pos}.New(AreaEffectCloudType, AreaEffectCloudBehaviourConfig{
		Radius:             2.5,
		RadiusUseGrowth:    -0.5,
		RadiusTickGrowth:   -2.5 / 300,
		Duration:           15 * time.Second,
		ReapplicationDelay: time.Second,
		WaitTime:           time.Second / 2,
		DurationScale:      0.25,
		Effects:            effects,
	})
}

// NewAreaEffectCloudWith ...
func NewAreaEffectCloudWith(opts world.EntitySpawnOpts, t potion.Potion, duration, reapplicationDelay, durationOnUse time.Duration, radius, radiusOnUse, radiusGrowth float64) *world.EntityHandle {
	config := AreaEffectCloudBehaviourConfig{
		Potion:             t,
		Radius:             radius,
		RadiusUseGrowth:    radiusOnUse,
		RadiusTickGrowth:   radiusGrowth,
		Duration:           duration,
		DurationUseGrowth:  durationOnUse,
		ReapplicationDelay: reapplicationDelay,
		WaitTime:           time.Second / 2,
		DurationScale:      0.25,
	}
	return opts.New(AreaEffectCloudType, config)
}

// AreaEffectCloudType is a world.EntityType implementation for AreaEffectCloud.
var AreaEffectCloudType areaEffectCloudType

type areaEffectCloudType struct{}

func (t areaEffectCloudType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (areaEffectCloudType) EncodeEntity() string { return "minecraft:area_effect_cloud" }
func (areaEffectCloudType) BBox(e world.Entity) cube.BBox {
	r := e.(*Ent).Behaviour().(*AreaEffectCloudBehaviour).Radius()
	return cube.Box(-r, 0, -r, r, 0.5, r)
}

func (areaEffectCloudType) DecodeNBT(m map[string]any, data *world.EntityData) {
	// AreaEffectCloud.readAdditionalSaveData: a wait of 20 ticks unless
	// saved; clouds saved before the scale was kept had a quarter.
	wait, scale := time.Second, 0.25
	if _, ok := m["WaitTime"]; ok {
		wait = nbtconv.TickDuration[int32](m, "WaitTime")
	}
	if _, ok := m["PotionDurationScale"]; ok {
		scale = float64(nbtconv.Float32(m, "PotionDurationScale"))
	}
	var effects []effect.Effect
	for _, raw := range nbtconv.Slice(m, "mobEffects") {
		if e, ok := decodeEffectEntry(raw); ok {
			effects = append(effects, e)
		}
	}
	data.Data = AreaEffectCloudBehaviourConfig{
		WaitTime:           wait,
		DurationScale:      scale,
		Effects:            effects,
		Potion:             potion.From(nbtconv.Int32(m, "PotionId")),
		Radius:             float64(nbtconv.Float32(m, "Radius")),
		RadiusUseGrowth:    float64(nbtconv.Float32(m, "RadiusOnUse")),
		RadiusTickGrowth:   float64(nbtconv.Float32(m, "RadiusPerTick")),
		Duration:           nbtconv.TickDuration[int32](m, "Duration"),
		DurationUseGrowth:  nbtconv.TickDuration[int32](m, "DurationOnUse"),
		ReapplicationDelay: nbtconv.TickDuration[int32](m, "ReapplicationDelay"),
		Particle:           nbtconv.Int32(m, "ParticleId"),
	}.New()
}

func (areaEffectCloudType) EncodeNBT(data *world.EntityData) map[string]any {
	a := data.Data.(*AreaEffectCloudBehaviour)
	m := map[string]any{
		"PotionId":            int32(a.conf.Potion.Uint8()),
		"ReapplicationDelay":  durationToTicks(a.conf.ReapplicationDelay),
		"RadiusPerTick":       float32(a.conf.RadiusTickGrowth),
		"RadiusOnUse":         float32(a.conf.RadiusUseGrowth),
		"DurationOnUse":       durationToTicks(a.conf.DurationUseGrowth),
		"Radius":              float32(a.radius),
		"Duration":            durationToTicks(a.duration),
		"WaitTime":            durationToTicks(a.conf.WaitTime),
		"PotionDurationScale": float32(a.conf.DurationScale),
	}
	if len(a.conf.Effects) > 0 {
		list := make([]any, 0, len(a.conf.Effects))
		for _, e := range a.conf.Effects {
			if entry, ok := encodeEffectEntry(e); ok {
				list = append(list, entry)
			}
		}
		m["mobEffects"] = list
	}
	if a.conf.Particle != 0 {
		m["ParticleId"] = a.conf.Particle
	}
	return m
}

// newDragonBreathCloud returns a cloud of dragon's breath, as the ender
// dragon breathes and its fireballs leave: Instant Damage of the amplifier,
// lasting the ticks given, that a glass bottle may collect.
func newDragonBreathCloud(pos mgl64.Vec3, owner *world.EntityHandle, radius float64, ticks int, radiusPerTick float64, amplifier int) *world.EntityHandle {
	p := potion.Harming()
	if amplifier > 0 {
		p = potion.StrongHarming()
	}
	return world.EntitySpawnOpts{Position: pos}.New(AreaEffectCloudType, AreaEffectCloudBehaviourConfig{
		Potion:             p,
		Radius:             radius,
		RadiusTickGrowth:   radiusPerTick,
		Duration:           time.Duration(ticks) * time.Second / 20,
		ReapplicationDelay: time.Second,
		WaitTime:           time.Second,
		DurationScale:      0.25,
		Owner:              owner,
		Particle:           packet.ParticleTypeDragonBreath,
	})
}

// durationToTicks converts a duration to the tick units used in NBT payloads.
func durationToTicks(d time.Duration) int32 {
	if d <= 0 {
		return 0
	}
	ticks := d / (time.Second / 20)
	if ticks > math.MaxInt32 {
		return math.MaxInt32
	}
	return int32(ticks)
}

// BottleDragonBreath lets a glass bottle take a half block of the radius of
// a cloud of dragon's breath whose dragon is still there, as BottleItem.use
// does. It reports if the cloud was one.
func (e *Ent) BottleDragonBreath(tx *world.Tx) bool {
	a, ok := e.Behaviour().(*AreaEffectCloudBehaviour)
	if !ok || a.stationary.close || a.conf.Owner == nil {
		return false
	}
	owner, ok := a.conf.Owner.Entity(tx)
	if _, dragon := owner.(*EnderDragon); !ok || !dragon {
		return false
	}
	a.SetRadius(a.radius - .5)
	viewers := tx.Viewers(e.Position())
	for _, v := range viewers {
		v.ViewEntityState(e)
	}
	tx.ReleaseViewers(viewers)
	return true
}
