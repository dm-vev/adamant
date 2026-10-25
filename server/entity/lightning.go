package entity

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewLightning creates a lightning entity. The lightning entity will be
// positioned at the position passed. Lightning is a lethal element to
// thunderstorms. Lightning momentarily increases the skylight's brightness to
// slightly greater than full daylight.
func NewLightning(opts world.EntitySpawnOpts) *world.EntityHandle {
	return NewLightningWithDamage(opts, 5, true, time.Second*8)
}

// NewLightningWithDamage creates a new lightning entities using the damage and
// fire properties passed.
func NewLightningWithDamage(opts world.EntitySpawnOpts, dmg float64, blockFire bool, entityFireDuration time.Duration) *world.EntityHandle {
	conf := lightningConf
	conf.Tick = (&lightningState{
		Damage:             dmg,
		EntityFireDuration: entityFireDuration,
		BlockFire:          blockFire,
		state:              2,
		lifetime:           rand.IntN(3) + 1,
	}).tick
	return opts.New(LightningType, conf)
}

// NewVisualLightning creates a lightning bolt that neither hurts nor sets
// anything on fire, as LightningBolt.setVisualOnly makes the one that
// springs a skeleton trap.
func NewVisualLightning(opts world.EntitySpawnOpts) *world.EntityHandle {
	conf := lightningConf
	conf.Tick = (&lightningState{visualOnly: true, state: 2, lifetime: rand.IntN(3) + 1}).tick
	return opts.New(LightningType, conf)
}

var lightningConf = StationaryBehaviourConfig{SpawnSounds: []world.Sound{sound.Explosion{}, sound.Thunder{}}, ExistenceDuration: time.Second}

// lightningState holds the state of a lightning entity. state is Java's
// LightningBolt.life and lifetime its remaining flashes.
type lightningState struct {
	Damage             float64
	EntityFireDuration time.Duration
	BlockFire          bool
	state, lifetime    int
	visualOnly         bool
}

// tick ports LightningBolt.tick: the strike lights fires, powers a struck
// lightning rod, cleans oxidised copper and emits lightning_strike; then the
// bolt flashes a few times, damaging nearby entities while visible.
func (s *lightningState) tick(e *Ent, tx *world.Tx) {
	pos := e.Position()
	if s.state == 2 {
		if d := tx.World().Difficulty(); s.BlockFire && !s.visualOnly && (d == world.DifficultyNormal || d == world.DifficultyHard) {
			s.spawnFire(tx, cube.PosFromVec3(pos), 4)
		}
		strike := cube.PosFromVec3(pos.Sub(mgl64.Vec3{0, 1e-6}))
		if rod, ok := tx.Block(strike).(block.LightningRod); ok {
			rod.Strike(strike, tx)
		}
		clearCopperOnLightningStrike(tx, strike)
		tx.EmitGameEvent(world.GameEventLightningStrike, pos, world.GameEventContext{Source: e})
	}
	if s.state--; s.state < 0 {
		if s.lifetime == 0 {
			_ = e.CloseIn(tx)
			return
		} else if s.state < -rand.IntN(10) {
			s.lifetime--
			s.state = 1
			if s.BlockFire && !s.visualOnly {
				s.spawnFire(tx, cube.PosFromVec3(pos), 0)
			}
		}
	}
	if s.state >= 0 && !s.visualOnly {
		s.dealDamage(e, tx)
	}
}

// clearCopperOnLightningStrike ports LightningBolt.clearCopperOnLightningStrike:
// struck unwaxed copper loses all oxidation, and 3-5 random walks of up to
// eight steps through neighbouring copper each remove one oxidation level.
func clearCopperOnLightningStrike(tx *world.Tx, struck cube.Pos) {
	copper, ok := tx.Block(struck).(block.Oxidisable)
	if !ok {
		return
	}
	if copper.CanOxidate() {
		tx.SetBlock(struck, copper.WithOxidationLevel(block.UnoxidisedOxidation()), nil)
	}
	for range rand.IntN(3) + 3 {
		walk := struck
		for range rand.IntN(8) + 1 {
			next, ok := cleanRandomCopperNear(tx, walk)
			if !ok {
				break
			}
			walk = next
		}
	}
}

// cleanRandomCopperNear ports randomStepCleaningCopper: of ten random
// positions within one block, the first unwaxed copper block loses one
// oxidation level and becomes the next step.
func cleanRandomCopperNear(tx *world.Tx, pos cube.Pos) (cube.Pos, bool) {
	for range 10 {
		candidate := pos.Add(cube.Pos{rand.IntN(3) - 1, rand.IntN(3) - 1, rand.IntN(3) - 1})
		copper, ok := tx.Block(candidate).(block.Oxidisable)
		if !ok || !copper.CanOxidate() {
			continue
		}
		if previous, ok := copper.OxidationLevel().Decrease(); ok {
			tx.SetBlock(candidate, copper.WithOxidationLevel(previous), nil)
		}
		tx.AddParticle(candidate.Vec3Centre(), particle.ElectricSpark{Data: -1})
		return candidate, true
	}
	return cube.Pos{}, false
}

// dealDamage deals damage to all entities around the lightning and sets them
// on fire.
func (s *lightningState) dealDamage(e *Ent, tx *world.Tx) {
	pos := e.Position()
	bb := e.H().Type().BBox(e).GrowVec3(mgl64.Vec3{3, 6, 3}).Translate(pos.Add(mgl64.Vec3{0, 3}))
	for target := range tx.EntitiesWithin(bb) {
		// Only damage entities that weren't already dead.
		if l, ok := target.(Living); ok && l.Health() > 0 {
			if s.Damage > 0 {
				l.Hurt(s.Damage, LightningDamageSource{BoltUUID: e.H().UUID().String()})
			}
			if f, ok := target.(Flammable); ok && f.OnFireDuration() < s.EntityFireDuration {
				f.SetOnFire(s.EntityFireDuration)
			}
		}
	}
}

// spawnFire ports LightningBolt.spawnFire: fire lights at the strike and at
// additional random positions next to it, only in air where fire survives and
// fire may spread.
func (s *lightningState) spawnFire(tx *world.Tx, pos cube.Pos, additional int) {
	if !tx.CanSpreadFireAround(pos) {
		return
	}
	s.lightFire(tx, pos)
	for range additional {
		s.lightFire(tx, pos.Add(cube.Pos{rand.IntN(3) - 1, rand.IntN(3) - 1, rand.IntN(3) - 1}))
	}
}

func (s *lightningState) lightFire(tx *world.Tx, pos cube.Pos) {
	if _, air := tx.Block(pos).(block.Air); air {
		s.fire().Start(tx, pos)
	}
}

// fire returns a fire block.
func (s *lightningState) fire() interface {
	Start(tx *world.Tx, pos cube.Pos)
} {
	return fire().(interface {
		Start(tx *world.Tx, pos cube.Pos)
	})
}

// fire returns a fire block.
func fire() world.Block {
	f, ok := world.BlockByName("minecraft:fire", map[string]any{"age": int32(0)})
	if !ok {
		panic("could not find fire block")
	}
	return f
}

// LightningType is a world.EntityType implementation for Lightning.
var LightningType lightningType

type lightningType struct{}

func (t lightningType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}
func (t lightningType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = lightningConf.New()
}
func (t lightningType) EncodeNBT(*world.EntityData) map[string]any { return nil }
func (lightningType) EncodeEntity() string                         { return "minecraft:lightning_bolt" }
func (lightningType) BBox(world.Entity) cube.BBox                  { return cube.BBox{} }
