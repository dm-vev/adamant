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
	return cmd.New(
		"whitelist",
		"Manages the whitelist.",
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
	o.Printf("Whitelist: %d player(s).", len(entries))
	if len(entries) != 0 {
		o.Print(strings.Join(entries, ", "))
	}
}
