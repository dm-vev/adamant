package builtin

import (
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type listCommand struct {
	srv serverAdapter
}

func newListCommand(srv serverAdapter) cmd.Command {
	return newCommand("list", []string{"players"}, listCommand{srv: srv})
}

// Run lists the players of every world from the server's list, which it
// reads without waiting for the owners of other worlds.
func (l listCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	online := l.srv.Online()
	names := make([]string, 0, len(online))
	for _, p := range online {
		names = append(names, p.Name)
	}
	slices.Sort(names)

	o.Printf("There are %d/%d players online.", len(names), l.srv.MaxPlayerCount())
	if len(names) != 0 {
		o.Print(strings.Join(names, ", "))
	}
}
