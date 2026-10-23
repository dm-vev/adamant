package particle

// TrialSpawnerDetection is the burst of flames a trial spawner shows when it
// detects new players. Players is how many players it has detected; Ominous
// selects the soul flames of an ominous spawner.
type TrialSpawnerDetection struct {
	particle
	Players int
	Ominous bool
}

// TrialSpawnerSpawning is the burst of flames shown at a trial spawner and at
// the mob it spawns.
type TrialSpawnerSpawning struct {
	particle
	Ominous bool
}

// TrialSpawnerEjecting is the burst of flames and smoke a trial spawner shows
// when it ejects a reward.
type TrialSpawnerEjecting struct{ particle }

// TrialSpawnerBecomeOminous is shown at a trial spawner turning ominous and
// at the player that made it so.
type TrialSpawnerBecomeOminous struct{ particle }

// VaultActivate is shown by a vault that becomes active as a player comes
// near.
type VaultActivate struct {
	particle
	Ominous bool
}

// VaultDeactivate is shown by a vault that becomes inactive.
type VaultDeactivate struct {
	particle
	Ominous bool
}

// VaultEjectItem is shown by a vault that ejects one of its rewards.
type VaultEjectItem struct{ particle }
