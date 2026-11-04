package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/protocol/bedrock"
	"github.com/df-mc/dragonfly/protocol/java"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/pelletier/go-toml"
)

// fileConfig is config.toml: the settings of the server and of each
// protocol adapter. Each part reads the keys it owns from the same file, and
// two parts may own keys of the same table.
type fileConfig struct {
	Server  server.UserConfig
	Bedrock bedrockadapter.UserConfig
	Java    javaadapter.UserConfig
}

// defaultFileConfig returns the file of the default settings of every part.
func defaultFileConfig() fileConfig {
	return fileConfig{Server: server.DefaultConfig(), Bedrock: bedrockadapter.DefaultConfig(), Java: javaadapter.DefaultConfig()}
}

// parts returns pointers to the parts of the file, in the order they are
// read and composed.
func (f *fileConfig) parts() []any { return []any{&f.Server, &f.Bedrock, &f.Java} }

// readConfig reads config.toml, or writes the default file if there is none,
// and composes the server's Config with the listeners of the adapters.
func readConfig(log *slog.Logger) (server.Config, error) {
	f, err := readConfigFile("config.toml")
	if err != nil {
		return server.Config{}, err
	}
	return f.config(log)
}

// readConfigFile reads the file of the path passed into the parts of a
// fileConfig, each filled out with its defaults first, so that a key the
// file lacks keeps its default. If there is no file, it writes the default
// one.
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
	for _, p := range f.parts() {
		if err := toml.Unmarshal(data, p); err != nil {
			return f, fmt.Errorf("decode config: %v", err)
		}
	}
	return f, nil
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
	// The adapters' parts come first: a bad Transport is reported before the
	// world is opened, as UserConfig.Config reported it before.
	bc, err := f.Bedrock.Config(log)
	if err != nil {
		return server.Config{}, bc, javaadapter.Config{}, err
	}
	jc := f.Java.Config()
	key, err := identityKey(f.Server.Server.IdentityKey, !bc.AuthDisabled, log)
	if err != nil {
		return server.Config{}, bc, jc, err
	}
	f.Server.Server.IdentityKey = key.String()
	conf, err := f.Server.Config(log)
	if err != nil {
		return conf, bc, jc, err
	}
	conf.Listeners = append(conf.Listeners, bc.Listener)
	if jc.Address != "" {
		conf.Listeners = append(conf.Listeners, jc.Listener)
	}
	return conf, bc, jc, nil
}

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
			return mode, errors.New(`IdentityKey "xuid" needs AuthEnabled = true: without Xbox Live sign-in, the XUID of a player is what its client claims`)
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
