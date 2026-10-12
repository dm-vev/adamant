package entity

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

//go:embed template_spawn_registry.json
var templateSpawnRegistryJSON []byte

// The lists retain actual Java 26.3 registry encounter order. Choosing from a
// singleton still consumes nextInt(1), as PriorityProvider.pick does in Source.
type templateSpawnRegistryData struct {
	CatVariants       []string        `json:"cat_variants"`
	CatSounds         []string        `json:"cat_sounds"`
	CowSounds         []string        `json:"cow_sounds"`
	PigSounds         []string        `json:"pig_sounds"`
	WarmBiomes        []string        `json:"spawns_warm_variant_farm_animals"`
	ColdBiomes        []string        `json:"spawns_cold_variant_farm_animals"`
	AllowedInPeaceful map[string]bool `json:"allowed_in_peaceful"`
}

var templateSpawnRegistry = func() templateSpawnRegistryData {
	var data templateSpawnRegistryData
	if err := json.Unmarshal(templateSpawnRegistryJSON, &data); err != nil {
		panic(err)
	}
	return data
}()

func preparePassiveTemplateSpawn(nbt map[string]any, levelRandom, entityRandom *java26.Random, context world.GenerationContext, biome string, catsSpawnAsBlack bool) error {
	id, _ := nbt["id"].(string)
	switch id {
	case "minecraft:villager":
		finalizeTemplateVillagerType(nbt, biome)
	case "minecraft:cat":
		// Cat calls its super chain before selecting variant and sound.
		prepareTemplateMobBase(nbt, 16, levelRandom)
		variants := templateSpawnRegistry.CatVariants
		if catsSpawnAsBlack {
			variants = variants[:1]
		} else if context.MoonBrightness < .9 {
			variants = variants[1:]
		}
		nbt["variant"] = variants[levelRandom.NextInt(int32(len(variants)))]
		nbt["sound_variant"] = templateSpawnRegistry.CatSounds[levelRandom.NextInt(int32(len(templateSpawnRegistry.CatSounds)))]
		return nil
	case "minecraft:cow", "minecraft:pig":
		nbt["variant"] = "minecraft:" + templateFarmTemperature(biome)
		levelRandom.NextInt(1)
		sounds := templateSpawnRegistry.CowSounds
		if id == "minecraft:pig" {
			sounds = templateSpawnRegistry.PigSounds
		}
		nbt["sound_variant"] = sounds[levelRandom.NextInt(int32(len(sounds)))]
	case "minecraft:sheep":
		colour := templateSheepColour(levelRandom, biome)
		nbt["Color"] = byte(colour)
	case "minecraft:horse":
		nbt["Variant"] = levelRandom.NextInt(7) | levelRandom.NextInt(5)<<8
		templateSpawnAttribute(nbt, "minecraft:max_health", 20)["base"] = float64(15 + levelRandom.NextInt(8) + levelRandom.NextInt(9))
		templateSpawnAttribute(nbt, "minecraft:movement_speed", .225)["base"] = (float64(float32(.45)) + levelRandom.NextDouble()*.3 + levelRandom.NextDouble()*.3 + levelRandom.NextDouble()*.3) * .25
		templateSpawnAttribute(nbt, "minecraft:jump_strength", .7)["base"] = float64(float32(.4)) + levelRandom.NextDouble()*.2 + levelRandom.NextDouble()*.2 + levelRandom.NextDouble()*.2
	case "minecraft:camel":
		nbt["LastPoseTick"] = max(int64(0), context.GameTime-53)
		if _, exists := nbt["Health"]; !exists {
			nbt["Health"] = float32(32)
		}
		if _, exists := nbt["Age"]; !exists {
			nbt["Age"] = int32(0)
		}
	case "minecraft:iron_golem", "minecraft:allay":
	default:
		return fmt.Errorf("unsupported passive template mob %q", id)
	}
	// A null group starts at groupSize=0: AgeableMob does not sample a baby
	// chance or replace authored Age. AbstractHorse's camel override is empty.
	prepareTemplateMobBase(nbt, 16, levelRandom)
	return nil
}

func templateFarmTemperature(biome string) string {
	if slices.Contains(templateSpawnRegistry.WarmBiomes, biome) {
		return "warm"
	}
	if slices.Contains(templateSpawnRegistry.ColdBiomes, biome) {
		return "cold"
	}
	return "temperate"
}

func templateSheepColour(random *java26.Random, biome string) int {
	var rare [4]int
	common := 0
	switch templateFarmTemperature(biome) {
	case "warm":
		rare, common = [4]int{7, 8, 0, 15}, 12
	case "cold":
		rare, common = [4]int{8, 7, 0, 12}, 15
	default:
		rare = [4]int{15, 7, 8, 12}
	}
	choice := random.NextInt(100)
	for i, weight := range [...]int32{5, 5, 5, 3} {
		choice -= weight
		if choice < 0 {
			return rare[i]
		}
	}
	if random.NextInt(500) == 499 {
		return 6
	}
	return common
}
