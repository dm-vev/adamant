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
	return cmd.New("list", "Lists players currently online.", []string{"players"}, listCommand{srv: srv})
}

func (l listCommand) Run(_ cmd.Source, o *cmd.Output, tx *world.Tx) {
	names := make([]string, 0)
	for p := range l.srv.Players(tx) {
		names = append(names, p.Name())
	}
	slices.Sort(names)

	o.Printf("There are %d/%d players online.", len(names), l.srv.MaxPlayerCount())
	if len(names) != 0 {
		o.Print(strings.Join(names, ", "))
	}
}
