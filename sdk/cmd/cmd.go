// Package cmd provides ergonomic declarative, typed, and subcommand-group
// command registration with Bedrock client-side autocomplete.
package cmd

import (
	"fmt"
	"reflect"
	"strings"

	core "github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// Type aliases for common command types so plugins rarely need to import server/cmd directly.
type (
	Command  = core.Command
	Runnable = core.Runnable
	Source   = core.Source
	Output   = core.Output
	Level    = core.Level
)

// Option configures a command or subcommand.
type Option func(*config)

type config struct {
	description string
	aliases     []string
	level       Level
	allow       func(Source) bool
	run         func(Source, []string, *world.Tx)
	runOutput   func(Source, *Output, []string, *world.Tx)
}

// Desc sets the command description shown in /help.
func Desc(s string) Option { return func(c *config) { c.description = s } }

// Aliases sets alternative command names.
func Aliases(names ...string) Option {
	return func(c *config) { c.aliases = append([]string(nil), names...) }
}

// MinLevel sets the minimum cmd.Level required to run the command.
func MinLevel(l Level) Option { return func(c *config) { c.level = l } }

// Allow restricts execution to sources for which fn returns true.
func Allow(fn func(Source) bool) Option { return func(c *config) { c.allow = fn } }

// Run sets the command handler receiving parsed space-separated arguments.
func Run(fn func(src Source, args []string, tx *world.Tx)) Option {
	return func(c *config) { c.run = fn }
}

// RunOutput sets a command handler that also writes directly to *cmd.Output.
func RunOutput(fn func(src Source, out *Output, args []string, tx *world.Tx)) Option {
	return func(c *config) { c.runOutput = fn }
}

type simpleRunnable struct {
	Args core.Optional[core.Varargs] `cmd:"args"`
	cfg  config
}

func (r simpleRunnable) RequiredLevel() Level { return r.cfg.level }

func (r simpleRunnable) Allow(src Source) bool {
	if r.cfg.allow != nil {
		return r.cfg.allow(src)
	}
	return true
}

func (r simpleRunnable) Run(src Source, o *Output, tx *world.Tx) {
	var args []string
	if raw, ok := r.Args.Load(); ok {
		args = strings.Fields(string(raw))
	}
	if r.cfg.runOutput != nil {
		r.cfg.runOutput(src, o, args, tx)
	}
	if r.cfg.run != nil {
		r.cfg.run(src, args, tx)
	}
}

// Register builds and registers a declarative command with options.
func Register(name string, opts ...Option) Command {
	if name == "" {
		panic("sdk/cmd: command name must not be empty")
	}
	var cfg config
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	c := core.New(name, cfg.description, cfg.aliases, simpleRunnable{cfg: cfg})
	core.Register(c)
	return c
}

// RegisterTyped registers a strongly typed command with full Bedrock client
// argument autocomplete using one or more cmd.Runnable overload structs.
func RegisterTyped(name, description string, aliases []string, overloads ...Runnable) Command {
	if name == "" {
		panic("sdk/cmd: command name must not be empty")
	}
	if len(overloads) == 0 {
		panic("sdk/cmd: RegisterTyped requires at least one Runnable")
	}
	c := core.New(name, description, aliases, overloads...)
	core.Register(c)
	return c
}

// SubCommand describes one autocomplete-enabled subcommand inside a Group.
type SubCommand struct {
	name string
	cfg  config
}

// Sub defines a subcommand branch for use with Group.
func Sub(name string, opts ...Option) SubCommand {
	if name == "" {
		panic("sdk/cmd: subcommand name must not be empty")
	}
	var cfg config
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return SubCommand{name: name, cfg: cfg}
}

// CommandGroup builds a multi-subcommand command with client-side tab completion.
type CommandGroup struct {
	name      string
	cfg       config
	subs      []SubCommand
	overloads []Runnable
}

// Group starts building a subcommand group under name.
func Group(name string, subs ...SubCommand) *CommandGroup {
	if name == "" {
		panic("sdk/cmd: command group name must not be empty")
	}
	return &CommandGroup{name: name, subs: append([]SubCommand(nil), subs...)}
}

// Desc sets the group's description shown in /help.
func (g *CommandGroup) Desc(s string) *CommandGroup { g.cfg.description = s; return g }

// Aliases sets alternative command names for the group.
func (g *CommandGroup) Aliases(names ...string) *CommandGroup {
	g.cfg.aliases = append([]string(nil), names...)
	return g
}

// MinLevel sets the default minimum cmd.Level for all subcommands in the group.
func (g *CommandGroup) MinLevel(l Level) *CommandGroup { g.cfg.level = l; return g }

// Allow sets a parent permission filter applied to all subcommands in the group.
func (g *CommandGroup) Allow(fn func(Source) bool) *CommandGroup { g.cfg.allow = fn; return g }

// Sub appends additional subcommands to the group.
func (g *CommandGroup) Sub(subs ...SubCommand) *CommandGroup {
	g.subs = append(g.subs, subs...)
	return g
}

// Overloads appends extra strongly-typed cmd.Runnable overloads to the group.
func (g *CommandGroup) Overloads(r ...Runnable) *CommandGroup {
	g.overloads = append(g.overloads, r...)
	return g
}

// Register compiles and registers the command group with server/cmd.
func (g *CommandGroup) Register() Command {
	runnables := make([]Runnable, 0, len(g.subs)+len(g.overloads))
	for _, sub := range g.subs {
		runnables = append(runnables, buildSubRunnable(g.cfg, sub))
	}
	runnables = append(runnables, g.overloads...)
	if len(runnables) == 0 {
		panic("sdk/cmd: Group requires at least one SubCommand or Overload")
	}
	c := core.New(g.name, g.cfg.description, g.cfg.aliases, runnables...)
	core.Register(c)
	return c
}

type subLiteral struct {
	expected string
}

func (s subLiteral) Type() string { return "SubCommand" }

func (s subLiteral) Parse(line *core.Line, v reflect.Value) error {
	tok, ok := line.Next()
	if !ok {
		return fmt.Errorf("missing subcommand %q", s.expected)
	}
	if !strings.EqualFold(tok, s.expected) {
		return fmt.Errorf("unexpected subcommand %q, want %q", tok, s.expected)
	}
	v.Set(reflect.ValueOf(s))
	return nil
}

type subRunnable struct {
	Sub  subLiteral                  `cmd:"sub"`
	Args core.Optional[core.Varargs] `cmd:"args"`
	name string
	cfg  config
}

func (r subRunnable) RequiredLevel() Level { return r.cfg.level }

func (r subRunnable) Allow(src Source) bool {
	if r.cfg.allow != nil {
		return r.cfg.allow(src)
	}
	return true
}

func (r subRunnable) DescribeParams(Source) []core.ParamInfo {
	return []core.ParamInfo{
		{Name: r.name, Value: core.SubCommand{}},
		{Name: "args", Value: core.Varargs(""), Optional: true},
	}
}

func (r subRunnable) Run(src Source, o *Output, tx *world.Tx) {
	var args []string
	if raw, ok := r.Args.Load(); ok {
		args = strings.Fields(string(raw))
	}
	if r.cfg.runOutput != nil {
		r.cfg.runOutput(src, o, args, tx)
	}
	if r.cfg.run != nil {
		r.cfg.run(src, args, tx)
	}
}

func buildSubRunnable(parent config, sub SubCommand) Runnable {
	cfg := sub.cfg
	if cfg.level == 0 {
		cfg.level = parent.level
	}
	switch {
	case cfg.allow == nil:
		cfg.allow = parent.allow
	case parent.allow != nil:
		pAllow, sAllow := parent.allow, cfg.allow
		cfg.allow = func(src Source) bool { return pAllow(src) && sAllow(src) }
	}
	return subRunnable{
		Sub:  subLiteral{expected: sub.name},
		name: sub.name,
		cfg:  cfg,
	}
}
