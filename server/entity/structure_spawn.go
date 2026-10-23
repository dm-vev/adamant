package entity

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

// PrepareStructureMobSpawn applies the mansion and monument Mob.finalizeSpawn
// chains before their marker is replaced. Region and entity streams have separate
// owners; none of these Source overrides draw from Entity.random.
func PrepareStructureMobSpawn(id string, levelRandom, entityRandom *vanilla.Random, context world.GenerationContext) (map[string]any, error) {
	if levelRandom == nil || entityRandom == nil {
		return nil, fmt.Errorf("structure mob spawn requires level and entity random streams")
	}
	var health float32
	var followRange float64
	switch id {
	case "minecraft:evoker", "minecraft:vindicator":
		health, followRange = 24, 12
	case "minecraft:allay":
		health, followRange = 20, 16
	case "minecraft:elder_guardian":
		health, followRange = 80, 16
	default:
		return nil, fmt.Errorf("unsupported prepared structure mob %q", id)
	}
	follow := spawnFollowRange(levelRandom, followRange)
	prepared := map[string]any{
		"id": id, "Health": health, "LeftHanded": boolByte(levelRandom.NextFloat() < .05),
		"attributes": []any{follow},
	}
	if id == "minecraft:evoker" || id == "minecraft:vindicator" {
		// Raider.finalizeSpawn permits STRUCTURE illagers to join a later raid.
		prepared["CanJoinRaid"] = byte(1)
	}
	if id == "minecraft:vindicator" {
		main := item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)
		difficulty := context.Difficulty.SpecialMultiplier()
		if levelRandom.NextFloat() < float32(.25)*difficulty {
			var err error
			main, err = loot.EnchantMobSpawnEquipment(main, difficulty, levelRandom)
			if err != nil {
				return nil, err
			}
		}
		encoded, err := item.StackToStructureNBT(main)
		if err != nil {
			return nil, err
		}
		prepared["equipment"] = map[string]any{"mainhand": encoded}
	}
	return prepared, nil
}
