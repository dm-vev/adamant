package block

import (
	"math/rand/v2"

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
	case ShortGrass, Fern, DoubleTallGrass, Flower, GoldenDandelion, Torchflower, Eyeblossom, DoubleFlower, PitcherPlant, NetherSprouts, PinkPetals, SugarCane, DeadBush, TreeSapling:
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
	case ShortGrass, Fern, DoubleTallGrass, Flower, GoldenDandelion, Torchflower, Eyeblossom, DoubleFlower, PitcherPlant, NetherSprouts, PinkPetals, SugarCane, DeadBush, TreeSapling:
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

// side returns the wall side of the carpet toward a horizontal face.
func (m PaleMossCarpet) side(face cube.Face) WallConnectionType {
	switch face {
	case cube.FaceNorth:
		return m.NorthConnection
	case cube.FaceEast:
		return m.EastConnection
	case cube.FaceSouth:
		return m.SouthConnection
	default:
		return m.WestConnection
	}
}

// withSide returns the carpet with its wall side toward a horizontal face set.
func (m PaleMossCarpet) withSide(face cube.Face, side WallConnectionType) PaleMossCarpet {
	switch face {
	case cube.FaceNorth:
		m.NorthConnection = side
	case cube.FaceEast:
		m.EastConnection = side
	case cube.FaceSouth:
		m.SouthConnection = side
	default:
		m.WestConnection = side
	}
	return m
}

// hasFaces ports MossyCarpetBlock.hasFaces: a base carpet, or a topper with
// a side.
func (m PaleMossCarpet) hasFaces() bool {
	if !m.Upper {
		return true
	}
	for _, face := range cube.HorizontalFaces() {
		if m.side(face) != NoWallConnection() {
			return true
		}
	}
	return false
}

// survives ports MossyCarpetBlock.canSurvive: a base carpet rests on any
// block, a topper on a base carpet.
func (m PaleMossCarpet) survives(pos cube.Pos, tx *world.Tx) bool {
	below := tx.Block(pos.Side(cube.FaceDown))
	if !m.Upper {
		return !isAir(below)
	}
	carpet, ok := below.(PaleMossCarpet)
	return ok && !carpet.Upper
}

// updated ports MossyCarpetBlock.getUpdatedState: a side grows low against
// each wall it can attach to, tall below a topper growing there, and none
// above a carpet without that side.
func (m PaleMossCarpet) updated(pos cube.Pos, tx *world.Tx, createSides bool) PaleMossCarpet {
	createSides = createSides || !m.Upper
	for _, face := range cube.HorizontalFaces() {
		side := NoWallConnection()
		if multifaceCanAttachTo(tx, pos, face) {
			side = m.side(face)
			if createSides {
				side = ShortWallConnection()
			}
		}
		if side == ShortWallConnection() {
			if above, ok := tx.Block(pos.Side(cube.FaceUp)).(PaleMossCarpet); ok && above.Upper && above.side(face) != NoWallConnection() {
				side = TallWallConnection()
			}
			if m.Upper {
				if below, ok := tx.Block(pos.Side(cube.FaceDown)).(PaleMossCarpet); ok && below.side(face) == NoWallConnection() {
					side = NoWallConnection()
				}
			}
		}
		m = m.withSide(face, side)
	}
	return m
}

// paleMossTopper ports MossyCarpetBlock.createTopperWithSideChance: the
// topper a carpet grows above itself, each side surviving if keep says so,
// and whether there is one to grow.
func paleMossTopper(pos cube.Pos, tx *world.Tx, keep func() bool) (PaleMossCarpet, bool) {
	above := pos.Side(cube.FaceUp)
	previous := tx.Block(above)
	carpet, isCarpet := previous.(PaleMossCarpet)
	if isCarpet && !carpet.Upper {
		return PaleMossCarpet{}, false
	}
	if r, ok := previous.(Replaceable); !isCarpet && (!ok || !r.ReplaceableBy(PaleMossCarpet{})) {
		return PaleMossCarpet{}, false
	}
	topper := PaleMossCarpet{Upper: true}.updated(above, tx, true)
	for _, face := range cube.HorizontalFaces() {
		if topper.side(face) != NoWallConnection() && !keep() {
			topper = topper.withSide(face, NoWallConnection())
		}
	}
	if !topper.hasFaces() || (isCarpet && carpet == topper) {
		return PaleMossCarpet{}, false
	}
	return topper, true
}

// NeighbourUpdateTick ports MossyCarpetBlock.updateShape: a carpet that no
// longer survives or has no faces left breaks, others update their sides.
func (m PaleMossCarpet) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !m.survives(pos, tx) {
		breakBlock(m, pos, tx)
		return
	}
	updated := m.updated(pos, tx, false)
	if !updated.hasFaces() {
		breakBlock(m, pos, tx)
		return
	}
	if updated != m {
		tx.SetBlock(pos, updated, nil)
	}
}

// UseOnBlock places a base carpet with its sides, as getStateForPlacement,
// and a topper above with each side kept by chance, as setPlacedBy.
func (m PaleMossCarpet) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, m)
	if !used {
		return
	}
	carpet := PaleMossCarpet{}.updated(pos, tx, true)
	if !carpet.survives(pos, tx) {
		return false
	}
	place(tx, pos, carpet, user, ctx)
	if !placed(ctx) {
		return false
	}
	if topper, ok := paleMossTopper(pos, tx, func() bool { return rand.IntN(2) == 0 }); ok {
		tx.SetBlock(pos.Side(cube.FaceUp), topper, nil)
	}
	return true
}

// BoneMeal grows a topper with every side above a base carpet, as
// MossyCarpetBlock.performBonemeal.
func (m PaleMossCarpet) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if m.Upper {
		return item.BoneMealResultNone
	}
	topper, ok := paleMossTopper(pos, tx, func() bool { return true })
	if !ok {
		return item.BoneMealResultNone
	}
	tx.SetBlock(pos.Side(cube.FaceUp), topper, nil)
	return item.BoneMealResultSmall
}

// BreakInfo ...
func (m PaleMossCarpet) BreakInfo() BreakInfo {
	return newBreakInfo(0.1, alwaysHarvestable, nothingEffective, oneOf(m))
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
