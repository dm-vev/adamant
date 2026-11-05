package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/java"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/config"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/logging"
	"github.com/df-mc/dragonfly/server/monitor"
	"github.com/df-mc/dragonfly/server/remoteadmin"
	"github.com/pelletier/go-toml"
)

// fileConfig is config.toml: the settings of the server and of each
// protocol adapter. Each part reads the keys it owns from the same file, and
// two parts may own keys of the same table. A part with a Check method, as
// server.UserConfig.Check, has the values of its keys checked by it as the
// file is read: see package server/config.
type fileConfig struct {
	Server  server.UserConfig
	Bedrock bedrockadapter.UserConfig
	Java    javaadapter.UserConfig
	Program programConfig
	Log     logConfig
	Monitor monitor.UserConfig
	// RemoteAdmin is the [RemoteAdmin] table: the remote admin API, which
	// listens apart from Monitor, of other trust.
	RemoteAdmin remoteadmin.UserConfig

	// warnings are the problems of the file the server starts with, which
	// compose logs.
	warnings []config.Problem
}

// programConfig is the part of config.toml the program owns: how strictly
// it reads the file.
type programConfig struct {
	Server struct {
		// StrictConfig refuses a file with a key or a table that no part
		// reads, or with a value the server falls back from, rather than
		// warn of it and start.
		StrictConfig bool
	}
}

// logConfig is the part of config.toml the program itself owns about its
// logs: the LogLevel key of [Server], the level of every component, and the
// [Log] table, of the format of the logs and of the levels of components
// that differ.
type logConfig struct {
	Server struct {
		// LogLevel is the least level of the messages logged: "debug",
		// "info", "warn" or "error", in any case. An empty value is
		// "info".
		LogLevel string
	}
	Log struct {
		// Format is how logs are written to stderr: "text", the lines the
		// server always wrote, or "json", one object per line with a
		// "component" field. An empty value is "text".
		Format string
		// RateLimit is the lines a second each component may log, in
		// bursts of ten times as many: the lines past it are dropped,
		// counted and summed up. 0 drops none.
		RateLimit int
		// Levels holds the level of each component that logs at a level
		// other than LogLevel; an empty value is LogLevel.
		Levels logLevels
	}
}

// logLevels holds a level for each component of the logs of the server,
// as package server/logging names them.
type logLevels struct {
	AntiCheat, Commands, Generator, Network, Player, Save, Server, Session, World string
}

// byComponent returns the levels of logLevels by the components of
// package server/logging, with the key of each in the file.
func (l logLevels) byComponent() []struct{ component, key, value string } {
	return []struct{ component, key, value string }{
		{logging.AntiCheat, "AntiCheat", l.AntiCheat}, {logging.Commands, "Commands", l.Commands},
		{logging.Generator, "Generator", l.Generator}, {logging.Network, "Network", l.Network},
		{logging.Player, "Player", l.Player}, {logging.Save, "Save", l.Save},
		{logging.Server, "Server", l.Server}, {logging.Session, "Session", l.Session},
		{logging.World, "World", l.World},
	}
}

// Check adds the problems of the levels and of the format the program does
// not know to p.
func (c logConfig) Check(p *config.Problems) {
	_, err := c.level()
	p.Add("Server.LogLevel", err)
	config.OneOf(p, "Log.Format", c.Log.Format, "text", "json")
	config.AtLeast(p, "Log.RateLimit", c.Log.RateLimit, 0)
	for _, l := range c.Log.Levels.byComponent() {
		if _, ok := parseLevel(l.value); !ok && strings.TrimSpace(l.value) != "" {
			p.Errorf("Log.Levels."+l.key, `Log.Levels.%s: unknown level %q: use "debug", "info", "warn" or "error"`, l.key, strings.TrimSpace(l.value))
		}
	}
}

// level returns the slog.Level of LogLevel.
func (c logConfig) level() (slog.Level, error) {
	s := strings.TrimSpace(c.Server.LogLevel)
	if s == "" {
		return slog.LevelInfo, nil
	}
	if l, ok := parseLevel(s); ok {
		return l, nil
	}
	return 0, fmt.Errorf(`unknown LogLevel %q: use "debug", "info", "warn" or "error"`, s)
}

