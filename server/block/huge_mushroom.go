package block

import "github.com/df-mc/dragonfly/server/world"

// MushroomBlockKind represents the kind of huge mushroom block.
type MushroomBlockKind struct {
	kind
}

// BrownMushroomBlock returns the brown mushroom block kind.
func BrownMushroomBlock() MushroomBlockKind {
	return MushroomBlockKind{0}
}

// RedMushroomBlock returns the red mushroom block kind.
func RedMushroomBlock() MushroomBlockKind {
	return MushroomBlockKind{1}
}

// MushroomStemBlock returns the mushroom stem kind.
func MushroomStemBlock() MushroomBlockKind {
	return MushroomBlockKind{2}
}

type kind uint8

// Uint8 returns the kind as a uint8.
func (k kind) Uint8() uint8 {
	return uint8(k)
}

// String returns the kind as a string.
func (k kind) String() string {
	switch k {
	case 0:
		return "brown_mushroom_block"
	case 1:
		return "red_mushroom_block"
	case 2:
		return "mushroom_stem"
	}
	panic("unknown mushroom block kind")
}

// HugeMushroomBlock is a huge mushroom block variant.
type HugeMushroomBlock struct {
	solid

	Kind    MushroomBlockKind
	Variant int
}

// BreakInfo ...
func (m HugeMushroomBlock) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, alwaysHarvestable, axeEffective, silkTouchOnlyDrop(m))
}

// EncodeItem ...
func (m HugeMushroomBlock) EncodeItem() (name string, meta int16) {
	return "minecraft:" + m.Kind.String(), 0
}

// EncodeBlock ...
func (m HugeMushroomBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:" + m.Kind.String(), map[string]any{"huge_mushroom_bits": int32(m.Variant)}
}

// allHugeMushroomBlocks returns all huge mushroom block variants for a kind.
func allHugeMushroomBlocks(kind MushroomBlockKind) (b []world.Block) {
	for i := 0; i <= 15; i++ {
		b = append(b, HugeMushroomBlock{Kind: kind, Variant: i})
	}
	return
}
