package world

import (
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// poiState is the points of interest of the world.
type poiState struct {
	pois map[cube.Pos]POI
	// poiClasses classifies the POI of every block state by runtime ID. It is
	// built on first use, once the block registry is final.
	poiClasses     []poiClass
	poiClassesOnce sync.Once
}

// POIs returns a transaction-scoped handle for the points of interest and the villages of the world.
func (tx *Tx) POIs() POITransaction {
	return POITransaction{tx: tx}
}

// POITransaction provides access to the points of interest of the world, and the villages they make, within a
// transaction. Its methods are those of the transaction that concern them, named without the word POI where the
// handle says it already: POIs().At, POIs().Nearest, POIs().Claim. The words village and section stay:
// POIs().IsVillage, POIs().VillageDistance.
type POITransaction struct {
	tx *Tx
}
