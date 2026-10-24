package entity

import (
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

// This file ports Mob.checkDespawn: when a mob far from players leaves the
// world, and what keeps it.

// mobCategories are the MobCategory of the entity types that are not MISC,
// as EntityTypes registers them.
var mobCategories = func() map[string]naturalSpawnCategory {
	m := map[string]naturalSpawnCategory{}
	for category, ids := range map[naturalSpawnCategory][]string{
		spawnCreature: {"allay", "armadillo", "bee", "camel", "cat", "chicken", "cow", "donkey", "fox", "frog", "goat", "happy_ghast", "horse",
			"llama", "mooshroom", "mule", "ocelot", "panda", "parrot", "pig", "polar_bear", "rabbit", "sheep", "skeleton_horse", "sniffer",
			"strider", "tadpole", "trader_llama", "turtle", "wandering_trader", "wolf"},
		spawnAxolotls: {"axolotl"},
		spawnAmbient:  {"bat"},
		spawnMonster: {"blaze", "bogged", "breeze", "camel_husk", "cave_spider", "creaking", "creeper", "drowned", "elder_guardian",
			"ender_dragon", "enderman", "endermite", "evoker", "ghast", "giant", "guardian", "hoglin", "husk", "illusioner", "magma_cube",
			"parched", "phantom", "piglin", "piglin_brute", "pillager", "ravager", "shulker", "silverfish", "skeleton", "slime", "spider",
			"stray", "sulfur_cube", "vex", "vindicator", "warden", "witch", "wither", "wither_skeleton", "zoglin", "zombie", "zombie_horse",
			"zombie_nautilus", "zombie_villager", "zombified_piglin"},
		spawnWaterAmbient:             {"cod", "pufferfish", "salmon", "tropical_fish"},
		spawnWaterCreature:            {"dolphin", "nautilus", "squid"},
		spawnUndergroundWaterCreature: {"glow_squid"},
	} {
		for _, id := range ids {
			m["minecraft:"+id] = category
		}
	}
	return m
}()

// notInPeaceful are the entity types EntityTypes marks notInPeaceful, which
// leave the world on peaceful.
var notInPeaceful = func() map[string]bool {
	m := map[string]bool{}
	for _, id := range []string{"blaze", "bogged", "breeze", "cave_spider", "creaking", "creeper", "drowned", "elder_guardian", "enderman",
		"endermite", "evoker", "ghast", "giant", "guardian", "hoglin", "husk", "illusioner", "magma_cube", "parched", "phantom", "piglin_brute",
		"pillager", "ravager", "silverfish", "skeleton", "slime", "spider", "stray", "vex", "vindicator", "warden", "witch", "wither",
		"wither_skeleton", "zoglin", "zombie", "zombie_villager", "zombified_piglin"} {
		m["minecraft:"+id] = true
	}
	return m
}()

// javaAnimalClasses are the mobs that extend Java's Animal, which
// Animal.removeWhenFarAway keeps however far they are.
var javaAnimalClasses = func() map[string]bool {
	m := map[string]bool{}
	for _, id := range []string{"armadillo", "axolotl", "bee", "camel", "camel_husk", "cat", "chicken", "cow", "donkey", "fox", "frog",
		"goat", "happy_ghast", "hoglin", "horse", "llama", "mooshroom", "mule", "ocelot", "panda", "parrot", "pig", "polar_bear", "rabbit",
		"sheep", "skeleton_horse", "sniffer", "strider", "trader_llama", "turtle", "wolf", "zombie_horse"} {
		m["minecraft:"+id] = true
	}
	return m
}()

// despawnDistance ports MobCategory.getDespawnDistance: a mob beyond it
// leaves at once.
func despawnDistance(id string) float64 {
	if mobCategories[id] == spawnWaterAmbient {
		return 64
	}
	return 128
}

// noDespawnDistance is MobCategory.getNoDespawnDistance: a mob within it never
// leaves of its own accord.
const noDespawnDistance = 32

// mobDespawner is implemented by the behaviours of mobs that override
// Mob.removeWhenFarAway or requiresCustomPersistence.
type mobDespawner interface {
	removeWhenFarAway(m *Mob, distSqr float64) bool
}

// mobPersister is implemented by the behaviours of mobs that override
// Mob.requiresCustomPersistence.
type mobPersister interface {
	requiresCustomPersistence(m *Mob) bool
}

// checkDespawn ports Mob.checkDespawn. It reports whether the mob left the
// world.
func (ai *MobAI) checkDespawn(m *Mob) bool {
	id := loot.JavaEntityID(m.Self)
	if m.tx.World().Difficulty() == world.DifficultyPeaceful && notInPeaceful[id] {
		discardMob(m)
		return true
	}
	persistent := ai.persistent || mobRequiresCustomPersistence(m)
	if p, ok := m.Self.(interface{ PersistenceRequired() bool }); ok && p.PersistenceRequired() {
		persistent = true
	}
	nearest := -1.0
	for p := range m.tx.Players() {
		if spectator(p) {
			continue
		}
		if d := p.Position().Sub(m.data.Pos).LenSqr(); nearest < 0 || d < nearest {
			nearest = d
		}
	}
	if nearest < 0 {
		return false
	}
	far := despawnDistance(id)
	if !persistent && nearest > far*far && mobRemoveWhenFarAway(m, id, nearest) {
		discardMob(m)
		return true
	}
	if !persistent && ai.noActionTime > 600 && m.Random().NextInt(800) == 0 && nearest > noDespawnDistance*noDespawnDistance && mobRemoveWhenFarAway(m, id, nearest) {
		discardMob(m)
		return true
	} else if nearest < noDespawnDistance*noDespawnDistance {
		ai.noActionTime = 0
	}
	return false
}

// mobRemoveWhenFarAway ports Mob.removeWhenFarAway and its overrides.
func mobRemoveWhenFarAway(m *Mob, id string, distSqr float64) bool {
	if d, ok := m.Behaviour().(mobDespawner); ok {
		return d.removeWhenFarAway(m, distSqr)
	}
	switch id {
	case "minecraft:hoglin", "minecraft:camel_husk", "minecraft:zombie_horse", "minecraft:nautilus", "minecraft:zombie_nautilus":
		return true
	case "minecraft:villager", "minecraft:wandering_trader", "minecraft:iron_golem", "minecraft:snow_golem", "minecraft:copper_golem",
		"minecraft:allay", "minecraft:warden":
		return false
	case "minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish", "minecraft:pufferfish", "minecraft:tadpole", "minecraft:axolotl":
		// AbstractFish and Axolotl: what came from a bucket or was named
		// stays.
		return !mobFromBucket(m) && m.NameTag() == ""
	case "minecraft:chicken":
		c, ok := m.Self.(*Chicken)
		return ok && c.behaviour().jockey
	case "minecraft:cat", "minecraft:ocelot":
		// Cat and Ocelot: strays that are not tame or trusting leave once
		// two minutes old.
		if t, ok := m.Self.(interface{ Tamed() bool }); ok && t.Tamed() {
			return false
		}
		if t, ok := m.Self.(interface{ Trusting() bool }); ok && t.Trusting() {
			return false
		}
		return m.AI.tickCount > 2400
	}
	return !javaAnimalClasses[id]
}

// mobRequiresCustomPersistence ports Mob.requiresCustomPersistence and its
// overrides: passengers and leashed mobs stay.
func mobRequiresCustomPersistence(m *Mob) bool {
	if m.data.Riding != nil || m.HolderLeashID() != 0 {
		return true
	}
	if p, ok := m.Behaviour().(mobPersister); ok && p.requiresCustomPersistence(m) {
		return true
	}
	switch loot.JavaEntityID(m.Self) {
	case "minecraft:elder_guardian":
		// ElderGuardian sets persistenceRequired when it is made.
		return true
	case "minecraft:cod", "minecraft:salmon", "minecraft:tropical_fish", "minecraft:pufferfish", "minecraft:tadpole", "minecraft:axolotl", "minecraft:sulfur_cube":
		return mobFromBucket(m)
	case "minecraft:nautilus", "minecraft:zombie_nautilus":
		t, ok := m.Self.(interface{ Tamed() bool })
		return ok && t.Tamed()
	case "minecraft:enderman":
		c, ok := m.Self.(interface{ CarriedBlock() (world.Block, bool) })
		if ok {
			_, carrying := c.CarriedBlock()
			return carrying
		}
	}
	return false
}

// mobFromBucket reports whether a mob came from a bucket.
func mobFromBucket(m *Mob) bool {
	b, ok := m.Self.(interface{ FromBucket() bool })
	return ok && b.FromBucket()
}

// discardMob removes a mob from the world, as Entity.discard.
func discardMob(m *Mob) {
	if c, ok := m.Self.(interface{ CloseIn(*world.Tx) error }); ok {
		_ = c.CloseIn(m.tx)
		return
	}
	m.tx.RemoveEntity(m.Self)
}

// PersistenceRequired reports whether the mob never leaves the world of its
// own accord, as Mob.isPersistenceRequired.
func (e *Ent) PersistenceRequired() bool {
	ai, _ := e.mobAI()
	return ai != nil && ai.persistent
}

// SetPersistenceRequired makes the mob stay in the world however far it is
// from players, as Mob.setPersistenceRequired.
func (e *Ent) SetPersistenceRequired(v bool) {
	if ai, _ := e.mobAI(); ai != nil {
		ai.persistent = v
	}
}

// encodePersistence writes the persistence of a mob as the Persistent flag
// Bedrock saves actors with.
func encodePersistence(data *world.EntityData) map[string]any {
	b, ok := data.Data.(mobAIBehaviour)
	if !ok {
		return nil
	}
	return map[string]any{"Persistent": boolByte(b.mobAI().persistent)}
}

// decodePersistence reads the persistence of a mob from Bedrock's Persistent
// flag or Java's PersistenceRequired.
func decodePersistence(data *world.EntityData, m map[string]any) {
	b, ok := data.Data.(mobAIBehaviour)
	if !ok {
		return
	}
	persistent := nbtBool(m, "Persistent") || nbtBool(m, "PersistenceRequired")
	if java := ReadNativeMobJavaState(m); java != nil {
		persistent = persistent || nbtBool(java, "PersistenceRequired")
	}
	if persistent {
		b.mobAI().persistent = true
	}
}

// nbtBool reads a flag Java may save as a byte or a bool.
func nbtBool(m map[string]any, key string) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case byte:
		return v != 0
	case int32:
		return v != 0
	}
	return false
}

