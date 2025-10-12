package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
)

// Register registers the built-in command set on the provided server.
func Register(srv serverAdapter) {
	cmd.Register(newAboutCommand(srv))
	cmd.Register(newHelpCommand())
	cmd.Register(newListCommand(srv))
	cmd.Register(newStatusCommand(srv))
	cmd.Register(newSayCommand())
	cmd.Register(newMeCommand())
	cmd.Register(newStopCommand(srv))
	cmd.Register(newKickCommand())
	cmd.Register(newGamemodeCommand())
	cmd.Register(newTimeCommand())
	cmd.Register(newGCCommand(srv))
}
