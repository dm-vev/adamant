package item

// EyeOfEnder is an item used to activate end portal frames.
type EyeOfEnder struct{}

// EncodeItem ...
func (EyeOfEnder) EncodeItem() (name string, meta int16) {
	return "minecraft:ender_eye", 0
}
