package item

// NetheriteScrap is a material smelted from ancient debris, which is found in the Nether.
type NetheriteScrap struct{}

// EncodeItem ...
func (NetheriteScrap) EncodeItem() (name string, meta int16) {
	return "minecraft:netherite_scrap", 0
}

// FireProof reports that the item survives fire and lava, as its
// damage_resistant component for is_fire.
func (NetheriteScrap) FireProof() bool { return true }