// parseLevel returns the level a value of config.toml names: "debug",
// "info", "warn" (or "warning") or "error", in any case, with spaces
// around.
func parseLevel(s string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return 0, false
}

// options returns the options of the Set of loggers that logConfig
// configures, without its output.
func (c logConfig) options() (logging.Options, error) {
	root, err := c.level()
	if err != nil {
		return logging.Options{}, err
	}
	o := logging.Options{Level: root, Levels: map[string]slog.Level{}, RateLimit: c.Log.RateLimit}
	if strings.EqualFold(strings.TrimSpace(c.Log.Format), "json") {
		o.Format = logging.JSON
	}
	for _, l := range c.Log.Levels.byComponent() {
		if v, ok := parseLevel(l.value); ok {
			o.Levels[l.component] = v
		}
	}
	return o, nil
}

// setupLogs makes the Set of loggers that logConfig configures, writing as
// out says and counting the lines it drops through out.Dropped, and makes the default logger of slog log as it: at LogLevel in
// text, through the default handler of slog, which the Set writes as; in
// JSON, through the Set itself, so that log.Print and the code that logs
// through slog.Default() write JSON too, the lines of log.Print at info.
func setupLogs(c logConfig, out logging.Options) (*logging.Set, error) {
	o, err := c.options()
	if err != nil {
		return nil, err
	}
	o.Output, o.Logger, o.Dropped = out.Output, out.Logger, out.Dropped
	set := logging.New(o)
	if set.Format() == logging.JSON {
		// A Set of text must never be the default: it writes through the
		// default logger of log, which would write through it.
		slog.SetLogLoggerLevel(slog.LevelInfo)
		slog.SetDefault(set.Logger("default"))
		return set, nil
	}
	slog.SetLogLoggerLevel(o.Level)
	return set, nil
}

// defaultFileConfig returns the file of the default settings of every part.
func defaultFileConfig() fileConfig {
	f := fileConfig{Server: server.DefaultConfig(), Bedrock: bedrockadapter.DefaultConfig(), Java: javaadapter.DefaultConfig()}
	f.Log.Server.LogLevel = "info"
	f.Log.Log.Format, f.Log.Log.RateLimit = "text", 100
	f.Monitor = monitor.DefaultConfig()
	f.RemoteAdmin = remoteadmin.DefaultConfig()
	return f
}

// parts returns pointers to the parts of the file, in the order they are
// read and composed.
func (f *fileConfig) parts() []any {
	return []any{&f.Server, &f.Bedrock, &f.Java, &f.Program, &f.Log, &f.Monitor, &f.RemoteAdmin}
}

// process is what config.toml says of the process beside the server: its
// logs, what it serves to be watched, and the remote admin API, whose
// relative files are in adminDir, beside ops.json.
type process struct {
	logs     *logging.Set
	monitor  monitor.UserConfig
	remote   remoteadmin.UserConfig
	adminDir string
}

// readConfig reads config.toml, or writes the default file if there is none,
// sets up the logs as it says, writing as out says, before anything is
// logged of the file, and composes the server's Config with the listeners
// of the adapters, logging through the logger of the server component.
func readConfig(out logging.Options) (server.Config, process, error) {
	f, err := readConfigFile("config.toml")
	if err != nil {
		return server.Config{}, process{}, err
	}
	set, err := setupLogs(f.Log, out)
	if err != nil {
		return server.Config{}, process{}, err
	}
	conf, err := f.config(set.Logger(logging.Server))
	return conf, process{logs: set, monitor: f.Monitor, remote: f.RemoteAdmin, adminDir: f.adminDir()}, err
}

