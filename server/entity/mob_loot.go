package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// dropAllDeathLoot ports the server side of LivingEntity.die for a mob killed
// by src: the entity_die game event, which the nearest sculk catalyst answers
// by absorbing the mob's experience, the loot table rolled with the dying
// entity, its killer and the killer's weapon, the experience orbs and the
// Wither's wither rose. It also awards the killing player's totalKillCount.
func dropAllDeathLoot(tx *world.Tx, e world.Entity, src world.DamageSource) {
	killer, player := deathCredit(tx, src)
	if player != nil && killer == player {
		if named, ok := player.(interface{ Name() string }); ok {
			tx.World().Scoreboard().AddCriteria(scoreboard.TotalKillCount, named.Name(), 1)
		}
	}
	id := loot.JavaEntityID(e)
	monster := javaMonsters[id]
	baby := mobIsBaby(e)
	xp := &world.DeathExperience{Amount: mobBaseExperience(e, id, baby), Eligible: monster || id == "minecraft:hoglin" || id != "minecraft:tadpole" && !baby}
	tx.EmitGameEvent(world.GameEventEntityDie, e.Position(), world.GameEventContext{Source: e, Experience: xp})

	mobDrops := tx.World().GameRuleBool("mob_drops")
	if mobDrops && (monster || !baby) {
		table := "minecraft:entities/" + id[len("minecraft:"):]
		sequence, err := loot.RandomSequence(table)
		if err != nil {
			panic(err)
		}
		ctx := mobLootContext(tx, e, src)
		ctx.Random = tx.LootRandomSequence(sequence, 0)
		drops, err := loot.Generate(table, 0, ctx)
		if err != nil {
			panic(err)
		}
		for _, stack := range drops {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position()}, stack))
		}
		if custom, ok := e.(interface {
			dropCustomDeathLoot(killedByPlayer bool, looting int)
		}); ok {
			looting := 0
			if player != nil && killer == player {
				looting = ctx.Looting
			}
			custom.dropCustomDeathLoot(player != nil, looting)
		}
	}
	// LivingEntity.dropEquipment: inventories and saddles drop regardless of
	// the mob_drops game rule.
	if equipped, ok := e.(interface{ dropEquipment() }); ok {
		equipped.dropEquipment()
	}
	if !xp.Consumed && player != nil && xp.Eligible && mobDrops && xp.Amount > 0 {
		for _, orb := range NewExperienceOrbs(e.Position(), xp.Amount) {
			tx.AddEntity(orb)
		}
	}
	DropWitherRose(tx, e.Position(), src)
}

// DropWitherRose ports LivingEntity.createWitherRose for an entity that died
// at pos: if the Wither killed it, a wither rose is planted there while mob
// griefing is enabled and the rose can grow, and dropped as an item otherwise.
func DropWitherRose(tx *world.Tx, pos mgl64.Vec3, src world.DamageSource) {
	killer := mobKiller(src)
	if killer == nil || killer.H().Type() != WitherType {
		return
	}
	if tx.World().GameRuleBool("mob_griefing") && block.PlantWitherRose(tx, cube.PosFromVec3(pos)) {
		return
	}
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(block.Flower{Type: block.WitherRose()}, 1)))
}

// deathCredit returns the entity credited with a kill (Java
// DamageSource.getEntity) and the player credited with it: the killer itself
// or the owner of a tamed wolf, as LivingEntity.lastHurtByPlayer.
func deathCredit(tx *world.Tx, src world.DamageSource) (killer, player world.Entity) {
	killer = mobKiller(src)
	if explosion, ok := src.(ExplosionDamageSource); ok {
		killer = world.ExplosionCausingEntity(explosion.Source, tx)
	}
	if killer == nil {
		return nil, nil
	}
	if killer.H().Type().EncodeEntity() == "minecraft:player" {
		return killer, killer
	}
	if wolf, ok := killer.(*Wolf); ok && wolf.Tamed() {
		if owner, err := uuid.Parse(wolf.behaviour().ownerUUID); err == nil {
			if p, ok := tx.EntityByUUID(owner); ok && p.H().Type().EncodeEntity() == "minecraft:player" {
				return killer, p
			}
		}
	}
	return killer, nil
}

