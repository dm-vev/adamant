package player

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"golang.org/x/text/language"
)

type deathKey string

func (k deathKey) Resolve(language.Tag) string { return "%" + string(k) }

func deathTranslation(key string, params int, fallback string) chat.Translation {
	return chat.Translate(deathKey(key), params, fallback)
}

// Death message translations from the Bedrock en_US.lang death.attack keys.
var (
	deathGeneric     = deathTranslation("death.attack.generic", 1, "%v died")
	deathPlayer      = deathTranslation("death.attack.player", 2, "%v was slain by %v")
	deathMob         = deathTranslation("death.attack.mob", 2, "%v was slain by %v")
	deathArrow       = deathTranslation("death.attack.arrow", 2, "%v was shot by %v")
	deathTrident     = deathTranslation("death.attack.trident", 2, "%v was impaled to death by %v")
	deathFireball    = deathTranslation("death.attack.fireball", 2, "%v was fireballed by %v")
	deathThrown      = deathTranslation("death.attack.thrown", 2, "%v was pummeled by %v")
	deathThorns      = deathTranslation("death.attack.thorns", 2, "%v was killed trying to hurt %v")
	deathFall        = deathTranslation("death.attack.fall", 1, "%v hit the ground too hard")
	deathInFire      = deathTranslation("death.attack.inFire", 1, "%v went up in flames")
	deathOnFire      = deathTranslation("death.attack.onFire", 1, "%v burned to death")
	deathLava        = deathTranslation("death.attack.lava", 1, "%v tried to swim in lava")
	deathMagma       = deathTranslation("death.attack.magma", 1, "%v discovered the floor was lava")
	deathInWall      = deathTranslation("death.attack.inWall", 1, "%v suffocated in a wall")
	deathDrown       = deathTranslation("death.attack.drown", 1, "%v drowned")
	deathOutOfWorld  = deathTranslation("death.attack.outOfWorld", 1, "%v fell out of the world")
	deathExplosion   = deathTranslation("death.attack.explosion", 1, "%v blew up")
	deathExplosionBy = deathTranslation("death.attack.explosion.player", 2, "%v was blown up by %v")
	deathFireworks   = deathTranslation("death.attack.fireworks", 1, "%v went off with a bang")
	deathMagic       = deathTranslation("death.attack.magic", 1, "%v was killed by magic")
	deathWither      = deathTranslation("death.attack.wither", 1, "%v withered away")
	deathLightning   = deathTranslation("death.attack.lightningBolt", 1, "%v was struck by lightning")
	deathFlyIntoWall = deathTranslation("death.attack.flyIntoWall", 1, "%v experienced kinetic energy")
	deathStarve      = deathTranslation("death.attack.starve", 1, "%v starved to death")
)

// deathMessage ports DamageSource.getLocalizedDeathMessage onto the Bedrock
// death.attack keys: attacker-based messages name the killer, others only the
// victim. pos is the victim's feet, used to tell in-fire from burning damage.
func deathMessage(victim string, pos cube.Pos, src world.DamageSource, tx *world.Tx) (chat.Translation, []any) {
	one := func(t chat.Translation) (chat.Translation, []any) { return t, []any{victim} }
	two := func(t chat.Translation, killer world.Entity) (chat.Translation, []any) {
		return t, []any{victim, entityDisplayName(killer)}
	}
	switch s := src.(type) {
	case entity.ProjectileDamageSource:
		if s.Owner == nil {
			return one(deathGeneric)
		}
		var id string
		if s.Projectile != nil {
			id = s.Projectile.H().Type().EncodeEntity()
		}
		switch {
		case strings.Contains(id, "arrow"):
			return two(deathArrow, s.Owner)
		case id == "minecraft:thrown_trident":
			return two(deathTrident, s.Owner)
		case id == "minecraft:fireball", id == "minecraft:small_fireball":
			return two(deathFireball, s.Owner)
		}
		return two(deathThrown, s.Owner)
	case entity.AttackDamageSource, entity.SpearDamageSource, entity.MaceSmashDamageSource:
		killer := entity.DamageSourceKiller(src)
		if killer == nil {
			return one(deathGeneric)
		}
		if _, ok := killer.(*Player); ok {
			return two(deathPlayer, killer)
		}
		return two(deathMob, killer)
	case enchantment.ThornsDamageSource:
		if s.Owner == nil {
			return one(deathGeneric)
		}
		return two(deathThorns, s.Owner)
	case entity.ExplosionDamageSource:
		if explosionEntityID(s.Source) == "minecraft:fireworks_rocket" {
			return one(deathFireworks)
		}
		if causing := world.ExplosionCausingEntity(s.Source, tx); causing != nil {
			return two(deathExplosionBy, causing)
		}
		return one(deathExplosion)
	case entity.FallDamageSource:
		return one(deathFall)
	case entity.GlideDamageSource:
		return one(deathFlyIntoWall)
	case entity.SuffocationDamageSource:
		return one(deathInWall)
	case entity.DrowningDamageSource:
		return one(deathDrown)
	case entity.VoidDamageSource:
		return one(deathOutOfWorld)
	case entity.LightningDamageSource:
		return one(deathLightning)
	case block.LavaDamageSource:
		return one(deathLava)
	case block.MagmaDamageSource:
		return one(deathMagma)
	case block.FireDamageSource:
		// Fire and campfire contact is IN_FIRE; damage while merely burning is ON_FIRE.
		switch tx.Block(pos).(type) {
		case block.Fire, block.Campfire:
			return one(deathInFire)
		}
		return one(deathOnFire)
	case effect.WitherDamageSource:
		return one(deathWither)
	case effect.InstantDamageSource, effect.PoisonDamageSource:
		return one(deathMagic)
	case StarvationDamageSource:
		return one(deathStarve)
	}
	return one(deathGeneric)
}

// explosionEntityID returns the type of the entity that exploded, if any.
func explosionEntityID(src world.ExplosionSource) string {
	var e world.Entity
	switch s := src.(type) {
	case world.EntityExplosionSource:
		e = s.Entity
	case *world.EntityExplosionSource:
		e = s.Entity
	}
	if e == nil {
		return ""
	}
	return e.H().Type().EncodeEntity()
}

// entityDisplayName is Entity.getDisplayName: players use their name, named
// mobs their custom name, and other entities their translated type name.
func entityDisplayName(e world.Entity) string {
	if p, ok := e.(*Player); ok {
		return p.Name()
	}
	if named, ok := e.(interface{ NameTag() string }); ok && named.NameTag() != "" {
		return named.NameTag()
	}
	id := strings.TrimPrefix(e.H().Type().EncodeEntity(), "minecraft:")
	return "%entity." + id + ".name"
}

// broadcastDeathMessage sends the death message to every player in the world
// when the show_death_messages gamerule allows it.
func (p *Player) broadcastDeathMessage(src world.DamageSource) {
	w := p.tx.World()
	if !w.GameRuleBool("show_death_messages") {
		return
	}
	t, args := deathMessage(p.Name(), cube.PosFromVec3(p.Position()), src, p.tx)
	for e := range p.tx.Players() {
		if other, ok := e.(*Player); ok {
			other.Messaget(t, args...)
		}
	}
}
