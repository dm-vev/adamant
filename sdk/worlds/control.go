package worlds

import (
	"errors"
	"sync"
	"time"

	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/server/scoreboard"
	"github.com/dm-vev/adamant/server/world"
)

// ForceChunk keeps the chunk at pos loaded and ticking inside tx and returns an
// idempotent release function that unforces it. When ctx is non-nil, the chunk
// ticket is automatically released when the plugin disables.
func ForceChunk(ctx *plugin.Context, tx *world.Tx, pos world.ChunkPos) (func(), error) {
	if tx == nil || tx.Closed() {
		return func() {}, errors.New("sdk/worlds: nil or closed transaction")
	}
	if err := tx.ForceChunk(pos, true); err != nil {
		return func() {}, err
	}
	w := tx.World()
	var once sync.Once
	release := func() {
		once.Do(func() {
			if w != nil {
				w.Exec(func(releaseTx *world.Tx) {
					_ = releaseTx.ForceChunk(pos, false)
				})
			}
		})
	}
	if ctx != nil {
		ctx.OnDisable(release)
	}
	return release, nil
}

// ForcedChunks returns a snapshot of all currently force-loaded chunk positions in w.
func ForcedChunks(w *world.World) []world.ChunkPos {
	if w == nil {
		return nil
	}
	return w.ForcedChunks()
}

// IsChunkForced reports whether pos is currently force-loaded in w.
func IsChunkForced(w *world.World, pos world.ChunkPos) bool {
	return w != nil && w.IsChunkForced(pos)
}

// SetGameRule sets a Java/Bedrock game rule on w (e.g. "keep_inventory", "pvp",
// "mob_griefing", "do_daylight_cycle").
func SetGameRule(w *world.World, name string, value any) error {
	if w == nil {
		return errors.New("sdk/worlds: nil World")
	}
	return w.SetGameRule(name, value)
}

// GameRule returns the current value of game rule name on w.
func GameRule(w *world.World, name string) (any, bool) {
	if w == nil {
		return nil, false
	}
	return w.GameRule(name)
}

// GameRuleBool returns the boolean value of game rule name on w.
func GameRuleBool(w *world.World, name string) bool {
	return w != nil && w.GameRuleBool(name)
}

// GameRuleInt returns the integer value of game rule name on w.
func GameRuleInt(w *world.World, name string) int {
	if w == nil {
		return 0
	}
	return w.GameRuleInt(name)
}

// SetTime sets the time of day in ticks on w.
func SetTime(w *world.World, ticks int) {
	if w != nil {
		w.SetTime(ticks)
	}
}

// Time returns the current time of day in ticks on w.
func Time(w *world.World) int {
	if w == nil {
		return 0
	}
	return w.Time()
}

// StopTime freezes the daylight cycle on w.
func StopTime(w *world.World) {
	if w != nil {
		w.StopTime()
	}
}

// StartTime resumes the daylight cycle on w.
func StartTime(w *world.World) {
	if w != nil {
		w.StartTime()
	}
}

// StartRain starts rain in w for duration d.
func StartRain(w *world.World, d time.Duration) {
	if w != nil {
		w.StartRaining(d)
	}
}

// StopRain clears rain and thunder in w.
func StopRain(w *world.World) {
	if w != nil {
		w.StopRaining()
	}
}

// StartThunder starts a thunderstorm in w for duration d.
func StartThunder(w *world.World, d time.Duration) {
	if w != nil {
		w.StartThundering(d)
	}
}

// StopThunder stops thunder in w while leaving rain intact.
func StopThunder(w *world.World) {
	if w != nil {
		w.StopThundering()
	}
}

// SetDifficulty changes the difficulty of w.
func SetDifficulty(w *world.World, d world.Difficulty) {
	if w != nil {
		w.SetDifficulty(d)
	}
}

// Difficulty returns the current difficulty of w.
func Difficulty(w *world.World) world.Difficulty {
	if w == nil {
		return world.DifficultyNormal
	}
	return w.Difficulty()
}

// Scoreboard returns the world's persistent shared scoreboard model.
func Scoreboard(w *world.World) *scoreboard.Model {
	if w == nil {
		return nil
	}
	return w.Scoreboard()
}
