package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MossBlock is a mossy ground block.
type MossBlock struct {
	solid
}

// SoilFor ...
func (m MossBlock) SoilFor(block world.Block) bool {
	switch block.(type) {
	case ShortGrass, Fern, DoubleTallGrass, Flower, DoubleFlower, NetherSprouts, PinkPetals, SugarCane, DeadBush:
		return true
	}
	return false
}

// BreakInfo ...
func (m MossBlock) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, hoeEffective, oneOf(m)).withBlastResistance(2.5)
}

// EncodeItem ...
func (MossBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:moss_block", 0
}

// EncodeBlock ...
func (MossBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:moss_block", nil
}

// PaleMossBlock is a pale moss block.
type PaleMossBlock struct {
	solid
}

// SoilFor ...
func (m PaleMossBlock) SoilFor(block world.Block) bool {
	switch block.(type) {
	case ShortGrass, Fern, DoubleTallGrass, Flower, DoubleFlower, NetherSprouts, PinkPetals, SugarCane, DeadBush:
		return true
	}
	return false
}

// BreakInfo ...
func (m PaleMossBlock) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, hoeEffective, oneOf(m)).withBlastResistance(2.5)
}

// EncodeItem ...
func (PaleMossBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:pale_moss_block", 0
}

// EncodeBlock ...
func (PaleMossBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:pale_moss_block", nil
}

// PaleMossCarpet is a pale moss carpet with wall-like sides.
type PaleMossCarpet struct {
	carpet
	transparent
	sourceWaterDisplacer

	NorthConnection WallConnectionType
	EastConnection  WallConnectionType
	SouthConnection WallConnectionType
	WestConnection  WallConnectionType
	Upper           bool
}

// SideClosed ...
func (PaleMossCarpet) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// HasLiquidDrops ...
func (PaleMossCarpet) HasLiquidDrops() bool {
	return true
}

// NeighbourUpdateTick ...
func (m PaleMossCarpet) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		breakBlock(m, pos, tx)
	}
}

// UseOnBlock ...
func (m PaleMossCarpet) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, m)
	if !used {
		return
	}
	if !tx.Block(pos.Side(cube.FaceDown)).Model().FaceSolid(pos.Side(cube.FaceDown), cube.FaceUp, tx) {
		return
	}

	place(tx, pos, m, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (m PaleMossCarpet) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, nothingEffective, oneOf(m))
}

// CompostChance ...
func (PaleMossCarpet) CompostChance() float64 {
	return 0.3
}

// EncodeItem ...
func (PaleMossCarpet) EncodeItem() (name string, meta int16) {
	return "minecraft:pale_moss_carpet", 0
}

// EncodeBlock ...
func (m PaleMossCarpet) EncodeBlock() (string, map[string]any) {
	return "minecraft:pale_moss_carpet", map[string]any{
		"pale_moss_carpet_side_north": m.NorthConnection.String(),
		"pale_moss_carpet_side_east":  m.EastConnection.String(),
		"pale_moss_carpet_side_south": m.SouthConnection.String(),
		"pale_moss_carpet_side_west":  m.WestConnection.String(),
		"upper_block_bit":             boolByte(m.Upper),
	}
}

// allPaleMossCarpet returns all pale moss carpet states.
func allPaleMossCarpet() (b []world.Block) {
	types := []WallConnectionType{NoWallConnection(), ShortWallConnection(), TallWallConnection()}
	for _, north := range types {
		for _, east := range types {
			for _, south := range types {
				for _, west := range types {
					b = append(b, PaleMossCarpet{
						NorthConnection: north,
						EastConnection:  east,
						SouthConnection: south,
						WestConnection:  west,
						Upper:           false,
					})
					b = append(b, PaleMossCarpet{
						NorthConnection: north,
						EastConnection:  east,
						SouthConnection: south,
						WestConnection:  west,
						Upper:           true,
					})
				}
			}
		}
	}
	return
}
