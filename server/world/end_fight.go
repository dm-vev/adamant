package world

import (
	"fmt"
	"iter"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// DragonFightController owns the End arena lifecycle in addition to the callbacks
// used by dragons and crystals. Implementations run only on the world owner.
type DragonFightController interface {
	DragonFight
	Tick(tx *Tx)
	Data() DragonFightData
	TryRespawn(tx *Tx) bool
	AttachDragon(dragon Entity)
}

// DragonFight returns the configured End fight, or nil in other dimensions.
func (tx *Tx) DragonFight() DragonFightController {
	return tx.World().dragonFight
}

func (w *World) loadDragonFight() error {
	factory := w.conf.Entities.conf.DragonFight
	if w.Dimension() != End || factory == nil {
		return nil
	}
	data := DragonFightData{NeedsStateScanning: true}
	if provider, ok := w.conf.Provider.(DragonFightProvider); ok {
		stored, present, err := provider.LoadDragonFightData()
		if err != nil {
			return fmt.Errorf("load End dragon fight: %w", err)
		}
		if present {
			data = stored
		}
	}
	w.dragonFight = factory(w.Seed(), data)
	return nil
}

func (w *World) saveDragonFight() {
	if w.dragonFight == nil || w.conf.ReadOnly {
		return
	}
	if provider, ok := w.conf.Provider.(DragonFightProvider); ok {
		if err := provider.SaveDragonFightData(w.dragonFight.Data()); err != nil {
			w.conf.Log.Error("save End dragon fight", "error", err)
		}
	}
}

// LoadDragonArena holds the source 17-by-17 chunk arena while players participate.
// Loading is asynchronous; false means the fight must wait for chunk readiness.
// Passing false releases the arena ticket without retaining an artificial viewer.
func (tx *Tx) LoadDragonArena(active bool) bool {
	w := tx.World()
	if !active {
		if w.dragonArena != nil {
			arena := w.dragonArena
			w.dragonArena = nil
			arena.Close(tx)
		}
		return false
	}
	if w.Dimension() != End {
		return false
	}
	if w.dragonArena == nil {
		arena := &Loader{
			r: 8,
			square: true,
			forceActive: true,
			loaded: make(map[ChunkPos]*Column, 17*17),
			pending: make(map[ChunkPos]struct{}, 17*17),
			viewer: NopViewer{},
		}
		arena.world(w)
		w.dragonArena = arena
	}
	w.dragonArena.Load(tx, 17*17)
	for x := int32(-8); x <= 8; x++ {
		for z := int32(-8); z <= 8; z++ {
			col, loaded := w.dragonArena.Chunk(ChunkPos{x, z})
			if !loaded || !col.Ready() {
				return false
			}
		}
	}
	return true
}

// EndPortalPositions visits the loaded End arena's portal actors without
// scanning empty cells or generating chunks outside the arena ticket.
func (tx *Tx) EndPortalPositions() iter.Seq[cube.Pos] {
	return func(yield func(cube.Pos) bool) {
		for x := int32(-8); x <= 8; x++ {
			for z := int32(-8); z <= 8; z++ {
				col, ok := tx.World().loadedChunk(ChunkPos{x, z})
				if !ok {
					continue
				}
				for pos, actor := range col.BlockEntities {
					name, _ := actor.EncodeBlock()
					if name == "minecraft:end_portal" && !yield(pos) {
						return
					}
				}
			}
		}
	}
}
