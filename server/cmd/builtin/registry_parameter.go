package builtin

import (
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// itemArgument and blockArgument advertise the native registry enum types. Their
// custom parsers retain Java aliases and block states instead of requiring the
// complete token to match a bare identifier in the completion list.
type itemArgument string

func (itemArgument) Type() string                     { return "Item" }
func (itemArgument) ParamOptions() byte               { return v2193.ParamOptionHasSemanticConstraint }
func (itemArgument) Constraints(option string) []byte { return registryAliasConstraints(option) }
func (itemArgument) Parse(line *cmd.Line, value reflect.Value) error {
	return parseRegistryArgument(line, value)
}
func (itemArgument) Options(cmd.Source) []string {
	names := make(map[string]struct{})
	for _, item := range world.Items() {
		name, _ := item.EncodeItem()
		addRegistryName(names, name)
	}
	return slices.Sorted(maps.Keys(names))
}

type blockArgument string

func (blockArgument) Type() string                     { return "Block" }
func (blockArgument) ParamOptions() byte               { return v2193.ParamOptionHasSemanticConstraint }
func (blockArgument) Constraints(option string) []byte { return registryAliasConstraints(option) }
func (blockArgument) Parse(line *cmd.Line, value reflect.Value) error {
	return parseRegistryArgument(line, value)
}
func (blockArgument) Options(src cmd.Source) []string {
	registry := commandBlockRegistry(src)
	registry.Finalize()
	lookup := registry.RuntimeIDToState
	if view, ok := registry.(interface {
		RuntimeIDToStateView(uint32) (string, map[string]any, bool)
	}); ok {
		// BasicBlockRegistry has cached states, avoiding property map
		// allocation for every permutation while enumerating names.
		lookup = view.RuntimeIDToStateView
	}
	names := make(map[string]struct{})
	for rid := range uint32(registry.BlockCount()) {
		if name, _, ok := lookup(rid); ok {
			addRegistryName(names, name)
		}
	}
	return slices.Sorted(maps.Keys(names))
}

func commandBlockRegistry(src cmd.Source) world.BlockRegistry {
	for {
		if context, ok := cmd.ExecutionContextOf(src); ok && context.World != nil {
			return context.World.BlockRegistry()
		}
		if source, ok := src.(interface{ Tx() *world.Tx }); ok {
			if tx := source.Tx(); tx != nil {
				return tx.World().BlockRegistry()
			}
		}
		if context, ok := src.(cmd.ContextSource); ok {
			src = context.Source
			continue
		}
		return world.DefaultBlockRegistry
	}
}

// BDS supplies both full and unqualified names for the minecraft namespace.
// Other namespaces must remain qualified to avoid ambiguous completions.
func addRegistryName(names map[string]struct{}, name string) {
	names[name] = struct{}{}
	if short, vanilla := strings.CutPrefix(name, "minecraft:"); vanilla {
		names[short] = struct{}{}
	}
}

func registryAliasConstraints(name string) []byte {
	if strings.HasPrefix(name, "minecraft:") {
		// SemanticConstraint::RequiresAllowAliases is constraint ID 3 in
		// BDS 1.26.51. Full minecraft identifiers carry it; short names do not.
		return []byte{3}
	}
	return nil
}

func parseRegistryArgument(line *cmd.Line, value reflect.Value) error {
	arg, ok := line.Next()
	if !ok {
		return line.UsageError()
	}
	value.SetString(arg)
	return nil
}