// readConfigFile reads the file of the path passed into the parts of a
// fileConfig, each filled out with its defaults first, so that a key the
// file lacks keeps its default. If there is no file, it writes the default
// one.
//
// It checks the file before anything is made of it, and returns a
// *config.Error of all its problems at once: the values a part cannot
// decode, those its Check refuses, and, with StrictConfig, the warnings.
// The warnings are of the keys and tables no part reads, and of the values a
// part falls back from; without StrictConfig, compose logs them.
func readConfigFile(path string) (fileConfig, error) {
	f := defaultFileConfig()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		data, err := marshalConfig(f.parts()...)
		if err != nil {
			return f, fmt.Errorf("encode default config: %v", err)
		}
		if err := os.WriteFile(path, data, 0644); err != nil {
			return f, fmt.Errorf("create default config: %v", err)
		}
		return f, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return f, fmt.Errorf("read config: %v", err)
	}
	tree, err := toml.LoadBytes(data)
	if err != nil {
		return f, fmt.Errorf("decode config: %v", err)
	}
	var problems config.Problems
	parts := f.parts()
	decoded := map[any]bool{}
	for _, p := range parts {
		if err := tree.Unmarshal(p); err != nil {
			problems.AddProblem(config.Problem{Message: fmt.Sprintf("decode config: %v", err)})
			continue
		}
		decoded[p] = true
	}
	keys := scanKeys(tree, parts)
	for _, pr := range keys.unknown {
		problems.AddProblem(pr)
	}
	// The values of a part that failed to decode are not of the file: they
	// are left unchecked.
	for _, p := range parts {
		if c, ok := p.(interface{ Check(*config.Problems) }); ok && decoded[p] {
			c.Check(&problems)
		}
	}
	if decoded[&f.Server] && decoded[&f.Bedrock] {
		f.check(&problems)
	}
	problems.Locate(func(key string) int { return keys.lines[key] })
	strict := decoded[&f.Program] && f.Program.Server.StrictConfig
	if err := problems.Err(strict); err != nil {
		err.(*config.Error).File = path
		return f, err
	}
	for _, pr := range problems.List() {
		if pr.Warning {
			f.warnings = append(f.warnings, pr)
		}
	}
	return f, nil
}

// check adds the problems of keys of the parts that depend on each other.
func (f fileConfig) check(p *config.Problems) {
	if mode, err := identity.ParseMode(f.Server.Server.IdentityKey); err == nil && mode == identity.XUID && !f.Bedrock.Server.AuthEnabled {
		p.Errorf("Server.IdentityKey", "%v", errIdentityXUIDOffline)
	}
	f.checkTokenFile(p)
}

// adminDir returns the folder of the admin lists, beside the whitelist,
// which the relative files of the remote admin API are in.
func (f fileConfig) adminDir() string { return filepath.Dir(f.Server.Whitelist.File) }

// checkTokenFile checks the file of the tokens of the remote admin API,
// beside ops.json, if the API is enabled: a file the server may write, or
// make, and that no one but its owner may read, which is warned of, and
// refused with StrictConfig.
func (f fileConfig) checkTokenFile(p *config.Problems) {
	if !f.RemoteAdmin.RemoteAdmin.Enabled || strings.TrimSpace(f.RemoteAdmin.RemoteAdmin.TokenFile) == "" {
		return
	}
	path := f.RemoteAdmin.TokenPath(f.adminDir())
	if !config.File(p, "RemoteAdmin.TokenFile", path) {
		return
	}
	if open, err := remoteadmin.TokenFileTooOpen(path); err == nil && open {
		p.Warnf("RemoteAdmin.TokenFile", "RemoteAdmin.TokenFile %s may be read by others than its owner: chmod 600 it", path)
	}
}

// config composes the server's Config of the parts of the file, with the
// listeners of the adapters: the NetherNet listener, and the listener for
// clients of Java Edition if it has an address.
func (f fileConfig) config(log *slog.Logger) (server.Config, error) {
	conf, _, _, err := f.compose(log)
	return conf, err
}

