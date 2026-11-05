package world

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/google/uuid"
)

// currentTickLocked returns the tick used to update this world. The caller
// must hold the shared settings lock.
func (w *World) currentTickLocked() int64 {
	if w.advance {
		return w.set.CurrentTick
	}
	return w.tick
}

const (
	TimeDay      = 1000
	TimeNoon     = 6000
	TimeSunset   = 12000
	TimeNight    = 13000
	TimeMidnight = 18000
	TimeSunrise  = 23000
	TimeFull     = 24000
)

// CurrentTick returns the current tick counter of the world.
func (w *World) CurrentTick() int64 {
	if w == nil {
		return 0
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.currentTickLocked()
}

// TPS returns the current average ticks per second of the world. The value is
// averaged over the last tpsSampleSize ticks and may be zero if no samples have
// been recorded yet.
func (w *World) TPS() float64 {
	if w == nil {
		return 0
	}
	return math.Float64frombits(w.tps.Load())
}

// Time returns the current time of the world. The time is incremented every
// 1/20th of a second, unless World.StopTime() is called.
func (w *World) Time() int {
	if w == nil {
		return 0
	}
	w.set.Lock()
	defer w.set.Unlock()
	return int(w.set.Time)
}

// SetTime sets the new time of the world. SetTime will always work, regardless
// of whether the time is stopped or not.
func (w *World) SetTime(new int) {
	if w == nil {
		return
	}
	w.set.Lock()
	w.set.Time = int64(new)
	w.set.Unlock()

	viewers, _ := w.allViewers()
	for _, viewer := range viewers {
		w.view(viewer, func(v Viewer) { v.ViewTime(new) })
	}
	w.releaseViewers(viewers)
}

// StopTime stops the time in the world. When called, the time will no longer
// cycle and the world will remain at the time when StopTime is called. The
// time may be restarted by calling World.StartTime().
func (w *World) StopTime() {
	w.enableTimeCycle(false)
}

// StartTime restarts the time in the world. When called, the time will start
// cycling again and the day/night cycle will continue. The time may be stopped
// again by calling World.StopTime().
func (w *World) StartTime() {
	w.enableTimeCycle(true)
}

// TimeCycle returns whether time cycle is enabled.
func (w *World) TimeCycle() bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.TimeCycle
}

// enableTimeCycle enables or disables the time cycling of the World.
func (w *World) enableTimeCycle(v bool) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.TimeCycle = v
	viewers, _ := w.allViewers()
	defer w.releaseViewers(viewers)
	for _, viewer := range viewers {
		viewer.ViewTimeCycle(v)
	}
}

// PlayersSleepingPercentage returns the configured percentage of players required to sleep before the night is skipped.
func (w *World) PlayersSleepingPercentage() int32 {
	if w == nil {
		return 100
	}
	w.set.Lock()
	defer w.set.Unlock()
	if w.set.PlayersSleepingPercentage <= 0 {
		return 100
	}
	return w.set.PlayersSleepingPercentage
}

// Spawn returns the spawn of the world. Every new player will by default spawn
// on this position in the world when joining.
func (w *World) Spawn() cube.Pos {
	if w == nil {
		return cube.Pos{}
	}

	if w.Dimension() == End {
		return cube.Pos{100, 50}
	} else if w.Dimension() == Nether {
		return cube.Pos{}
	}

	w.set.Lock()
	defer w.set.Unlock()
	return w.set.Spawn
}

// SetSpawn sets the spawn of the world to a different position. The player
// will be spawned in the centre of this position when newly joining.
func (w *World) SetSpawn(pos cube.Pos) {
	if w == nil {
		return
	}

	// nether has no spawn point and end spawn point is always 100 50 0.
	if w.Dimension() == Nether || w.Dimension() == End {
		return
	}

	w.set.Lock()
	w.set.Spawn = pos
	w.set.Unlock()

	viewers, _ := w.allViewers()
	for _, viewer := range viewers {
		viewer.ViewWorldSpawn(pos)
	}
	w.releaseViewers(viewers)
}

// PlayerSpawn returns the spawn position of a player with a UUID in this World.
func (w *World) PlayerSpawn(id uuid.UUID) cube.Pos {
	if w == nil {
		return cube.Pos{}
	}
	pos, exist, err := w.conf.Provider.LoadPlayerSpawnPosition(id)
	if err != nil {
		w.log.Error("load player spawn: "+err.Error(), "ID", id)
		return w.Spawn()
	}
	if !exist {
		return w.Spawn()
	}
	return pos
}

// SetPlayerSpawn sets the spawn position of a player with a UUID in this
// World. If the player has a spawn in the world, the player will be teleported
// to this location on respawn. Unlike the other data saved beside the
// columns, it is written to the provider at once (Staging).
func (w *World) SetPlayerSpawn(id uuid.UUID, pos cube.Pos) {
	if w == nil {
		return
	}
	if err := w.conf.Provider.SavePlayerSpawnPosition(id, pos); err != nil {
		w.log.Error("save player spawn: "+err.Error(), "ID", id)
	}
}

