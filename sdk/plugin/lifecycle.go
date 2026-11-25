package plugin

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Reloadable may be implemented by a Plugin that supports live configuration
// or state reload without restarting the server.
type Reloadable interface {
	Reload(ctx *Context) error
}

// Depender may be implemented by a Plugin that requires other plugins to be
// enabled before it.
type Depender interface {
	Depends() []string
}

type depFuncPlugin struct {
	name      string
	deps      []string
	onEnable  func(*Context)
	onDisable func(*Context)
	onReload  func(*Context) error
}

func (p depFuncPlugin) Name() string      { return p.name }
func (p depFuncPlugin) Depends() []string { return slices.Clone(p.deps) }

func (p depFuncPlugin) Enable(ctx *Context) {
	if p.onEnable != nil {
		p.onEnable(ctx)
	}
}

func (p depFuncPlugin) Disable(ctx *Context) {
	if p.onDisable != nil {
		p.onDisable(ctx)
	}
}

func (p depFuncPlugin) Reload(ctx *Context) error {
	if p.onReload != nil {
		return p.onReload(ctx)
	}
	return nil
}

// FuncOption configures a plugin created by NewFunc.
type FuncOption func(*depFuncPlugin)

// WithDepends declares plugin names that must be enabled before this plugin.
func WithDepends(deps ...string) FuncOption {
	return func(p *depFuncPlugin) { p.deps = append(p.deps, deps...) }
}

// WithOnDisable sets the callback run when the plugin disables.
func WithOnDisable(fn func(*Context)) FuncOption {
	return func(p *depFuncPlugin) { p.onDisable = fn }
}

// WithOnReload sets the callback run when App.Reload or App.ReloadPlugin fires.
func WithOnReload(fn func(*Context) error) FuncOption {
	return func(p *depFuncPlugin) { p.onReload = fn }
}

// NewFunc builds a named Plugin with optional dependency, disable, and reload
// hooks.
func NewFunc(name string, onEnable func(*Context), opts ...FuncOption) Plugin {
	if name == "" {
		name = "unnamed"
	}
	p := depFuncPlugin{name: name, onEnable: onEnable}
	for _, opt := range opts {
		if opt != nil {
			opt(&p)
		}
	}
	return p
}

// UseOrdered topologically sorts plugins according to any Depender.Depends()
// declarations (accounting for plugins already enabled on a) and enables them
// in dependency order. Returns an error on duplicate names, missing
// dependencies, or dependency cycles without enabling any plugin from the batch.
func (a *App) UseOrdered(plugins ...Plugin) error {
	if a == nil {
		return errors.New("sdk/plugin: nil App")
	}
	already := make(map[string]bool)
	a.mu.RLock()
	for _, entry := range a.plugins {
		if entry.enabled {
			already[entry.plugin.Name()] = true
		}
	}
	a.mu.RUnlock()

	byName := make(map[string]Plugin, len(plugins))
	orderIn := make([]string, 0, len(plugins))
	for _, p := range plugins {
		if p == nil {
			continue
		}
		name := p.Name()
		if already[name] {
			return fmt.Errorf("sdk/plugin: plugin %q already enabled", name)
		}
		if _, dup := byName[name]; dup {
			return fmt.Errorf("sdk/plugin: duplicate plugin %q", name)
		}
		byName[name] = p
		orderIn = append(orderIn, name)
	}

	var (
		state  = make(map[string]uint8, len(byName)) // 0=unvisited, 1=visiting, 2=done
		sorted = make([]Plugin, 0, len(byName))
		visit  func(name string, stack []string) error
	)
	visit = func(name string, stack []string) error {
		switch state[name] {
		case 2:
			return nil
		case 1:
			return fmt.Errorf("sdk/plugin: dependency cycle: %s -> %s", strings.Join(stack, " -> "), name)
		}
		state[name] = 1
		p := byName[name]
		if dep, ok := p.(Depender); ok {
			for _, need := range dep.Depends() {
				if need == "" || already[need] {
					continue
				}
				if _, ok := byName[need]; !ok {
					return fmt.Errorf("sdk/plugin: plugin %q requires missing dependency %q", name, need)
				}
				if err := visit(need, append(stack, name)); err != nil {
					return err
				}
			}
		}
		state[name] = 2
		sorted = append(sorted, p)
		return nil
	}

	for _, name := range orderIn {
		if err := visit(name, nil); err != nil {
			return err
		}
	}
	a.Use(sorted...)
	return nil
}

// Reload invokes Reload on every enabled plugin implementing Reloadable and
// returns a map of plugin name -> error for any plugin that failed (nil when
// all succeed).
func (a *App) Reload() map[string]error {
	if a == nil {
		return nil
	}
	a.mu.RLock()
	entries := slices.Clone(a.plugins)
	a.mu.RUnlock()

	var errs map[string]error
	for _, entry := range entries {
		if !entry.enabled {
			continue
		}
		rel, ok := entry.plugin.(Reloadable)
		if !ok {
			continue
		}
		if err := rel.Reload(entry.ctx); err != nil {
			if errs == nil {
				errs = make(map[string]error)
			}
			errs[entry.plugin.Name()] = err
		}
	}
	return errs
}

// ReloadPlugin invokes Reload on the named enabled plugin. Returns an error if
// the plugin is not found, not enabled, or its Reload fails.
func (a *App) ReloadPlugin(name string) error {
	if a == nil {
		return errors.New("sdk/plugin: nil App")
	}
	a.mu.RLock()
	var found *pluginEntry
	for i := range a.plugins {
		if a.plugins[i].plugin.Name() == name && a.plugins[i].enabled {
			e := a.plugins[i]
			found = &e
			break
		}
	}
	a.mu.RUnlock()
	if found == nil {
		return fmt.Errorf("sdk/plugin: plugin %q not enabled", name)
	}
	rel, ok := found.plugin.(Reloadable)
	if !ok {
		return nil
	}
	return rel.Reload(found.ctx)
}
