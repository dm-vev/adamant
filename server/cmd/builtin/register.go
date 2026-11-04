package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
)

// commandObserver is a server that records the commands run, as the audit
// log does, and shows their feedback to operators.
type commandObserver interface {
	ObserveCommand(e cmd.Execution)
	AdminFeedback(src cmd.Source, name, message string) bool
}

// Register registers the built-in command set on the provided server.
func Register(srv serverAdapter) {
	commandServer = srv
	cmd.SetOperatorChecker(srv.PlayerIsOperator)
	cmd.SetPermissions(srv)
	if o, ok := srv.(commandObserver); ok {
		cmd.SetObserver(o.ObserveCommand)
		cmd.SetAdminFeedback(o.AdminFeedback)
	}
	cmd.Register(newAboutCommand(srv))
	cmd.Register(newHelpCommand())
	cmd.Register(newListCommand(srv))
	cmd.Register(newStatusCommand(srv))
	cmd.Register(newSayCommand())
	cmd.Register(newMeCommand())
	cmd.Register(newStopCommand(srv))
	cmd.Register(newKickCommand())
	cmd.Register(newNetstatCommand())
	cmd.Register(newGamemodeCommand())
	cmd.Register(newTimeCommand())
	cmd.Register(newGCCommand(srv))
	cmd.Register(newWhitelistCommand(srv))
	cmd.Register(newClearCommand())
	registerPlayerCommands(srv)
	registerWorldCommands(srv)
	registerAdminCommands(srv)
	registerScoreboardCommands(srv)
	registerDataCommands(srv)
}
