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
	BoatVariantPoplar
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
		BoatVariantPoplar,
	}
}

// Int returns the integer representation of the variant.
func (v BoatVariant) Int() int {
	return int(v)
}

// BoatVariantFromInt converts an integer to a boat variant, defaulting to oak for unknown values.
func BoatVariantFromInt(variant int) BoatVariant {
	if variant < int(BoatVariantOak) || variant > int(BoatVariantPoplar) {
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
	case BoatVariantPoplar:
		return "minecraft:poplar_boat"
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
	case BoatVariantPoplar:
		return "minecraft:poplar_chest_boat"
	default:
		return "minecraft:oak_chest_boat"
	}
}

// BoatVariantFromID resolves the identifier of a boat or chest boat, such as
// minecraft:oak_boat or minecraft:bamboo_chest_raft, which Java and Bedrock
// share for both the item and the entity.
func BoatVariantFromID(id string) (variant BoatVariant, chest, ok bool) {
	for _, v := range BoatVariants() {
		switch id {
		case v.boatItemName():
			return v, false, true
		case v.chestBoatItemName():
			return v, true, true
		}
	}
	return BoatVariantOak, false, false
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

// Use places a boat on what the player looks at, such as water.
func (b Boat) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	return placeBoat(cube.Pos{}, cube.FaceUp, tx, user, ctx, b.Variant, tx.World().EntityRegistry().Config().Boat, "boat")
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

// Use places a chest boat on what the player looks at, such as water.
func (b ChestBoat) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	return placeBoat(cube.Pos{}, cube.FaceUp, tx, user, ctx, b.Variant, tx.World().EntityRegistry().Config().ChestBoat, "chest_boat")
}

// EncodeItem returns the item ID for the chest boat variant.
func (b ChestBoat) EncodeItem() (string, int16) {
	return b.Variant.chestBoatItemName(), 0
}

// MaxCount returns the maximum stack size for chest boats.
func (ChestBoat) MaxCount() int {
	return 1
}

// placeBoat ports BoatItem.use: the boat is put where the player's view
// first meets a block or a liquid surface, facing as the player does, unless
// the player is inside an entity it could pick instead or the boat would
// collide with something.
func placeBoat(_ cube.Pos, _ cube.Face, tx *world.Tx, user User, ctx *UseContext, variant BoatVariant, create func(world.EntitySpawnOpts, int) *world.EntityHandle, name string) bool {
	if create == nil {
		slog.Default().Info("boat: missing entity factory", "name", name, "world", tx.World().Name())
		return false
	}
	hit, _, ok := povHit(tx, user, true)
	if !ok {
		return false
	}
	eye := user.Position()
	if e, ok := user.(interface{ EyeHeight() float64 }); ok {
		eye[1] += e.EyeHeight()
	}
	reachBox := cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3).Translate(user.Position()).Extend(user.Rotation().Vec3().Mul(5)).Grow(1)
	for e := range tx.EntitiesWithin(reachBox) {
		if e.H() == user.H() {
			continue
		}
		if e.H().Type().BBox(e).Translate(e.Position()).Vec3Within(eye) {
			return false
		}
	}
	// AbstractBoat is 1.375 wide and 0.5625 high.
	if !noCollision(tx, cube.Box(-0.6875, 0, -0.6875, 0.6875, 0.5625, 0.6875).Translate(hit), nil) {
		return false
	}
	opts := stackSpawnOpts(user, world.EntitySpawnOpts{Position: hit, Rotation: cube.Rotation{user.Rotation().Yaw(), 0}})
	tx.AddEntity(create(opts, variant.Int()))
	ctx.SubtractFromCount(1)
	return true
}
