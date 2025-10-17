package item

// HeavyCore is a trial chamber relic required to forge a mace.
type HeavyCore struct{}

// EncodeItem ...
func (HeavyCore) EncodeItem() (name string, meta int16) {
	return "minecraft:heavy_core", 0
}
