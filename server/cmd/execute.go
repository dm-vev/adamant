package cmd

import (
	"strings"

	"github.com/df-mc/dragonfly/server/world"
)

// ExecuteLine executes a command line on behalf of the Source passed. The commandLine
// is expected to include the leading slash. If the command cannot be found, an
// appropriate error is sent back to the Source. The optional before function may
// be supplied to intercept execution; returning false from it will stop execution.
func ExecuteLine(source Source, commandLine string, tx *world.Tx, before func(Command, []string) bool) {
	if source == nil {
		panic("cmd.ExecuteLine: source must not be nil")
	}
	commandLine = strings.TrimSpace(commandLine)
	if commandLine == "" {
		return
	}
	args := strings.Split(commandLine, " ")
	if len(args) == 0 {
		return
	}
	name, ok := strings.CutPrefix(args[0], "/")
	if !ok || name == "" {
		return
	}

	command, ok := ByAlias(name)
	if !ok {
		output := &Output{}
		output.Errort(MessageUnknown, name)
		source.SendCommandOutput(output)
		return
	}
	if before != nil && !before(command, args[1:]) {
		return
	}
	command.Execute(strings.Join(args[1:], " "), source, tx)
}
