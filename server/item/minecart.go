package item

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Minecart is a basic minecart item.
type Minecart struct{}

// UseOnBlock places a minecart on a rail.
func (Minecart) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().Minecart
	return spawnMinecartOnRail(pos, tx, ctx, create, "minecart")
}

// EncodeItem returns the minecart item ID.
func (Minecart) EncodeItem() (string, int16) { return "minecraft:minecart", 0 }

// MaxCount returns the max stack size.
func (Minecart) MaxCount() int { return 1 }

// MinecartChest is a chest minecart item.
type MinecartChest struct{}

// UseOnBlock places a chest minecart on a rail.
func (MinecartChest) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().MinecartChest
	return spawnMinecartOnRail(pos, tx, ctx, create, "chest_minecart")
}

// EncodeItem returns the chest minecart item ID.
func (MinecartChest) EncodeItem() (string, int16) { return "minecraft:chest_minecart", 0 }

// MaxCount returns the max stack size.
func (MinecartChest) MaxCount() int { return 1 }

// MinecartHopper is a hopper minecart item.
type MinecartHopper struct{}

// UseOnBlock places a hopper minecart on a rail.
func (MinecartHopper) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().MinecartHopper
	return spawnMinecartOnRail(pos, tx, ctx, create, "hopper_minecart")
}

// EncodeItem returns the hopper minecart item ID.
func (MinecartHopper) EncodeItem() (string, int16) { return "minecraft:hopper_minecart", 0 }

// MaxCount returns the max stack size.
func (MinecartHopper) MaxCount() int { return 1 }

// MinecartTNT is a TNT minecart item.
type MinecartTNT struct{}

// UseOnBlock places a TNT minecart on a rail.
func (MinecartTNT) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().MinecartTNT
	return spawnMinecartOnRail(pos, tx, ctx, create, "tnt_minecart")
}

// EncodeItem returns the TNT minecart item ID.
func (MinecartTNT) EncodeItem() (string, int16) { return "minecraft:tnt_minecart", 0 }

// MaxCount returns the max stack size.
func (MinecartTNT) MaxCount() int { return 1 }

func spawnMinecartOnRail(pos cube.Pos, tx *world.Tx, ctx *UseContext, create func(world.EntitySpawnOpts) *world.EntityHandle, name string) bool {
	ascending, ok := railInfo(tx.Block(pos))
	if !ok {
		return false
	}
	if create == nil {
		slog.Default().Info("minecart: missing entity factory", "name", name, "world", tx.World().Name())
		return false
	}
	spawn := pos.Vec3Middle()
	spawn[1] += 0.0625
	if ascending {
		spawn[1] += 0.5
	}
	opts := world.EntitySpawnOpts{Position: spawn}
	tx.AddEntity(create(opts))
	ctx.SubtractFromCount(1)
	return true
}

func railInfo(b world.Block) (ascending bool, ok bool) {
	name, props := b.EncodeBlock()
	switch name {
	case "minecraft:rail", "minecraft:golden_rail", "minecraft:detector_rail", "minecraft:activator_rail":
		// Valid rail types.
	default:
		return false, false
	}
	dir, ok := props["rail_direction"].(int32)
	if !ok {
		return false, true
	}
	return dir >= 2 && dir <= 5, true
}
