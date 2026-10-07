package item

// TrialKey is a key used to activate vaults in trial chambers.
type TrialKey struct{}

// MaxCount returns the maximum stack size for the item.
func (TrialKey) MaxCount() int { return 64 }

// EncodeItem returns the Bedrock item identifier.
func (TrialKey) EncodeItem() (name string, meta int16) { return "minecraft:trial_key", 0 }

// OminousTrialKey is a key used to activate ominous vaults in trial chambers.
type OminousTrialKey struct{}

// MaxCount returns the maximum stack size for the item.
func (OminousTrialKey) MaxCount() int { return 64 }

// EncodeItem returns the Bedrock item identifier.
func (OminousTrialKey) EncodeItem() (name string, meta int16) { return "minecraft:ominous_trial_key", 0 }
