package builtin

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// ReturnCommandHandler is implemented by a command source executing a function.
// ReturnCommand ends the current function with its command result and whether it
// succeeded. RunReturnCommand executes a nested command and reports its result.
// A regular command source should not implement this interface.
type ReturnCommandHandler interface {
	ReturnCommand(result int, success bool)
	RunReturnCommand(command string) (result int, success bool)
}

type returnValueCommand struct {
	opGate
	Value int `cmd:"value"`
}

type returnFailCommand struct {
	opGate
	Fail cmd.SubCommand `cmd:"fail"`
}

type returnRunCommand struct {
	opGate
	Subcommand cmd.SubCommand `cmd:"run"`
	Command    cmd.Varargs    `cmd:"command"`
}

func newReturnCommand() cmd.Command {
	return cmd.New(
		"return",
		"Returns from the current function with a command result.",
		nil,
		returnValueCommand{},
		returnFailCommand{},
		returnRunCommand{},
	)
}

func (r returnValueCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	h, ok := src.(ReturnCommandHandler)
	if !ok {
		o.Errorf("The return command can only be used while executing a function.")
		return
	}
	h.ReturnCommand(r.Value, true)
}

func (returnFailCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	h, ok := src.(ReturnCommandHandler)
	if !ok {
		o.Errorf("The return command can only be used while executing a function.")
		return
	}
	h.ReturnCommand(0, false)
}

func (r returnRunCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	h, ok := src.(ReturnCommandHandler)
	if !ok {
		o.Errorf("The return command can only be used while executing a function.")
		return
	}
	command := strings.TrimSpace(string(r.Command))
	if command == "" {
		o.Errorf("A command must be provided after 'return run'.")
		return
	}
	result, success := h.RunReturnCommand(command)
	h.ReturnCommand(result, success)
}
