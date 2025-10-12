package builtin

import (
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type helpCommand struct {
	Command cmd.Optional[string] `cmd:"command"`
}

func newHelpCommand() cmd.Command {
	return cmd.New("help", "Shows available commands and their usage.", []string{"?"}, helpCommand{})
}

func (h helpCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	if commandName, ok := h.Command.Load(); ok {
		name := strings.ToLower(strings.TrimPrefix(commandName, "/"))
		command, found := cmd.ByAlias(name)
		if !found || len(command.Runnables(src)) == 0 {
			o.Errort(cmd.MessageUnknown, name)
			return
		}
		if desc := command.Description(); desc != "" {
			o.Print(desc)
		}
		for _, line := range strings.Split(command.Usage(), "\n") {
			o.Print(line)
		}
		return
	}

	commands := cmd.Commands()
	names := make([]string, 0, len(commands))
	for alias, command := range commands {
		if command.Name() != alias {
			continue
		}
		if len(command.Runnables(src)) == 0 {
			continue
		}
		names = append(names, alias)
	}
	if len(names) == 0 {
		o.Print("No commands available.")
		return
	}
	sort.Strings(names)

	o.Printf("Available commands (%d):", len(names))
	for _, name := range names {
		command, _ := cmd.ByAlias(name)
		line := "/" + name
		if desc := command.Description(); desc != "" {
			line += " - " + desc
		}
		o.Print(line)
	}
}