// compose returns the server's Config of the parts of the file, with the
// listeners of the adapters, and the Configs of the adapters it was
// composed with.
func (f fileConfig) compose(log *slog.Logger) (server.Config, bedrockadapter.Config, javaadapter.Config, error) {
	serverLog := logging.Sub(log, logging.Server)
	for _, w := range f.warnings {
		serverLog.Warn("config.toml: " + w.String())
	}
	if _, err := f.Log.level(); err != nil {
		return server.Config{}, bedrockadapter.Config{}, javaadapter.Config{}, err
	}
	// The adapters' parts come first: a bad Transport is reported before the
	// world is opened, as UserConfig.Config reported it before.
	bc, err := f.Bedrock.Config(log)
	if err != nil {
		return server.Config{}, bc, javaadapter.Config{}, err
	}
	jc := f.Java.Config()
	key, err := identityKey(f.Server.Server.IdentityKey, !bc.AuthDisabled, serverLog)
	if err != nil {
		return server.Config{}, bc, jc, err
	}
	f.Server.Server.IdentityKey = key.String()
	conf, err := f.Server.Config(log)
	if err != nil {
		return conf, bc, jc, err
	}
	conf.StrictConfig = f.Program.Server.StrictConfig
	conf.Listeners = append(conf.Listeners, bc.Listener)
	if jc.Address != "" {
		conf.Listeners = append(conf.Listeners, jc.Listener)
	}
	return conf, bc, jc, nil
}

// errIdentityXUIDOffline is the error of IdentityKey "xuid" without Xbox
// Live sign-in.
var errIdentityXUIDOffline = errors.New(`IdentityKey "xuid" needs AuthEnabled = true: without Xbox Live sign-in, the XUID of a player is what its client claims`)

// identityKey resolves the IdentityKey of the [Server] table with the
// AuthEnabled key the Bedrock adapter owns beside it: "auto" keys players by
// XUID if Xbox Live vouches for them and by name otherwise, and "xuid" needs
// Xbox Live sign-in, as offline the XUID of a player is a claim. It logs
// the mode chosen, with a warning in name mode.
func identityKey(value string, authEnabled bool, log *slog.Logger) (identity.Mode, error) {
	mode := identity.Auto
	if strings.TrimSpace(value) != "" {
		var err error
		if mode, err = identity.ParseMode(value); err != nil {
			return mode, err
		}
	}
	switch mode {
	case identity.Auto:
		mode = identity.Name
		if authEnabled {
			mode = identity.XUID
		}
	case identity.XUID:
		if !authEnabled {
			return mode, errIdentityXUIDOffline
		}
	}
	switch {
	case mode == identity.XUID:
		log.Info("Players are keyed by their Xbox accounts (IdentityKey = \"xuid\").")
	case !authEnabled:
		log.Warn("Xbox Live sign-in is off (AuthEnabled = false): players are keyed by name, and anyone can join as any name, an operator's included. Use this only on a network you trust.")
	default:
		log.Warn("IdentityKey = \"name\": a player who renames their Xbox account loses operator status, leaves the whitelist and escapes a ban by name.")
	}
	return mode, nil
}

// marshalConfig writes the parts as one TOML document: their tables merged,
// keys sorted, as go-toml writes one struct that holds the keys of all. A key
// that two parts both write is an error.
func marshalConfig(parts ...any) ([]byte, error) {
	var merged *toml.Tree
	for _, p := range parts {
		data, err := toml.Marshal(p)
		if err != nil {
			return nil, err
		}
		tree, err := toml.LoadBytes(data)
		if err != nil {
			return nil, err
		}
		if merged == nil {
			merged = tree
			continue
		}
		if err := mergeTree(merged, tree, nil); err != nil {
			return nil, err
		}
	}
	if merged == nil {
		return nil, nil
	}
	return toml.Marshal(merged)
}

// mergeTree sets every key of src in dst, merging the tables both have. It
// goes through the keys in order, so that the key it reports two parts
// write is the first.
func mergeTree(dst, src *toml.Tree, path []string) error {
	keys := src.Keys()
	slices.Sort(keys)
	for _, k := range keys {
		v := src.GetPath([]string{k})
		existing := dst.GetPath([]string{k})
		if existing == nil {
			dst.SetPath([]string{k}, v)
			continue
		}
		sub, ok := v.(*toml.Tree)
		into, isTable := existing.(*toml.Tree)
		if !ok || !isTable {
			return fmt.Errorf("two parts of the config write %s", strings.Join(append(path[:len(path):len(path)], k), "."))
		}
		if err := mergeTree(into, sub, append(path[:len(path):len(path)], k)); err != nil {
			return err
		}
	}
	return nil
}
