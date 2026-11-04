package builtin

import (
	"errors"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

type whitelistAddCommand struct {
	adminPermission
	srv  serverAdapter
	Add  cmd.SubCommand `cmd:"add"`
	Name string         `cmd:"player"`
}

type whitelistOnCommand struct {
	adminPermission
	srv serverAdapter
	On  cmd.SubCommand `cmd:"on"`
}

type whitelistOffCommand struct {
	adminPermission
	srv serverAdapter
	Off cmd.SubCommand `cmd:"off"`
}

type whitelistReloadCommand struct {
	adminPermission
	srv    serverAdapter
	Reload cmd.SubCommand `cmd:"reload"`
}

type whitelistRemoveCommand struct {
	adminPermission
	srv    serverAdapter
	Remove cmd.SubCommand `cmd:"remove"`
	Name   string         `cmd:"player"`
}

type whitelistListCommand struct {
	adminPermission
	srv  serverAdapter
	List cmd.SubCommand `cmd:"list"`
}

func newWhitelistCommand(srv serverAdapter) cmd.Command {
	return newCommand(
		"whitelist",
		[]string{"allowlist"},
		whitelistAddCommand{adminPermission: adminPermission{srv}, srv: srv},
		whitelistOnCommand{adminPermission: adminPermission{srv}, srv: srv},
		whitelistOffCommand{adminPermission: adminPermission{srv}, srv: srv},
		whitelistReloadCommand{adminPermission: adminPermission{srv}, srv: srv},
		whitelistRemoveCommand{adminPermission: adminPermission{srv}, srv: srv},
		whitelistListCommand{adminPermission: adminPermission{srv}, srv: srv},
	)
}

func (c whitelistAddCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		o.Errort(cmd.MessageParameterInvalid, c.Name)
		return
	}
	t, ok := addTarget(c.srv, o, name)
	if !ok {
		return
	}
	added, err := c.srv.WhitelistAdd(t.Arg())
	if err != nil {
		if errors.Is(err, server.ErrWhitelistInvalidName) {
			o.Errort(cmd.MessageParameterInvalid, name)
			return
		}
		o.Error(err)
		return
	}
	switch {
	case !added:
		o.Printf("%s is already on the whitelist.", t)
	case t.Pending:
		o.Printf(pendingAdded, t.Name, t.Name, "is on the whitelist")
	default:
		o.Printf("Added %s to the whitelist.", t)
	}
}

func (c whitelistOnCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if err := c.srv.WhitelistSetEnabled(true); err != nil {
		o.Error(err)
		return
	}
	o.Print("Whitelist enabled.")
}

func (c whitelistOffCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if err := c.srv.WhitelistSetEnabled(false); err != nil {
		o.Error(err)
		return
	}
	o.Print("Whitelist disabled.")
}

func (c whitelistReloadCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if err := c.srv.WhitelistReload(); err != nil {
		o.Error(err)
		return
	}
	o.Print("Whitelist reloaded.")
}

func (c whitelistRemoveCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	name := strings.TrimSpace(c.Name)
	if name == "" {
		o.Errort(cmd.MessageParameterInvalid, c.Name)
		return
	}
	t, ok := removeTarget(c.srv, o, server.Whitelisted, name)
	if !ok {
		return
	}
	removed, err := c.srv.WhitelistRemove(t.Arg())
	if err != nil {
		if errors.Is(err, server.ErrWhitelistInvalidName) {
			o.Errort(cmd.MessageParameterInvalid, name)
			return
		}
		o.Error(err)
		return
	}
	if removed {
		o.Printf("Removed %s from the whitelist.", t)
		return
	}
	o.Printf("%s is not on the whitelist.", t)
}

func (c whitelistListCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	entries, err := c.srv.WhitelistEntries()
	if err != nil {
		o.Error(err)
		return
	}
	o.Printf("Whitelist: %d player(s).", len(entries))
	if len(entries) != 0 {
		o.Print(strings.Join(entries, ", "))
	}
}
