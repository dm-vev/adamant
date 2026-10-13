package block

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

// Source Player.freeAt checks suffocation, not whether a collision shape exists.
func bedBlockSuffocates(pos cube.Pos, tx *world.Tx) bool {
	id, err := tx.JavaBlockStateID(pos)
	if err != nil {
		panic(err)
	}
	name, ok := javadata.BlockStateName(id)
	if !ok {
		panic("bed obstruction has no Source state")
	}
	name = strings.TrimPrefix(name, "minecraft:")
	switch name {
	case "farmland", "dirt_path", "soul_sand", "mud":
		return true
	case "glass", "tinted_glass", "moving_piston", "mangrove_roots":
		return false
	case "piston", "sticky_piston":
		extended, ok := javadata.BlockStateBool(id, "extended")
		if !ok {
			panic("piston has no Source extended property")
		}
		return !extended
	}
	if strings.HasSuffix(name, "_leaves") || strings.HasSuffix(name, "_stained_glass") || strings.HasSuffix(name, "copper_grate") {
		return false
	}
	if name == "shulker_box" || strings.HasSuffix(name, "_shulker_box") {
		box, ok := tx.Block(pos).(ShulkerBox)
		return !ok || box.animationStatus == nil || box.animationStatus.Load() == StateClosed
	}
	causes, err := javadata.BlockTagContains("minecraft:causes_suffocation", name)
	if err != nil {
		panic(err)
	}
	if !causes {
		return false
	}
	shape, err := javadata.BlockGenerationShapeID(id)
	if err != nil {
		panic(err)
	}
	return shape.CollisionFull
}

func bedRestUnsafe(pos cube.Pos, tx *world.Tx, sleeper world.Sleeper) bool {
	if mode, ok := sleeper.(interface{ GameMode() world.GameMode }); ok && mode.GameMode() == world.GameModeCreative {
		return false
	}
	box := cube.Box(-8, -5, -8, 8, 5, 8).Translate(pos.Vec3Middle())
	for entity := range tx.EntitiesWithin(box) {
		if monster, ok := entity.(interface{ IsPreventingPlayerRest(world.Entity) bool }); ok && monster.IsPreventingPlayerRest(sleeper) {
			return true
		}
	}
	return false
}
