package world

import "github.com/df-mc/dragonfly/server/block/cube"

// DragonFightData is the persisted state of the Ender Dragon fight.
type DragonFightData struct {
	DragonKilled          bool
	DragonSpawned         bool
	PreviouslyKilled      bool
	DragonUniqueID        int64
	ExitPortal            *cube.Pos
	Gateways              []int32
	IsRespawning          bool
	NeedsStateScanning    bool
	RespawnStage          string
	RespawnTime           int32
	RespawnCrystalIDs     []int64
	Version               uint8
}

// DragonFightProvider optionally persists End dragon fight state.
type DragonFightProvider interface {
	LoadDragonFightData() (DragonFightData, bool, error)
	SaveDragonFightData(DragonFightData) error
}
