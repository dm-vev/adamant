package item

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BoatVariant represents a wood variant of a boat.
type BoatVariant uint8

const (
	BoatVariantOak BoatVariant = iota
	BoatVariantSpruce
	BoatVariantBirch
	BoatVariantJungle
	BoatVariantAcacia
	BoatVariantDarkOak
	BoatVariantMangrove
	BoatVariantBamboo
	BoatVariantCherry
	BoatVariantPaleOak
)

// BoatVariants returns all supported boat variants.
func BoatVariants() []BoatVariant {
	return []BoatVariant{
		BoatVariantOak,
		BoatVariantSpruce,
		BoatVariantBirch,
		BoatVariantJungle,
		BoatVariantAcacia,
		BoatVariantDarkOak,
		BoatVariantMangrove,
		BoatVariantBamboo,
		BoatVariantCherry,
		BoatVariantPaleOak,
	}
}

// Int returns the integer representation of the variant.
func (v BoatVariant) Int() int {
	return int(v)
}

// BoatVariantFromInt converts an integer to a boat variant, defaulting to oak for unknown values.
func BoatVariantFromInt(variant int) BoatVariant {
	if variant < int(BoatVariantOak) || variant > int(BoatVariantPaleOak) {
		return BoatVariantOak
	}
	return BoatVariant(variant)
}

func (v BoatVariant) boatItemName() string {
	switch v {
	case BoatVariantSpruce:
		return "minecraft:spruce_boat"
	case BoatVariantBirch:
		return "minecraft:birch_boat"
	case BoatVariantJungle:
		return "minecraft:jungle_boat"
	case BoatVariantAcacia:
		return "minecraft:acacia_boat"
	case BoatVariantDarkOak:
		return "minecraft:dark_oak_boat"
	case BoatVariantMangrove:
		return "minecraft:mangrove_boat"
	case BoatVariantBamboo:
		return "minecraft:bamboo_raft"
	case BoatVariantCherry:
		return "minecraft:cherry_boat"
	case BoatVariantPaleOak:
		return "minecraft:pale_oak_boat"
	default:
		return "minecraft:oak_boat"
	}
}

func (v BoatVariant) chestBoatItemName() string {
	switch v {
	case BoatVariantSpruce:
		return "minecraft:spruce_chest_boat"
	case BoatVariantBirch:
		return "minecraft:birch_chest_boat"
	case BoatVariantJungle:
		return "minecraft:jungle_chest_boat"
	case BoatVariantAcacia:
		return "minecraft:acacia_chest_boat"
	case BoatVariantDarkOak:
		return "minecraft:dark_oak_chest_boat"
	case BoatVariantMangrove:
		return "minecraft:mangrove_chest_boat"
	case BoatVariantBamboo:
		return "minecraft:bamboo_chest_raft"
	case BoatVariantCherry:
		return "minecraft:cherry_chest_boat"
	case BoatVariantPaleOak:
		return "minecraft:pale_oak_chest_boat"
	default:
		return "minecraft:oak_chest_boat"
	}
}

// Boat is an item that spawns a boat entity.
type Boat struct {
	// Variant determines the wood variant used for this boat.
	Variant BoatVariant
}

// UseOnBlock places a boat entity.
func (b Boat) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().Boat
	return placeBoat(pos, face, tx, user, ctx, b.Variant, create, "boat")
}

// EncodeItem returns the item ID for the boat variant.
func (b Boat) EncodeItem() (string, int16) {
	return b.Variant.boatItemName(), 0
}

// MaxCount returns the maximum stack size for boats.
func (Boat) MaxCount() int {
	return 1
}

// ChestBoat is an item that spawns a chest boat entity.
type ChestBoat struct {
	// Variant determines the wood variant used for this chest boat.
	Variant BoatVariant
}

// UseOnBlock places a chest boat entity.
func (b ChestBoat) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	create := tx.World().EntityRegistry().Config().ChestBoat
	return placeBoat(pos, face, tx, user, ctx, b.Variant, create, "chest_boat")
}

// EncodeItem returns the item ID for the chest boat variant.
func (b ChestBoat) EncodeItem() (string, int16) {
	return b.Variant.chestBoatItemName(), 0
}

// MaxCount returns the maximum stack size for chest boats.
func (ChestBoat) MaxCount() int {
	return 1
}

func placeBoat(pos cube.Pos, face cube.Face, tx *world.Tx, user User, ctx *UseContext, variant BoatVariant, create func(world.EntitySpawnOpts, int) *world.EntityHandle, name string) bool {
	if face == cube.FaceDown {
		return false
	}
	if create == nil {
		slog.Default().Info("boat: missing entity factory", "name", name, "world", tx.World().Name())
		return false
	}

	placePos := pos.Side(face)
	if face == cube.FaceUp {
		if liquid, ok := tx.Liquid(pos); ok && liquid.LiquidType() == "water" {
			placePos = pos
		}
	}
	if _, liquid := tx.Liquid(placePos); !liquid {
		blockName, _ := tx.Block(placePos).EncodeBlock()
		if blockName != "minecraft:air" {
			return false
		}
	}

	spawn := placePos.Vec3Middle()
	if liquid, ok := tx.Liquid(placePos); ok && liquid.LiquidType() == "water" {
		spawn[1] += 0.375
	} else {
		spawn[1] += 0.5
	}
	rot := user.Rotation().Neg()
	rot = cube.Rotation{rot.Yaw(), 0}

	opts := world.EntitySpawnOpts{Position: spawn, Rotation: rot}
	tx.AddEntity(create(opts, variant.Int()))
	ctx.SubtractFromCount(1)
	return true
}
