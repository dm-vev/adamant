package item

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndCrystal is an item that may be placed to spawn an end crystal entity.
type EndCrystal struct{}

// UseOnBlock places the end crystal entity above the clicked block if possible.
func (EndCrystal) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	logger := slog.Default()
	if face == cube.FaceDown {
		logger.Info("end_crystal: reject - clicked bottom face", "world", tx.World().Name(), "pos", pos)
		return false
	}

	basePos := pos
	base := tx.Block(basePos)
	name, _ := base.EncodeBlock()
	showBase := true
	switch name {
	case "minecraft:bedrock":
		showBase = false
	case "minecraft:obsidian":
		// Allowed without changes.
	default:
		logger.Info("end_crystal: reject - invalid base block", "world", tx.World().Name(), "pos", pos, "base", name)
		return false
	}

	above := basePos.Side(cube.FaceUp)
	aboveName, _ := tx.Block(above).EncodeBlock()
	if aboveName != "minecraft:air" {
		// Allow replacing fire above the base, matching vanilla end pillar behaviour.
		if aboveName == "minecraft:fire" || aboveName == "minecraft:soul_fire" {
			logger.Info("end_crystal: clearing fire above base", "world", tx.World().Name(), "pos", above, "block", aboveName)
			tx.SetBlock(above, nil, nil)
		} else {
			logger.Info("end_crystal: reject - blocked above base", "world", tx.World().Name(), "pos", above, "block", aboveName)
			return false
		}
	}
	if _, liquid := tx.Liquid(above); liquid {
		logger.Info("end_crystal: reject - liquid above base", "world", tx.World().Name(), "pos", above)
		return false
	}

	create := tx.World().EntityRegistry().Config().EndCrystal
	if create == nil {
		logger.Info("end_crystal: reject - entity factory missing", "world", tx.World().Name())
		return false
	}
	spawn := basePos.Vec3Centre()
	spawn[1] += 0.5

	// Prevent duplicate crystals at the same spot.
	bb := cube.Box(spawn[0]-0.5, float64(basePos[1]), spawn[2]-0.5, spawn[0]+0.5, float64(basePos[1])+2.0, spawn[2]+0.5)
	for e := range tx.EntitiesWithin(bb) {
		if e.H().Type().EncodeEntity() == "minecraft:ender_crystal" {
			logger.Info("end_crystal: reject - already present", "world", tx.World().Name(), "base", basePos)
			return false
		}
	}

	opts := world.EntitySpawnOpts{Position: spawn}
	tx.AddEntity(create(opts, showBase))
	logger.Info("end_crystal: placed", "world", tx.World().Name(), "base", basePos, "spawn", spawn, "showBase", showBase)

	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem returns the ID and meta of the end crystal item.
func (EndCrystal) EncodeItem() (string, int16) {
	return "minecraft:end_crystal", 0
}