// SetRequiredSleepDuration sets the duration of time players in the world must sleep for, in order to advance to the
// next day.
func (w *World) SetRequiredSleepDuration(duration time.Duration) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.RequiredSleepTicks = duration.Milliseconds() / 50
}

// DefaultGameMode returns the default game mode of the world. When players
// join, they are given this game mode. The default game mode may be changed
// using SetDefaultGameMode().
func (w *World) DefaultGameMode() GameMode {
	if w == nil {
		return GameModeSurvival
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.DefaultGameMode
}

// SetTickRange sets the range in chunks around each Viewer that will have the
// chunks (their blocks and entities) ticked when the World is ticked.
func (w *World) SetTickRange(v int) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.TickRange = int32(v)
}

// tickRange returns the tick range around each Viewer.
func (w *World) tickRange() int {
	w.set.Lock()
	defer w.set.Unlock()
	return int(w.set.TickRange)
}

// SetDefaultGameMode changes the default game mode of the world. When players
// join, they are then given that game mode.
func (w *World) SetDefaultGameMode(mode GameMode) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.DefaultGameMode = mode
}

// Difficulty returns the difficulty of the world. Properties of mobs in the
// world and the player's hunger will depend on this difficulty.
func (w *World) Difficulty() Difficulty {
	if w == nil {
		return DifficultyNormal
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.Difficulty
}

// SetDifficulty changes the difficulty of a world.
func (w *World) SetDifficulty(d Difficulty) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.Difficulty = d
}

// FallDamage reports whether entities in the World take damage from falling.
func (w *World) FallDamage() bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.FallDamage
}

// SetFallDamage sets whether entities in the World take damage from falling.
func (w *World) SetFallDamage(v bool) {
	if w == nil {
		return
	}
	w.set.Lock()
	if w.set.FallDamage == v {
		w.set.Unlock()
		return
	}
	w.set.FallDamage = v
	w.set.fallDamageVersion++
	if w.set.fallDamageDispatching {
		w.set.Unlock()
		return
	}
	w.set.fallDamageDispatching = true
	w.set.Unlock()
	w.dispatchFallDamage()
}

func (w *World) dispatchFallDamage() {
	for {
		w.set.Lock()
		version, value := w.set.fallDamageVersion, w.set.FallDamage
		worlds := make([]*World, 0, len(w.set.worlds))
		for shared := range w.set.worlds {
			worlds = append(worlds, shared)
		}
		w.set.Unlock()

		stale := false
		for _, shared := range worlds {
			viewers, _ := shared.allViewers()
			for _, viewer := range viewers {
				w.set.Lock()
				current := w.set.fallDamageVersion == version
				w.set.Unlock()
				if !current {
					stale = true
					break
				}
				if viewer, ok := viewer.(interface{ ViewFallDamage(bool) }); ok {
					viewer.ViewFallDamage(value)
				}
			}
			shared.releaseViewers(viewers)
			if stale {
				break
			}
		}

		w.set.Lock()
		if w.set.fallDamageVersion == version {
			w.set.fallDamageDispatching = false
			w.set.Unlock()
			return
		}
		w.set.Unlock()
	}
}

// PortalDestination returns the destination World for a portal of a specific
// Dimension. Calling PortalDestination(Nether) on an Overworld World returns
// Nether, while calling PortalDestination(Nether) on a Nether World will
// return the Overworld, for instance. If no destination World is available,
// nil is returned.
func (w *World) PortalDestination(dim Dimension) *World {
	if w == nil {
		return nil
	}
	if w.conf.PortalDestination == nil {
		return nil
	}
	dest := w.conf.PortalDestination(dim)
	if dest == w {
		if fallback := w.DefaultWorld(); fallback != w {
			return fallback
		}
		return nil
	}
	return dest
}

// PortalDisabledMessage resolves the message to display when a portal targeting the
// provided Dimension is disabled. An empty string suppresses any feedback.
func (w *World) PortalDisabledMessage(dim Dimension) string {
	if w == nil {
		return ""
	}
	if w.conf.PortalDisabledMessage == nil {
		return ""
	}
	return w.conf.PortalDisabledMessage(dim)
}

// DefaultWorld returns the primary world configured for this server. If no explicit default
// callback is provided, the world itself is returned so respawn logic always has a destination.
func (w *World) DefaultWorld() *World {
	if w == nil {
		return nil
	}
	if w.conf.DefaultWorld == nil {
		return w
	}
	if def := w.conf.DefaultWorld(); def != nil {
		return def
	}
	return w
}
