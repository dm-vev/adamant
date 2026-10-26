package block

// BambooMosaic is a decorative bamboo plank variant.
type BambooMosaic struct {
	solid
	bass
}

// FlammabilityInfo ...
func (BambooMosaic) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(5, 20, true)
}

// BreakInfo ...
func (b BambooMosaic) BreakInfo() BreakInfo {
	return newBreakInfo(2, alwaysHarvestable, axeEffective, oneOf(b)).withBlastResistance(3)
}

// RepairsWoodTools ...
func (BambooMosaic) RepairsWoodTools() bool {
	return true
}

// EncodeItem ...
func (BambooMosaic) EncodeItem() (name string, meta int16) {
	return "minecraft:bamboo_mosaic", 0
}

// EncodeBlock ...
func (BambooMosaic) EncodeBlock() (string, map[string]any) {
	return "minecraft:bamboo_mosaic", nil
}
