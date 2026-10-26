package item

// RawIron is a raw metal resource obtained from mining iron ore.
type RawIron struct{}

// EncodeItem ...
func (RawIron) EncodeItem() (name string, meta int16) {
	return "minecraft:raw_iron", 0
}
