package block

import (
	"github.com/df-mc/dragonfly/server/item"
	"time"
)

// CartographyTable is a job site block for cartographer villagers.
type CartographyTable struct {
	solid
	bass
}

// BreakInfo ...
func (c CartographyTable) BreakInfo() BreakInfo {
	return newBreakInfo(2.5, alwaysHarvestable, axeEffective, oneOf(c))
}

// FuelInfo ...
func (CartographyTable) FuelInfo() item.FuelInfo {
	return newFuelInfo(time.Second * 15)
}

// EncodeItem ...
func (CartographyTable) EncodeItem() (string, int16) {
	return "minecraft:cartography_table", 0
}

// EncodeBlock ...
func (CartographyTable) EncodeBlock() (string, map[string]any) {
	return "minecraft:cartography_table", nil
}

// Lodestone is a decorative stone block that compasses can lock onto.
type Lodestone struct {
	solid
	bassDrum
}

// BreakInfo ...
func (l Lodestone) BreakInfo() BreakInfo {
	return newBreakInfo(3.5, pickaxeHarvestable, pickaxeEffective, oneOf(l)).withBlastResistance(3.5)
}

// EncodeItem ...
func (Lodestone) EncodeItem() (string, int16) {
	return "minecraft:lodestone", 0
}

// EncodeBlock ...
func (Lodestone) EncodeBlock() (string, map[string]any) {
	return "minecraft:lodestone", nil
}

// BuddingAmethyst is an amethyst block variant that grows clusters.
type BuddingAmethyst struct {
	solid
}

// BreakInfo ...
func (b BuddingAmethyst) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, pickaxeHarvestable, pickaxeEffective, simpleDrops()).withBlastResistance(1.5)
}

// EncodeItem ...
func (BuddingAmethyst) EncodeItem() (string, int16) {
	return "minecraft:budding_amethyst", 0
}

// EncodeBlock ...
func (BuddingAmethyst) EncodeBlock() (string, map[string]any) {
	return "minecraft:budding_amethyst", nil
}

// Camera is an Education Edition block.
type Camera struct {
	solid
}

// BreakInfo ...
func (c Camera) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(c))
}

// EncodeItem ...
func (Camera) EncodeItem() (string, int16) {
	return "minecraft:camera", 0
}

// EncodeBlock ...
func (Camera) EncodeBlock() (string, map[string]any) {
	return "minecraft:camera", nil
}

// ChemicalHeat is an Education Edition block.
type ChemicalHeat struct {
	solid
}

// BreakInfo ...
func (c ChemicalHeat) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, alwaysHarvestable, nothingEffective, oneOf(c))
}

// EncodeItem ...
func (ChemicalHeat) EncodeItem() (string, int16) {
	return "minecraft:chemical_heat", 0
}

// EncodeBlock ...
func (ChemicalHeat) EncodeBlock() (string, map[string]any) {
	return "minecraft:chemical_heat", nil
}
