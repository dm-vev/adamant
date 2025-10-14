package builtin

import (
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

type pluginListCommand struct {
	List cmd.SubCommand `cmd:"list"`
	srv  serverAdapter
}

type pluginEnableCommand struct {
	Enable cmd.SubCommand `cmd:"enable"`
	File   string         `cmd:"file"`
	srv    serverAdapter
}

type pluginDisableCommand struct {
	Disable cmd.SubCommand `cmd:"disable"`
	Name    string         `cmd:"name"`
	srv     serverAdapter
}

type pluginReloadCommand struct {
	Reload cmd.SubCommand `cmd:"reload"`
	Name   string         `cmd:"name"`
	srv    serverAdapter
}

func newPluginCommand(srv serverAdapter) cmd.Command {
	return cmd.New(
		"plugin",
		"Manages dynamic plugins.",
		nil,
		pluginListCommand{srv: srv},
		pluginEnableCommand{srv: srv},
		pluginDisableCommand{srv: srv},
		pluginReloadCommand{srv: srv},
	)
}

func (p pluginListCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if !p.srv.PluginsEnabled() {
		o.Print("Plugin subsystem disabled.")
		return
	}
	plugins := append([]server.PluginInfo(nil), p.srv.Plugins()...)
	if len(plugins) == 0 {
		o.Print("No plugins loaded.")
		return
	}
	sort.SliceStable(plugins, func(i, j int) bool {
		return strings.ToLower(plugins[i].Name) < strings.ToLower(plugins[j].Name)
	})
	for _, info := range plugins {
		if info.Version != "" {
			o.Printf("%s v%s (%s)", info.Name, info.Version, info.Path)
			continue
		}
		o.Printf("%s (%s)", info.Name, info.Path)
	}
}

func (pluginListCommand) Allow(src cmd.Source) bool {
	_, isPlayer := src.(*player.Player)
	return !isPlayer
}

func (p pluginEnableCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if !p.srv.PluginsEnabled() {
		o.Error("Plugin subsystem disabled.")
		return
	}
	file := strings.TrimSpace(p.File)
	if file == "" {
		o.Error("Plugin file path is required.")
		return
	}
	info, err := p.srv.EnablePlugin(file)
	if err != nil {
		o.Error(err)
		return
	}
	if info.Version != "" {
		o.Printf("Enabled %s v%s from %s.", info.Name, info.Version, info.Path)
		return
	}
	o.Printf("Enabled %s from %s.", info.Name, info.Path)
}

func (pluginEnableCommand) Allow(src cmd.Source) bool {
	_, isPlayer := src.(*player.Player)
	return !isPlayer
}

func (p pluginDisableCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if !p.srv.PluginsEnabled() {
		o.Error("Plugin subsystem disabled.")
		return
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		o.Error("Plugin name is required.")
		return
	}
	info, err := p.srv.DisablePlugin(name)
	if err != nil {
		o.Error(err)
		return
	}
	o.Printf("Disabled %s.", info.Name)
}

func (pluginDisableCommand) Allow(src cmd.Source) bool {
	_, isPlayer := src.(*player.Player)
	return !isPlayer
}

func (p pluginReloadCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if !p.srv.PluginsEnabled() {
		o.Error("Plugin subsystem disabled.")
		return
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		o.Error("Plugin name is required.")
		return
	}
	info, err := p.srv.ReloadPlugin(name)
	if err != nil {
		o.Error(err)
		return
	}
	if info.Version != "" {
		o.Printf("Reloaded %s v%s.", info.Name, info.Version)
		return
	}
	o.Printf("Reloaded %s.", info.Name)
}

func (pluginReloadCommand) Allow(src cmd.Source) bool {
	_, isPlayer := src.(*player.Player)
	return !isPlayer
}
