package item

// NameTag is an item used to rename entities in an anvil and apply the result to mobs.
type NameTag struct{}

// MaxCount ...
func (NameTag) MaxCount() int {
	return 1
}

// EncodeItem ...
func (NameTag) EncodeItem() (name string, meta int16) {
	return "minecraft:name_tag", 0
}
