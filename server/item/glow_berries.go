package item

// GlowBerries is the native berry item dropped by cave vines.
type GlowBerries struct{}

// EncodeItem encodes the glow berries item.
func (GlowBerries) EncodeItem() (string, int16) { return "minecraft:glow_berries", 0 }
