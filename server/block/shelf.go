package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"time"
)

// Shelf is a decorative wooden storage shelf block.
type Shelf struct {
	solid
	bass

	Material         ShelfMaterial
	Facing           cube.Direction
	Powered          bool
	PoweredShelfType uint8
}

// UseOnBlock ...
func (s Shelf) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, s)
	if !used {
		return
	}
	s.Facing = user.Rotation().Direction().Opposite()
	s.Powered = false
	s.PoweredShelfType = 0

	place(tx, pos, s, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (s Shelf) BreakInfo() BreakInfo {
	return newBreakInfo(2.5, alwaysHarvestable, axeEffective, oneOf(s)).withBlastResistance(3)
}

// FlammabilityInfo ...
func (s Shelf) FlammabilityInfo() FlammabilityInfo {
	if !s.Material.Flammable() {
		return newFlammabilityInfo(0, 0, false)
	}
	return newFlammabilityInfo(5, 20, true)
}

// FuelInfo ...
func (s Shelf) FuelInfo() item.FuelInfo {
	if !s.Material.Flammable() {
		return item.FuelInfo{}
	}
	return newFuelInfo(time.Second * 15)
}

// EncodeItem ...
func (s Shelf) EncodeItem() (string, int16) {
	return "minecraft:" + s.Material.String() + "_shelf", 0
}

// EncodeBlock ...
func (s Shelf) EncodeBlock() (string, map[string]any) {
	return "minecraft:" + s.Material.String() + "_shelf", map[string]any{
		"minecraft:cardinal_direction": s.Facing.String(),
		"powered_bit":                  boolByte(s.Powered),
		"powered_shelf_type":           int32(s.PoweredShelfType),
	}
}

// allShelves ...
func allShelves() (shelves []world.Block) {
	for _, material := range ShelfMaterials() {
		for _, facing := range cube.Directions() {
			for poweredType := uint8(0); poweredType < 4; poweredType++ {
				shelves = append(shelves,
					Shelf{Material: material, Facing: facing, PoweredShelfType: poweredType},
					Shelf{Material: material, Facing: facing, Powered: true, PoweredShelfType: poweredType},
				)
			}
		}
	}
	return
}

// ShelfMaterial represents a shelf material.
type ShelfMaterial uint8

const (
	shelfOak ShelfMaterial = iota
	shelfSpruce
	shelfBirch
	shelfJungle
	shelfAcacia
	shelfDarkOak
	shelfCrimson
	shelfWarped
	shelfMangrove
	shelfCherry
	shelfPaleOak
	shelfBamboo
)

// Uint8 returns the material as a uint8.
func (m ShelfMaterial) Uint8() uint8 {
	return uint8(m)
}

// String ...
func (m ShelfMaterial) String() string {
	switch m {
	case shelfOak:
		return "oak"
	case shelfSpruce:
		return "spruce"
	case shelfBirch:
		return "birch"
	case shelfJungle:
		return "jungle"
	case shelfAcacia:
		return "acacia"
	case shelfDarkOak:
		return "dark_oak"
	case shelfCrimson:
		return "crimson"
	case shelfWarped:
		return "warped"
	case shelfMangrove:
		return "mangrove"
	case shelfCherry:
		return "cherry"
	case shelfPaleOak:
		return "pale_oak"
	case shelfBamboo:
		return "bamboo"
	}
	panic("unknown shelf material")
}

// Flammable reports if the shelf material burns.
func (m ShelfMaterial) Flammable() bool {
	return m != shelfCrimson && m != shelfWarped
}

// ShelfMaterials returns all shelf materials.
func ShelfMaterials() []ShelfMaterial {
	return []ShelfMaterial{
		shelfOak, shelfSpruce, shelfBirch, shelfJungle, shelfAcacia, shelfDarkOak,
		shelfCrimson, shelfWarped, shelfMangrove, shelfCherry, shelfPaleOak, shelfBamboo,
	}
}
