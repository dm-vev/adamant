package item

// ArmadilloScute is a material dropped by armadillos and used to repair wolf armour.
type ArmadilloScute struct{}

// EncodeItem returns the Bedrock item identifier.
func (ArmadilloScute) EncodeItem() (string, int16) { return "minecraft:armadillo_scute", 0 }
