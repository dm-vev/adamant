package item

// BreezeRod is a rare component dropped by breezes and used to craft or repair maces.
type BreezeRod struct{}

// EncodeItem ...
func (BreezeRod) EncodeItem() (name string, meta int16) {
	return "minecraft:breeze_rod", 0
}
