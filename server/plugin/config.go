package plugin

// Config controls the behaviour of the dynamic plugin loader.
type Config struct {
	// Enabled specifies if the plugin subsystem should be initialised. When
	// false, no plugins will be discovered or enabled.
	Enabled bool
	// Directory is the base directory used when automatically discovering
	// plugins or when resolving relative file paths in Files.
	Directory string
	// DataDirectory controls where plugin data folders should be created. If
	// empty, a `data` directory inside Directory will be used. Relative
	// paths are resolved against Directory.
	DataDirectory string
	// Autoload controls whether every .so file in Directory should be probed
	// and loaded automatically.
	Autoload bool
	// Files enumerates additional plugin files to load. Entries without an
	// absolute path are resolved relative to Directory.
	Files []string
}
