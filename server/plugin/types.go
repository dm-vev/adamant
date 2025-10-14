package plugin

import "errors"

// Plugin defines a dynamically loaded extension that can interact with the server.
type Plugin interface {
	// Name returns the display name of the plugin. It should be unique for the
	// lifetime of the server process.
	Name() string
	// Close releases all resources held by the plugin. It is called once when
	// the server shuts down or when the plugin is disabled.
	Close() error
}

// VersionedPlugin may be implemented by plugins to expose a version string.
type VersionedPlugin interface {
	Version() string
}

// PluginFactory is the expected constructor signature exposed by Go plugins. The
// returned Plugin is enabled immediately and must be ready to handle callbacks.
type PluginFactory[A any, C any] func(api *API[A, C]) (Plugin, error)

// Info describes a plugin currently loaded by the manager.
type Info struct {
	Name    string
	Version string
	Path    string
}

var (
	// ErrDisabled is returned when the plugin subsystem is disabled.
	ErrDisabled = errors.New("plugin subsystem disabled")
	// ErrAlreadyLoaded is returned when attempting to enable a plugin that has
	// already been loaded.
	ErrAlreadyLoaded = errors.New("plugin already loaded")
	// ErrNameConflict is returned when another loaded plugin already uses the
	// same case-insensitive name.
	ErrNameConflict = errors.New("plugin name already registered")
	// ErrNotFound is returned when attempting to disable or reload a plugin that
	// is not currently loaded.
	ErrNotFound = errors.New("plugin not found")
)
