package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// immuneEffects ports the tags checked by LivingEntity.canBeAffected:
// #immune_to_infested and #immune_to_oozing.
var immuneEffects = map[string]effect.Type{
	"minecraft:silverfish": effect.Infested,
	"minecraft:slime":      effect.Oozing,
}

// effectImmune reports whether e can never be affected by effects of type t.
func effectImmune(e world.Entity, t effect.Type) bool {
	if e == nil || e.H() == nil {
		return false
	}
	return immuneEffects[e.H().Type().EncodeEntity()] == t
}

// TriggerDeathEffects ports MobEffect.onMobRemoved for an entity killed at
// pos with the effects passed: Wind Charged bursts into wind, Weaving spreads
// cobwebs and Oozing releases slimes.
func TriggerDeathEffects(tx *world.Tx, e world.Entity, pos mgl64.Vec3, effects []effect.Effect) {
	for _, eff := range effects {
		switch eff.Type() {
		case effect.WindCharged:
			burstWindOnDeath(tx, e, pos)
		case effect.Weaving:
			if e.H().Type().EncodeEntity() == "minecraft:player" || tx.World().GameRuleBool("mob_griefing") {
				spreadCobwebs(tx, cube.PosFromVec3(pos), 2+rand.IntN(2))
			}
		case effect.Oozing:
			releaseOozingSlimes(tx, e, pos)
		}
	}
}

// TriggerHurtEffects ports MobEffect.onMobHurt for an entity that was hurt
// with the effects passed: Infested has a 10% chance to release one or two
// silverfish.
func TriggerHurtEffects(tx *world.Tx, e world.Entity, effects []effect.Effect) {
	for _, eff := range effects {
		if eff.Type() == effect.Infested && rand.Float32() <= 0.1 {
			for range 1 + rand.IntN(2) {
				releaseSilverfish(tx, e)
			}
		}
	}
}

// onMobHurt runs TriggerHurtEffects for a mob that took damage.
func onMobHurt(tx *world.Tx, e world.Entity) {
	if holder, ok := e.(interface{ Effects() []effect.Effect }); ok {
		TriggerHurtEffects(tx, e, holder.Effects())
	}
}

// onMobKilled runs TriggerDeathEffects for a mob killed where it stands.
func onMobKilled(tx *world.Tx, e world.Entity) {
	if holder, ok := e.(interface{ Effects() []effect.Effect }); ok {
		TriggerDeathEffects(tx, e, e.Position(), holder.Effects())
	}
}

// burstWindOnDeath ports WindChargedMobEffect.onMobRemoved: a gust of
// strength 3 to 5 from the middle of the entity, with the sound and particles
// of a breeze wind charge. It pushes items only if mobs may grief, as the
// explosion of a mob with the TRIGGER interaction.
func burstWindOnDeath(tx *world.Tx, e world.Entity, pos mgl64.Vec3) {
	origin := pos.Add(mgl64.Vec3{0, float64(float32(e.H().Type().BBox(e).Height()) / 2)})
	tx.AddParticle(origin, particle.WindBurst{})
	tx.PlaySound(origin, sound.BreezeWindChargeBurst{})
	windGust(tx, e.H(), origin, 3+rand.Float32()*2, 1, !mobGriefingDisabled(tx))
}

// spreadCobwebs ports WeavingMobEffect.spawnCobwebsRandomlyAround: of 15
// random positions in the 3x3x3 cube around pos, up to count replaceable ones
// on a sturdy floor become cobwebs.
func spreadCobwebs(tx *world.Tx, pos cube.Pos, count int) {
	chosen := make([]cube.Pos, 0, count)
	for range 15 {
		p := pos.Add(cube.Pos{rand.IntN(3) - 1, rand.IntN(3) - 1, rand.IntN(3) - 1})
		if containsPos(chosen, p) {
			continue
		}
		r, ok := tx.Block(p).(block.Replaceable)
		below := p.Side(cube.FaceDown)
		if !ok || !r.ReplaceableBy(block.Cobweb{}) || !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
			continue
		}
		if chosen = append(chosen, p); len(chosen) >= count {
			break
		}
	}
	for _, p := range chosen {
		tx.SetBlock(p, block.Cobweb{}, nil)
	}
}

func containsPos(positions []cube.Pos, pos cube.Pos) bool {
	for _, p := range positions {
		if p == pos {
			return true
		}
	}
	return false
}

// releaseOozingSlimes ports OozingMobEffect: two medium slimes appear half a
// block above pos, fewer when slimes within two blocks of the entity already
// reach the max_entity_cramming game rule.
func releaseOozingSlimes(tx *world.Tx, e world.Entity, pos mgl64.Vec3) {
	count := 2
	if cramming := tx.World().GameRuleInt("max_entity_cramming"); cramming >= 1 {
		nearby := 0
		box := e.H().Type().BBox(e).Translate(pos).Grow(2)
		for other := range tx.EntitiesWithin(box.Grow(2)) {
			if nearby >= cramming {
				break
			}
			if other.H() != e.H() && other.H().Type() == SlimeType && other.H().Type().BBox(other).Translate(other.Position()).IntersectsWith(box) {
				nearby++
			}
		}
		count = min(max(0, cramming-nearby), count)
	}
	for range count {
		opts := world.EntitySpawnOpts{Position: pos.Add(mgl64.Vec3{0, 0.5}), Rotation: cube.Rotation{rand.Float64() * 360}}
		tx.AddEntity(opts.New(SlimeType, speciesConfig{kind: slimeSpecies, size: 2}))
	}
}

// releaseSilverfish ports InfestedMobEffect.spawnSilverfish: a silverfish
// leaves the middle of the entity in the direction it looks, turned by up to
// a quarter turn either way.
func releaseSilverfish(tx *world.Tx, e world.Entity) {
	look := e.Rotation().Vec3().Mul(0.3)
	look[1] *= 1.5
	angle := rand.Float64()*math.Pi - math.Pi/2
	sin, cos := math.Sincos(angle)
	vel := mgl64.Vec3{look[0]*cos + look[2]*sin, look[1], -look[0]*sin + look[2]*cos}
	pos := e.Position().Add(mgl64.Vec3{0, e.H().Type().BBox(e).Height() / 2})
	tx.AddEntity(NewSilverfish(world.EntitySpawnOpts{Position: pos, Velocity: vel, Rotation: cube.Rotation{rand.Float64() * 360}}))
	tx.PlaySound(pos, sound.NativeEvent{SoundType: "hurt", EntityType: "minecraft:silverfish", ExtraData: -1})
}