// mobIsBaby reports whether e or its behaviour is a baby.
func mobIsBaby(e world.Entity) bool {
	if b, ok := e.(interface{ Baby() bool }); ok {
		return b.Baby()
	}
	if holder, ok := e.(interface{ Behaviour() Behaviour }); ok {
		if b, ok := holder.Behaviour().(interface{ Baby() bool }); ok {
			return b.Baby()
		}
	}
	return false
}

// javaMonsters are the entities whose Java class extends Monster: their
// babies still drop loot and experience.
var javaMonsters = map[string]bool{
	"minecraft:blaze": true, "minecraft:bogged": true, "minecraft:breeze": true, "minecraft:cave_spider": true,
	"minecraft:creaking": true, "minecraft:creeper": true, "minecraft:drowned": true, "minecraft:elder_guardian": true,
	"minecraft:enderman": true, "minecraft:endermite": true, "minecraft:evoker": true, "minecraft:giant": true,
	"minecraft:guardian": true, "minecraft:husk": true, "minecraft:illusioner": true, "minecraft:parched": true,
	"minecraft:piglin": true, "minecraft:piglin_brute": true, "minecraft:pillager": true, "minecraft:ravager": true,
	"minecraft:silverfish": true, "minecraft:skeleton": true, "minecraft:spider": true, "minecraft:stray": true,
	"minecraft:vex": true, "minecraft:vindicator": true, "minecraft:warden": true, "minecraft:witch": true,
	"minecraft:wither": true, "minecraft:wither_skeleton": true, "minecraft:zoglin": true, "minecraft:zombie": true,
	"minecraft:zombie_villager": true, "minecraft:zombified_piglin": true,
}

// mobXPReward is the Java Mob.xpReward of mobs using
// Mob.getBaseExperienceReward, which adds 1-3 per worn droppable item.
var mobXPReward = map[string]int{
	"minecraft:zombie": 5, "minecraft:husk": 5, "minecraft:drowned": 5, "minecraft:zombie_villager": 5,
	"minecraft:zombified_piglin": 5, "minecraft:skeleton": 5, "minecraft:stray": 5, "minecraft:wither_skeleton": 5,
	"minecraft:bogged": 5, "minecraft:parched": 5, "minecraft:spider": 5, "minecraft:cave_spider": 5,
	"minecraft:creeper": 5, "minecraft:enderman": 5, "minecraft:silverfish": 5, "minecraft:witch": 5,
	"minecraft:pillager": 5, "minecraft:vindicator": 5, "minecraft:illusioner": 5, "minecraft:giant": 5,
	"minecraft:shulker": 5, "minecraft:phantom": 5, "minecraft:warden": 5, "minecraft:ghast": 5,
	"minecraft:zoglin": 5, "minecraft:endermite": 3, "minecraft:vex": 3, "minecraft:blaze": 10,
	"minecraft:guardian": 10, "minecraft:elder_guardian": 10, "minecraft:evoker": 10, "minecraft:breeze": 10,
	"minecraft:ravager": 20, "minecraft:piglin_brute": 20, "minecraft:wither": 50,
}

// javaAnimals are Animal, WaterAnimal and AgeableWaterCreature subclasses,
// which reward 1-3 experience.
var javaAnimals = map[string]bool{
	"minecraft:cow": true, "minecraft:mooshroom": true, "minecraft:pig": true, "minecraft:sheep": true,
	"minecraft:chicken": true, "minecraft:rabbit": true, "minecraft:fox": true, "minecraft:wolf": true,
	"minecraft:cat": true, "minecraft:ocelot": true, "minecraft:parrot": true, "minecraft:panda": true,
	"minecraft:polar_bear": true, "minecraft:turtle": true, "minecraft:bee": true, "minecraft:goat": true,
	"minecraft:frog": true, "minecraft:camel": true, "minecraft:camel_husk": true, "minecraft:sniffer": true,
	"minecraft:armadillo": true, "minecraft:strider": true, "minecraft:axolotl": true, "minecraft:horse": true,
	"minecraft:donkey": true, "minecraft:mule": true, "minecraft:llama": true, "minecraft:trader_llama": true,
	"minecraft:skeleton_horse": true, "minecraft:zombie_horse": true, "minecraft:happy_ghast": true,
	"minecraft:nautilus": true, "minecraft:zombie_nautilus": true, "minecraft:cod": true, "minecraft:salmon": true,
	"minecraft:pufferfish": true, "minecraft:tropical_fish": true, "minecraft:squid": true,
	"minecraft:glow_squid": true, "minecraft:dolphin": true, "minecraft:tadpole": true,
}

