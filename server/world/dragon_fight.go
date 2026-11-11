package world

import "github.com/df-mc/dragonfly/server/block/cube"

// endState is the dragon fight of the world and the arena that keeps it loaded.
type endState struct {
	dragonFight DragonFightController
	dragonArena *Loader
}

// DragonFight connects an EnderDragon to the End dimension's persisted fight lifecycle.
type DragonFight interface {
	UpdateDragon(tx *Tx, dragon Entity)
	SetDragonKilled(tx *Tx, dragon Entity)
	OnCrystalDestroyed(tx *Tx, crystal Entity, pos cube.Pos, source DamageSource)
	HasPreviouslyKilledDragon() bool
	AliveCrystals() int
}
