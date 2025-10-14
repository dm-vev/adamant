package server

import "github.com/df-mc/dragonfly/server/plugin"

type (
	Plugin          = plugin.Plugin
	VersionedPlugin = plugin.VersionedPlugin
	PluginFactory   = plugin.PluginFactory[*Server, Config]
	PluginInfo      = plugin.Info
	PluginAPI       = plugin.API[*Server, Config]
)

var (
	ErrPluginsDisabled     = plugin.ErrDisabled
	ErrPluginAlreadyLoaded = plugin.ErrAlreadyLoaded
	ErrPluginNameConflict  = plugin.ErrNameConflict
	ErrPluginNotFound      = plugin.ErrNotFound
)
