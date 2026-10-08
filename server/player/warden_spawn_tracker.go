package player

import "github.com/df-mc/dragonfly/server/world"

// WardenSpawnTracker returns the source-compatible Java warning tracker owned
// by this player. It is saved as Java-only player state, not native actor NBT.
func (p *Player) WardenSpawnTracker() *world.WardenSpawnTracker { return p.wardenSpawnTracker }

// WardenSpectator reports whether the player is excluded from nearby warning
// propagation because they are in spectator mode.
func (p *Player) WardenSpectator() bool { return p.GameMode() == world.GameModeSpectator }

func decodePlayerWardenTracker(state map[string]any) *world.WardenSpawnTracker {
	if raw, ok := state["warden_spawn_tracker"].(map[string]any); ok {
		return world.DecodeWardenSpawnTracker(raw)
	}
	return &world.WardenSpawnTracker{}
}

func encodePlayerJavaState(p *Player) map[string]any {
	state := world.CloneNativeEntityNBT(p.javaState)
	if state == nil {
		state = make(map[string]any)
	}
	state["warden_spawn_tracker"] = p.wardenSpawnTracker.EncodeNBT()
	return state
}
