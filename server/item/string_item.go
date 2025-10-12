package item

// String is a crafting material dropped by spiders and found in various structures.
type String struct{}

// MaxCount ...
func (String) MaxCount() int {
	return 64
}

// EncodeItem ...
func (String) EncodeItem() (name string, meta int16) {
	return "minecraft:string", 0
}
