package item

// TripwireHook is a redstone component dropped when fishing junk.
type TripwireHook struct{}

// MaxCount ...
func (TripwireHook) MaxCount() int {
	return 64
}

// EncodeItem ...
func (TripwireHook) EncodeItem() (name string, meta int16) {
	return "minecraft:tripwire_hook", 0
}
