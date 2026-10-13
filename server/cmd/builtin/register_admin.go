package builtin

import "github.com/df-mc/dragonfly/server/cmd"

// registerAdminCommands registers Java 26.3 operator, moderation and server lifecycle commands.
func registerAdminCommands(srv serverAdapter) {
	cmd.Register(newOpCommand(srv))
	cmd.Register(newDeopCommand(srv))
	cmd.Register(newBanPlayerCommand(srv))
	cmd.Register(newBanIPCommand(srv))
	cmd.Register(newPardonCommand(srv))
	cmd.Register(newPardonIPCommand(srv))
	cmd.Register(newBanListCommand(srv))
	cmd.Register(newSaveAllCommand(srv))
	cmd.Register(newSaveOffCommand(srv))
	cmd.Register(newSaveOnCommand(srv))
	cmd.Register(newIdleTimeoutCommand(srv))
	cmd.Register(newTransferCommand(srv))
	cmd.Register(newVersionCommand(srv))
}
