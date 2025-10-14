package builtin

import (
	"errors"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type whitelistAddCommand struct {
	srv  serverAdapter
	Add  cmd.SubCommand `cmd:"add"`
	Name string         `cmd:"player"`
}

type whitelistRemoveCommand struct {
	srv    serverAdapter
	Remove cmd.SubCommand `cmd:"remove"`
	Name   string         `cmd:"player"`
}

type whitelistListCommand struct {
	srv  serverAdapter
	List cmd.SubCommand `cmd:"list"`
}

func newWhitelistCommand(srv serverAdapter) cmd.Command {
	return cmd.New(
		"whitelist",
		"Manages the whitelist.",
		nil,
		whitelistAddCommand{srv: srv},
		whitelistRemoveCommand{srv: srv},
		whitelistListCommand{srv: srv},
	)
}

func (c whitelistAddCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		o.Errort(cmd.MessageParameterInvalid, c.Name)
		return
	}
	added, err := c.srv.WhitelistAdd(name)
	if err != nil {
		if errors.Is(err, server.ErrWhitelistInvalidName) {
			o.Errort(cmd.MessageParameterInvalid, name)
			return
		}
		o.Error(err)
		return
	}
	if added {
		o.Printf("Added %s to the whitelist.", name)
		return
	}
	o.Printf("%s is already on the whitelist.", name)
}

func (c whitelistRemoveCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		o.Errort(cmd.MessageParameterInvalid, c.Name)
		return
	}
	removed, err := c.srv.WhitelistRemove(name)
	if err != nil {
		if errors.Is(err, server.ErrWhitelistInvalidName) {
			o.Errort(cmd.MessageParameterInvalid, name)
			return
		}
		o.Error(err)
		return
	}
	if removed {
		o.Printf("Removed %s from the whitelist.", name)
		return
	}
	o.Printf("%s is not on the whitelist.", name)
}

func (c whitelistListCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	entries, err := c.srv.WhitelistEntries()
	if err != nil {
		o.Error(err)
		return
	}
	status := "enabled"
	if !c.srv.WhitelistEnabled() {
		status = "disabled"
	}
	o.Printf("Whitelist (%s): %d player(s).", status, len(entries))
	if len(entries) != 0 {
		o.Print(strings.Join(entries, ", "))
	}
}
