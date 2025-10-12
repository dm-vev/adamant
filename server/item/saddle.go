package item

// Saddle is an item used to ride specific mobs.
type Saddle struct{}

// MaxCount ...
func (Saddle) MaxCount() int {
	return 1
}

// EncodeItem ...
func (Saddle) EncodeItem() (name string, meta int16) {
	return "minecraft:saddle", 0
}
