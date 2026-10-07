package world

import "github.com/df-mc/dragonfly/server/block/cube"

// DragonFight connects an EnderDragon to the End dimension's persisted fight lifecycle.
type DragonFight interface {
	UpdateDragon(tx *Tx, dragon Entity)
	SetDragonKilled(tx *Tx, dragon Entity)
	OnCrystalDestroyed(tx *Tx, crystal Entity, pos cube.Pos, source DamageSource)
	HasPreviouslyKilledDragon() bool
	AliveCrystals() int
}
