package main

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/df-mc/dragonfly/protocol/java"
	"github.com/df-mc/dragonfly/server"
	"github.com/pelletier/go-toml"
)

// fileConfig is config.toml: the settings of the server and of each
// protocol adapter. Each part reads the keys it owns from the same file, and
// two parts may own keys of the same table.
type fileConfig struct {
	Server server.UserConfig
	Java   javaadapter.UserConfig
}

// defaultFileConfig returns the file of the default settings of every part.
func defaultFileConfig() fileConfig {
	return fileConfig{Server: server.DefaultConfig(), Java: javaadapter.DefaultConfig()}
}

// parts returns pointers to the parts of the file, in the order they are
// read and composed.
func (f *fileConfig) parts() []any { return []any{&f.Server, &f.Java} }

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
	conf, err := f.Server.Config(log)
	if err != nil {
		return conf, err
	}
	if jc := f.Java.Config(); jc.Address != "" {
		conf.Listeners = append(conf.Listeners, jc.Listener)
	}
	return conf, nil
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

// mergeTree sets every key of src in dst, merging the tables both have.
func mergeTree(dst, src *toml.Tree, path []string) error {
	for _, k := range src.Keys() {
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
