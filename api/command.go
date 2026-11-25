package api

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// CommandSpec declaratively defines a server command registered via RegisterCommand.
type CommandSpec struct {
	// Name is the primary lowercase command name (without leading slash).
	Name string
	// Description is shown in /help and command metadata.
	Description string
	// Aliases lists alternative names for the command.
	Aliases []string
	// Permission sets the minimum cmd.Level required to execute the command.
	Permission cmd.Level
	// Allow optionally filters whether src may execute the command.
	Allow func(src cmd.Source) bool
	// Run handles command execution with parsed space-separated args.
	Run func(src cmd.Source, args []string, tx *world.Tx)
	// RunOutput optionally handles execution when writing to *cmd.Output directly.
	RunOutput func(src cmd.Source, out *cmd.Output, args []string, tx *world.Tx)
}

type declarativeRunnable struct {
	Args cmd.Optional[cmd.Varargs] `cmd:"args"`
	spec CommandSpec
}

func (r declarativeRunnable) RequiredLevel() cmd.Level {
	return r.spec.Permission
}

func (r declarativeRunnable) Allow(src cmd.Source) bool {
	if r.spec.Allow != nil {
		return r.spec.Allow(src)
	}
	return true
}

func (r declarativeRunnable) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	var args []string
	if raw, ok := r.Args.Load(); ok {
		args = strings.Fields(string(raw))
	}
	if r.spec.RunOutput != nil {
		r.spec.RunOutput(src, o, args, tx)
	}
	if r.spec.Run != nil {
		r.spec.Run(src, args, tx)
	}
}

// RegisterCommand compiles spec into a cmd.Command and registers it with cmd.Register.
func RegisterCommand(spec CommandSpec) cmd.Command {
	if spec.Name == "" {
		panic("api: CommandSpec.Name must not be empty")
	}
	c := cmd.New(spec.Name, spec.Description, spec.Aliases, declarativeRunnable{spec: spec})
	cmd.Register(c)
	return c
}

// RegisterTypedCommand registers a strongly typed command with full Bedrock
// client-side argument autocomplete (targets, numbers, enums, subcommands,
// optional parameters) using one or more cmd.Runnable overload structs.
func RegisterTypedCommand(name, description string, aliases []string, runnables ...cmd.Runnable) cmd.Command {
	if name == "" {
		panic("api: RegisterTypedCommand name must not be empty")
	}
	if len(runnables) == 0 {
		panic("api: RegisterTypedCommand requires at least one cmd.Runnable")
	}
	c := cmd.New(name, description, aliases, runnables...)
	cmd.Register(c)
	return c
}

// SubCommandSpec defines one subcommand branch inside CommandGroupSpec with
// client-side subcommand tab-completion.
type SubCommandSpec struct {
	// Name is the subcommand literal shown in Bedrock autocomplete (e.g. "buy").
	Name string
	// Permission overrides the parent group's permission level when non-zero.
	Permission cmd.Level
	// Allow optionally filters whether src may execute this subcommand.
	Allow func(src cmd.Source) bool
	// Run handles subcommand execution with trailing space-separated args.
	Run func(src cmd.Source, args []string, tx *world.Tx)
	// RunOutput optionally handles execution with direct *cmd.Output access.
	RunOutput func(src cmd.Source, out *cmd.Output, args []string, tx *world.Tx)
}

// CommandGroupSpec registers a command with multiple autocomplete-enabled
// subcommands (and optional additional typed cmd.Runnable overloads).
type CommandGroupSpec struct {
	Name        string
	Description string
	Aliases     []string
	Permission  cmd.Level
	Allow       func(src cmd.Source) bool
	SubCommands []SubCommandSpec
	// ExtraOverloads appends custom strongly-typed cmd.Runnable overloads.
	ExtraOverloads []cmd.Runnable
}

type subCommandLiteral struct {
	expected string
}

func (s subCommandLiteral) Type() string { return "SubCommand" }

func (s subCommandLiteral) Parse(line *cmd.Line, v reflect.Value) error {
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

type groupSubRunnable struct {
	Sub  subCommandLiteral         `cmd:"sub"`
	Args cmd.Optional[cmd.Varargs] `cmd:"args"`

	subName    string
	permission cmd.Level
	allow      func(cmd.Source) bool
	run        func(cmd.Source, []string, *world.Tx)
	runOutput  func(cmd.Source, *cmd.Output, []string, *world.Tx)
}

func (r groupSubRunnable) RequiredLevel() cmd.Level { return r.permission }

func (r groupSubRunnable) Allow(src cmd.Source) bool {
	if r.allow != nil {
		return r.allow(src)
	}
	return true
}

func (r groupSubRunnable) DescribeParams(cmd.Source) []cmd.ParamInfo {
	return []cmd.ParamInfo{
		{Name: r.subName, Value: cmd.SubCommand{}},
		{Name: "args", Value: cmd.Varargs(""), Optional: true},
	}
}

func (r groupSubRunnable) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	var args []string
	if raw, ok := r.Args.Load(); ok {
		args = strings.Fields(string(raw))
	}
	if r.runOutput != nil {
		r.runOutput(src, o, args, tx)
	}
	if r.run != nil {
		r.run(src, args, tx)
	}
}

func buildSubCommandRunnable(parent CommandGroupSpec, sub SubCommandSpec) cmd.Runnable {
	if sub.Name == "" {
		panic("api: SubCommandSpec.Name must not be empty")
	}
	perm := sub.Permission
	if perm == 0 {
		perm = parent.Permission
	}
	allow := sub.Allow
	if allow == nil {
		allow = parent.Allow
	} else if parent.Allow != nil {
		parentAllow := parent.Allow
		subAllow := allow
		allow = func(src cmd.Source) bool {
			return parentAllow(src) && subAllow(src)
		}
	}
	return groupSubRunnable{
		Sub:        subCommandLiteral{expected: sub.Name},
		subName:    sub.Name,
		permission: perm,
		allow:      allow,
		run:        sub.Run,
		runOutput:  sub.RunOutput,
	}
}

// RegisterCommandGroup registers a command with typed Bedrock client-side
// subcommand autocomplete and optional extra typed cmd.Runnable overloads.
func RegisterCommandGroup(spec CommandGroupSpec) cmd.Command {
	if spec.Name == "" {
		panic("api: CommandGroupSpec.Name must not be empty")
	}
	runnables := make([]cmd.Runnable, 0, len(spec.SubCommands)+len(spec.ExtraOverloads))
	for _, sub := range spec.SubCommands {
		runnables = append(runnables, buildSubCommandRunnable(spec, sub))
	}
	runnables = append(runnables, spec.ExtraOverloads...)
	if len(runnables) == 0 {
		panic("api: CommandGroupSpec requires at least one SubCommand or ExtraOverload")
	}
	c := cmd.New(spec.Name, spec.Description, spec.Aliases, runnables...)
	cmd.Register(c)
	return c
}
