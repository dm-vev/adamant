package item

// TorchflowerSeeds are dropped by sniffer archaeological finds and can be
// planted to grow a torchflower.
type TorchflowerSeeds struct{}

// EncodeItem returns the Bedrock item identifier.
func (TorchflowerSeeds) EncodeItem() (name string, meta int16) {
	return "minecraft:torchflower_seeds", 0
}

// PitcherPod is dropped by sniffer archaeological finds and can be planted to
// grow a pitcher plant.
type PitcherPod struct{}

// EncodeItem returns the Bedrock item identifier.
func (PitcherPod) EncodeItem() (name string, meta int16) {
	return "minecraft:pitcher_pod", 0
}
