package world

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// WardenSpawnTracker stores the Java-only per-player warning state used by
// sculk shriekers. It is deliberately separate from native Bedrock actor NBT.
type WardenSpawnTracker struct {
	TicksSinceLastWarning int
	WarningLevel          int
	CooldownTicks         int
}

// Tick advances the tracker once for each player tick, decaying one warning
// after 12,000 warning-free ticks and reducing the cooldown by one tick.
func (t *WardenSpawnTracker) Tick() {
	if t.TicksSinceLastWarning >= 12000 {
		t.SetWarningLevel(t.WarningLevel - 1)
		t.TicksSinceLastWarning = 0
	} else if t.TicksSinceLastWarning < math.MaxInt {
		t.TicksSinceLastWarning++
	}
	if t.CooldownTicks > 0 {
		t.CooldownTicks--
	}
}

// SetWarningLevel clamps a warning level to Java's [0,4] range.
func (t *WardenSpawnTracker) SetWarningLevel(level int) { t.WarningLevel = max(0, min(4, level)) }

// EncodeNBT returns the Java WardenSpawnTracker codec fields.
func (t *WardenSpawnTracker) EncodeNBT() map[string]any {
	return map[string]any{
		"ticks_since_last_warning": int32(max(0, t.TicksSinceLastWarning)),
		"warning_level":            int32(max(0, t.WarningLevel)),
		"cooldown_ticks":           int32(max(0, t.CooldownTicks)),
	}
}

// DecodeWardenSpawnTracker reads the Java tracker codec, whose absent fields
// default to zero.
func DecodeWardenSpawnTracker(data map[string]any) *WardenSpawnTracker {
	return &WardenSpawnTracker{
		TicksSinceLastWarning: max(0, intNBT(data["ticks_since_last_warning"])),
		WarningLevel:          max(0, min(4, intNBT(data["warning_level"]))),
		CooldownTicks:         max(0, intNBT(data["cooldown_ticks"])),
	}
}

func intNBT(v any) int {
	switch n := v.(type) {
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		if n > int64(math.MaxInt) {
			return math.MaxInt
		}
		if n < int64(math.MinInt) {
			return math.MinInt
		}
		return int(n)
	case int:
		return n
	case float64:
		if math.IsNaN(n) {
			return 0
		}
		if n >= float64(math.MaxInt) {
			return math.MaxInt
		}
		if n <= float64(math.MinInt) {
			return math.MinInt
		}
		return int(n)
	default:
		return 0
	}
}

// TryWarnWarden applies Java's shared player warning logic. trigger must expose
// its player-owned tracker; nearby players are discovered structurally to avoid
// a world->player package dependency.
func (tx *Tx) TryWarnWarden(pos cube.Pos, trigger Entity) (int, bool) {
	if trigger == nil || tx.hasNearbyWarden(pos) {
		return 0, false
	}
	triggerTracker, ok := wardenTrackerOf(trigger)
	if !ok {
		return 0, false
	}
	origin := pos.Vec3Centre()
	players := make([]wardenTrackedPlayer, 0, 4)
	seenTrigger := false
	for e := range tx.EntitiesWithin(cube.Box(-16, -16, -16, 16, 16, 16).Translate(origin)) {
		candidate, ok := asWardenPlayer(e)
		if !ok || candidate.Dead() || candidate.WardenSpectator() || candidate.Position().Sub(origin).Len() >= 16 {
			continue
		}
		tracker, ok := wardenTrackerOf(e)
		if !ok {
			continue
		}
		players = append(players, wardenTrackedPlayer{tracker})
		if e.H() == trigger.H() {
			seenTrigger = true
		}
	}
	if !seenTrigger {
		players = append(players, wardenTrackedPlayer{triggerTracker})
	}
	for _, p := range players {
		if p.tracker.CooldownTicks > 0 {
			return 0, false
		}
	}
	best := triggerTracker
	for _, p := range players {
		if p.tracker.WarningLevel > best.WarningLevel {
			best = p.tracker
		}
	}
	best.TicksSinceLastWarning = 0
	best.CooldownTicks = 200
	best.SetWarningLevel(best.WarningLevel + 1)
	for _, p := range players {
		p.tracker.TicksSinceLastWarning = best.TicksSinceLastWarning
		p.tracker.WarningLevel = best.WarningLevel
		p.tracker.CooldownTicks = best.CooldownTicks
	}
	return best.WarningLevel, true
}

type wardenPlayer interface {
	Entity
	Dead() bool
	WardenSpectator() bool
}
type wardenTrackerProvider interface{ WardenSpawnTracker() *WardenSpawnTracker }
type wardenTrackedPlayer struct{ tracker *WardenSpawnTracker }

func wardenTrackerOf(e Entity) (*WardenSpawnTracker, bool) {
	p, ok := e.(wardenTrackerProvider)
	if !ok {
		return nil, false
	}
	tracker := p.WardenSpawnTracker()
	return tracker, tracker != nil
}
func asWardenPlayer(e Entity) (wardenPlayer, bool) {
	p, ok := e.(wardenPlayer)
	return p, ok
}
func (tx *Tx) hasNearbyWarden(pos cube.Pos) bool {
	origin := pos.Vec3Centre()
	for e := range tx.EntitiesWithin(cube.Box(-24, -24, -24, 24, 24, 24).Translate(origin)) {
		if e.H().Type().EncodeEntity() == "minecraft:warden" {
			return true
		}
	}
	return false
}
