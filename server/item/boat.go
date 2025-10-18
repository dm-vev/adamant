package item

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Boat represents a boat, chest boat, or raft item.
type Boat struct {
	Variant string
	Chest   bool
}

var boatVariantNames = []string{"oak", "spruce", "birch", "jungle", "acacia", "dark_oak", "mangrove", "cherry", "bamboo", "pale_oak"}

// BoatVariants returns all available boat variant identifiers.
func BoatVariants() []string {
	out := make([]string, len(boatVariantNames))
	copy(out, boatVariantNames)
	return out
}

// MaxCount ...
func (Boat) MaxCount() int { return 1 }

// EncodeItem ...
func (b Boat) EncodeItem() (name string, meta int16) {
	variant := b.ensureVariant()
	switch variant {
	case "bamboo":
		if b.Chest {
			return "minecraft:bamboo_chest_raft", 0
		}
		return "minecraft:bamboo_raft", 0
	default:
		if b.Chest {
			return fmt.Sprintf("minecraft:%s_chest_boat", variant), 0
		}
		return fmt.Sprintf("minecraft:%s_boat", variant), 0
	}
}

// UseOnBlock spawns the boat entity on the targeted block.
func (b Boat) UseOnBlock(pos cube.Pos, _ cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	spawnPos := pos.Vec3().Add(clickPos)
	spawnPos[1] += 0.1

	var rot cube.Rotation
	if r, ok := user.(interface{ Rotation() cube.Rotation }); ok {
		userRot := r.Rotation()
		rot = cube.Rotation{userRot.Yaw(), 0}
	}

	create := tx.World().EntityRegistry().Config().Boat
	tx.AddEntity(create(world.EntitySpawnOpts{
		Position: spawnPos,
		Rotation: rot,
	}, b.ensureVariant(), b.Chest))

	ctx.SubtractFromCount(1)
	return true
}

func (b Boat) ensureVariant() string {
	if b.Variant == "" {
		return "oak"
	}
	return b.Variant
}