// zombieClass are the subclasses of Java's Zombie, whose babies reward 2.5
// times the experience.
var zombieClass = map[string]bool{
	"minecraft:zombie": true, "minecraft:husk": true, "minecraft:drowned": true,
	"minecraft:zombie_villager": true, "minecraft:zombified_piglin": true,
}

// mobBaseExperience ports getBaseExperienceReward of the mob's Java class.
func mobBaseExperience(e world.Entity, id string, baby bool) int {
	switch id {
	case "minecraft:piglin":
		return 5
	case "minecraft:hoglin":
		if baby {
			return 3
		}
		return 5
	case "minecraft:slime", "minecraft:magma_cube":
		if c, ok := e.(interface{ CubeSize() int }); ok {
			return c.CubeSize()
		}
		return 0
	case "minecraft:sulfur_cube":
		if baby {
			return 0
		}
		return 1 + rand.IntN(2)
	case "minecraft:chicken":
		if c, ok := e.(*Chicken); ok && c.behaviour().jockey {
			return 10
		}
	}
	if reward, ok := mobXPReward[id]; ok {
		if baby && zombieClass[id] {
			reward = int(float64(reward) * 2.5)
		}
		if equipped, ok := e.(interface{ experienceEquipment() int }); ok {
			for range equipped.experienceEquipment() {
				reward += 1 + rand.IntN(3)
			}
		}
		return reward
	}
	if javaAnimals[id] {
		return 1 + rand.IntN(3)
	}
	return 0
}

// mobLootContext builds the LootContextParamSets.ENTITY parameters.
func mobLootContext(tx *world.Tx, e world.Entity, src world.DamageSource) loot.Context {
	ctx := loot.Context{Entity: loot.JavaEntityID(e), Origin: e.Position(), Tx: tx, ThisEntity: e, Damage: src, DamageTags: damageTags(src)}
	ctx.SourceEntity, ctx.Player = deathCredit(tx, src)
	ctx.PlayerKilled = ctx.Player != nil
	ctx.DirectEntity = directDamageEntity(src)
	if user, ok := ctx.SourceEntity.(interface {
		HeldItems() (item.Stack, item.Stack)
	}); ok {
		tool, _ := user.HeldItems()
		ctx.ToolStack = tool
		if !tool.Empty() {
			ctx.Tool = tool.Item()
		}
		for _, ench := range tool.Enchantments() {
			if ench.Type().Name() == "Looting" {
				ctx.Looting = ench.Level()
			}
		}
	}
	return ctx
}

// directDamageEntity returns the entity that directly dealt the damage: the
// projectile of projectile damage, otherwise the attacker.
func directDamageEntity(src world.DamageSource) world.Entity {
	switch s := src.(type) {
	case ProjectileDamageSource:
		return s.Projectile
	case AttackDamageSource:
		return s.Attacker
	case SpearDamageSource:
		return s.Attacker
	case MaceSmashDamageSource:
		return s.Attacker
	}
	return nil
}

// damageTags returns the damage type tags loot predicates test.
func damageTags(src world.DamageSource) map[string]bool {
	if src == nil {
		return nil
	}
	tags := map[string]bool{
		"minecraft:is_fire":        src.Fire(),
		"minecraft:bypasses_armor": !src.ReducedByArmour(),
	}
	switch src.(type) {
	case ProjectileDamageSource:
		tags["minecraft:is_projectile"] = true
	case LightningDamageSource:
		tags["minecraft:is_lightning"] = true
	case ExplosionDamageSource:
		tags["minecraft:is_explosion"] = true
	case FallDamageSource:
		tags["minecraft:is_fall"] = true
	case DrowningDamageSource:
		tags["minecraft:is_drowning"] = true
	}
	return tags
}
