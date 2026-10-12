package entity

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

// templateSpawnAttribute returns an attribute in the placement-owned Source NBT.
// Authored values and modifiers survive finalizeSpawn; absent values use the
// concrete mob's Source attribute defaults.
func templateSpawnAttribute(nbt map[string]any, id string, base float64) map[string]any {
	attributes, _ := nbt["attributes"].([]any)
	for _, value := range attributes {
		if attribute, ok := value.(map[string]any); ok && attribute["id"] == id {
			return attribute
		}
	}
	attribute := spawnAttribute(id, base)
	nbt["attributes"] = append(attributes, attribute)
	return attribute
}

// prepareTemplateMobBase is Mob.finalizeSpawn. The random-spawn modifier is
// one-shot, but handedness is sampled on every finalize call.
func prepareTemplateMobBase(nbt map[string]any, followRange float64, random *java26.Random) {
	follow := templateSpawnAttribute(nbt, "minecraft:follow_range", followRange)
	modifiers, _ := follow["modifiers"].([]any)
	found := false
	for _, value := range modifiers {
		if modifier, ok := value.(map[string]any); ok && modifier["id"] == "minecraft:random_spawn_bonus" {
			found = true
			break
		}
	}
	if !found {
		spawnModifier(follow, "minecraft:random_spawn_bonus", .11485000000000001*(random.NextDouble()-random.NextDouble()), "add_multiplied_base")
	}
	nbt["LeftHanded"] = boolByte(random.NextFloat() < .05)
}

// TemplateMobSpawnContext supplies the live Source region inputs at the
// transformed entity position, not the template's integer clipping anchor.
type TemplateMobSpawnContext struct {
	Generation       world.GenerationContext
	Biome, Dimension string
	Position         [3]int32
	CatsSpawnAsBlack bool
}

// PrepareTemplateMobSpawn loads/finalizes a Mob from authored template NBT.
// A false keep result is Source EntityType.canSpawn rejecting a peaceful mob;
// that path consumes neither stream and creates no native actor.
func PrepareTemplateMobSpawn(nbt map[string]any, levelRandom, entityRandom *java26.Random, context TemplateMobSpawnContext) (vehicle map[string]any, keep bool, err error) {
	id, _ := nbt["id"].(string)
	allowed, known := templateSpawnRegistry.AllowedInPeaceful[id]
	if !known {
		return nil, false, fmt.Errorf("unsupported Source template entity %q", id)
	}
	if context.Generation.Difficulty.Base == world.DifficultyPeaceful && !allowed {
		return nil, false, nil
	}
	if levelRandom == nil || entityRandom == nil {
		return nil, false, fmt.Errorf("template mob %s requires independent region and entity random streams", id)
	}
	switch id {
	case "minecraft:piglin", "minecraft:piglin_brute", "minecraft:hoglin":
		err = prepareNetherTemplateSpawn(nbt, levelRandom, entityRandom, context.Generation, context.Position, context.Dimension)
	case "minecraft:zombie_villager":
		vehicle, err = prepareZombieVillagerTemplateSpawn(nbt, levelRandom, entityRandom, context.Generation, context.Biome)
	case "minecraft:villager", "minecraft:cat", "minecraft:cow", "minecraft:horse", "minecraft:iron_golem", "minecraft:pig", "minecraft:sheep", "minecraft:camel", "minecraft:allay":
		err = preparePassiveTemplateSpawn(nbt, levelRandom, entityRandom, context.Generation, context.Biome, context.CatsSpawnAsBlack)
	default:
		return nil, false, fmt.Errorf("Source template entity %q is not a Mob", id)
	}
	return vehicle, err == nil, err
}
