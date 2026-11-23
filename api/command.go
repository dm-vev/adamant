package api

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// CommandSpec declaratively defines a server command registered via RegisterCommand.
type CommandSpec struct {
	// Name is the primary lowercase command name (without leading slash).
	Name string
	// Description is shown in /help and command metadata.
	Description string
	// Aliases lists alternative names for the command.
	Aliases []string
	// Permission sets the minimum cmd.Level required to execute the command.
	Permission cmd.Level
	// Allow optionally filters whether src may execute the command.
	Allow func(src cmd.Source) bool
	// Run handles command execution with parsed space-separated args.
	Run func(src cmd.Source, args []string, tx *world.Tx)
	// RunOutput optionally handles execution when writing to *cmd.Output directly.
	RunOutput func(src cmd.Source, out *cmd.Output, args []string, tx *world.Tx)
}

type declarativeRunnable struct {
	Args cmd.Optional[cmd.Varargs] `cmd:"args"`
	spec CommandSpec
}

func (r declarativeRunnable) RequiredLevel() cmd.Level {
	return r.spec.Permission
}

func (r declarativeRunnable) Allow(src cmd.Source) bool {
	if r.spec.Allow != nil {
		return r.spec.Allow(src)
	}
	return true
}

func (r declarativeRunnable) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	var args []string
	if raw, ok := r.Args.Load(); ok {
		args = strings.Fields(string(raw))
	}
	if r.spec.RunOutput != nil {
		r.spec.RunOutput(src, o, args, tx)
	}
	if r.spec.Run != nil {
		r.spec.Run(src, args, tx)
	}
}

// RegisterCommand compiles spec into a cmd.Command and registers it with cmd.Register.
func RegisterCommand(spec CommandSpec) cmd.Command {
	if spec.Name == "" {
		panic("api: CommandSpec.Name must not be empty")
	}
	c := cmd.New(spec.Name, spec.Description, spec.Aliases, declarativeRunnable{spec: spec})
	cmd.Register(c)
	return c
}
