package javastructure

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// SinglePoolElement enables finalizeEntities; other template pieces do not.
// Block processors and template placement finish before any Mob draws from
// WorldGenRegion.random. Entity.random is an independent, fresh Legacy stream.
func finalizeJigsawTemplateEntities(placement *Placement, env ProcessorEnvironment) error {
	kept := placement.Entities[:0]
	for i := range placement.Entities {
		spawn := placement.Entities[i]
		id, _ := spawn.NBT["id"].(string)
		switch id {
		case "minecraft:armor_stand", "minecraft:cushion":
			spawn.SpawnReason = "structure"
			kept = append(kept, spawn)
			continue
		case "minecraft:piglin_brute", "minecraft:hoglin", "minecraft:zombie_villager":
			if env.GenerationContext.Difficulty.Base == world.DifficultyPeaceful { continue }
		}
		if env.JavaBiomeAt == nil { return fmt.Errorf("Java template Mob %s has no live biome source", id) }
		pos := [3]int32{int32(math.Floor(spawn.Pos[0])), int32(math.Floor(spawn.Pos[1])), int32(math.Floor(spawn.Pos[2]))}
		if id == "minecraft:piglin_brute" && env.SourceDimension == "" { return fmt.Errorf("Java Brute template has no dimension for HOME") }
		blackCats := false
		if id == "minecraft:cat" && env.CatsSpawnAsBlackAt != nil {
			blackCats = env.CatsSpawnAsBlackAt(pos[0], pos[1], pos[2])
		}
		spawn.ID = uuid.New()
		own := java26.NewLegacyRandom(int64(binary.LittleEndian.Uint64(spawn.ID[8:])))
		vehicle, keep, err := entity.PrepareTemplateMobSpawn(spawn.NBT, env.LevelRandom, &own, entity.TemplateMobSpawnContext{
			Generation: env.GenerationContext, Biome: env.JavaBiomeAt(pos[0], pos[1], pos[2]), Dimension: env.SourceDimension,
			Position: pos, CatsSpawnAsBlack: blackCats,
		})
		if err != nil { return err }
		if !keep { continue }
		spawn.SpawnReason, spawn.SpawnFinalized, spawn.EntityRandom = "structure", true, &own
		if vehicle != nil {
			spawn = Entity{ID: uuid.New(), Pos: spawn.Pos, BlockPos: spawn.BlockPos, NBT: vehicle, Seed: spawn.Seed,
				SpawnReason: "structure", SpawnFinalized: true, Passengers: []Entity{spawn}}
		}
		kept = append(kept, spawn)
	}
	placement.Entities = kept
	return nil
}
