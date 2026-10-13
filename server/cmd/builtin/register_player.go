package builtin

import "github.com/df-mc/dragonfly/server/cmd"

// registerPlayerCommands registers Java 26.3 player and entity state commands.
func registerPlayerCommands(srv serverAdapter) {
	cmd.Register(newGiveCommand())
	registerPlayerEffectCommands(srv)
	registerPlayerKillCommands(srv)
	registerPlayerTeleportCommands(srv)
	registerPlayerExperienceCommands(srv)
	registerPlayerTagCommands(srv)
	registerPlayerTextCommands(srv)
	registerPlayerSoundCommands(srv)
}