// encodeSharedNBT writes the NBT many entity types share: their effects and
// whether they persist.
func encodeSharedNBT(data *world.EntityData) map[string]any {
	out := encodeMobEffects(data)
	if p := encodePersistence(data); p != nil {
		if out == nil {
			out = map[string]any{}
		}
		for k, v := range p {
			out[k] = v
		}
	}
	return out
}

// decodeSharedNBT reads what encodeSharedNBT writes.
func decodeSharedNBT(data *world.EntityData, m map[string]any) {
	decodeMobEffects(data, m)
	decodePersistence(data, m)
}

// removeWhenFarAway ports the overrides of the hostile mobs: zombie
// villagers that are being cured or traded stay, and so do raiders in a raid
// and patrols unless far beyond their patrol.
func (b *hostileState) removeWhenFarAway(m *Mob, distSqr float64) bool {
	switch {
	case b.kind == "minecraft:zombie_villager":
		return b.cureTicks <= 0 && b.villagerXP == 0
	case hostileRaidMob(b.kind):
		return raiderRemoveWhenFarAway(m, &b.raiderState, distSqr)
	}
	return true
}

// removeWhenFarAway ports Raider.removeWhenFarAway for a witch.
func (b *WitchBehaviour) removeWhenFarAway(m *Mob, distSqr float64) bool {
	return raiderRemoveWhenFarAway(m, &b.raiderState, distSqr)
}

// requiresCustomPersistence ports Raider.requiresCustomPersistence for a
// witch.
func (b *WitchBehaviour) requiresCustomPersistence(m *Mob) bool {
	return m.tx.RaidForRaider(m.Self) != nil
}

// raiderRemoveWhenFarAway ports Raider.removeWhenFarAway and
// PatrollingMonster's: a raider in a raid stays, and a patrol unless it is
// beyond 128 blocks.
func raiderRemoveWhenFarAway(m *Mob, r *raiderState, distSqr float64) bool {
	if m.tx.RaidForRaider(m.Self) != nil {
		return false
	}
	return !r.patrolling || distSqr > 16384
}

// requiresCustomPersistence ports Raider.requiresCustomPersistence: a
// raider in a raid stays.
func (b *hostileState) requiresCustomPersistence(m *Mob) bool {
	return hostileRaidMob(b.kind) && m.tx.RaidForRaider(m.Self) != nil
}
