package world

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestJavaStateRulesMatch requires the rules to give what the old codec gives,
// errors included, over the whole domain of TestJavaStatesAreStable.
func TestJavaStateRulesMatch(t *testing.T) {
	t.Parallel()
	registry := javaRulesRegistry()
	failures := 0
	report := func(format string, args ...any) {
		if failures++; failures <= 40 {
			t.Errorf(format, args...)
		}
	}
	for id := uint32(1); ; id++ {
		name, properties, ok := gamedata.BlockStateFromID(id)
		if !ok {
			break
		}
		state := BlockState{Name: name, Properties: properties}
		if old, rules := javaForwardOld(registry, state), javaForwardRules(registry, state); old != rules {
			report("forward %s:\n old:   %s\n rules: %s", formatJavaStateLine(name, properties), old, rules)
		}
	}
	for _, native := range javaReverseDomain(registry) {
		if old, rules := javaReverseOld(registry, native), javaReverseRules(registry, native); old != rules {
			report("reverse %s:\n old:   %s\n rules: %s", formatNativeStateLine(registry, native), old, rules)
		}
	}
	if failures > 0 {
		t.Fatalf("%d states differ", failures)
	}
}

// FuzzJavaStateRules compares the rules with the old codec on Java property
// maps outside the registry: properties dropped, given values of other types
// or blocks, added, or moved to another block. It compares each property on
// its own, and the whole state unless its result depends on the order the old
// codec ranges over the properties in: with two failing properties the error
// names either, and two properties mapping to one native property with
// different values leave either value.
func FuzzJavaStateRules(f *testing.F) {
	for _, seed := range []string{"", "\x00", "\x01\x02", "\x03\x04\x05", "\x06\x07\x08\x09"} {
		f.Add(uint32(1), []byte(seed))
		f.Add(uint32(5000), []byte(seed))
		f.Add(uint32(20000), []byte(seed))
	}
	registry := javaRulesRegistry()
	keys, values := javaFuzzVocabulary()
	count := javaStateCount()
	f.Fuzz(func(t *testing.T, id uint32, ops []byte) {
		name, properties, _ := gamedata.BlockStateFromID(id%count + 1)
		for len(ops) >= 2 {
			op, arg := ops[0], int(ops[1])
			ops = ops[2:]
			sorted := slices.Sorted(maps.Keys(properties))
			switch op % 5 {
			case 0: // drop a property, so it takes its default
				if len(sorted) > 0 {
					delete(properties, sorted[arg%len(sorted)])
				}
			case 1: // give a property another value
				if len(sorted) > 0 {
					properties[sorted[arg%len(sorted)]] = values[arg*7%len(values)]
				}
			case 2: // add a property of another block
				properties[keys[arg%len(keys)]] = values[arg*13%len(values)]
			case 3: // move the properties to another block
				name, _, _ = gamedata.BlockStateFromID(uint32(arg)*uint32(len(ops)+1)*977%count + 1)
			case 4: // write a value as a string
				if len(sorted) > 0 {
					key := sorted[arg%len(sorted)]
					properties[key] = fmt.Sprint(properties[key])
				}
			}
		}
		state := BlockState{Name: name, Properties: properties}
		if !javaPropertiesMatch(t, registry, state) {
			return
		}
		if old, rules := javaForwardOld(registry, state), javaForwardRules(registry, state); old != rules {
			t.Fatalf("forward %s:\n old:   %s\n rules: %s", formatJavaStateLine(name, properties), old, rules)
		}
	})
}

// javaPropertiesMatch compares the old codec and the rules on each property
// of a state, and reports whether the result of the whole state is
// independent of the order of its properties.
func javaPropertiesMatch(t *testing.T, registry chunk.BlockRegistry, state BlockState) bool {
	javaName := NormaliseBlockName(state.Name)
	paletteName, paletteProps := javaActorPaletteState(javaName, gamedata.BlockPropertiesWithDefaults(javaName, state.Properties))
	name, err := nativeBlockNameOf(registry, paletteName, paletteProps)
	if err != nil {
		return true
	}
	rid, ok := registry.StateToRuntimeID(name, nil)
	if !ok {
		return true
	}
	_, schema, _ := registry.RuntimeIDToState(rid)
	failing, targets, independent := 0, map[string]string{}, true
	for _, key := range slices.Sorted(maps.Keys(paletteProps)) {
		value := paletteProps[key]
		target, mapped, consume, err := mapJavaProperty(paletteName, name, key, value, paletteProps, schema)
		old := javaPropertyResult(target, mapped, consume, err)
		c := ruleCtx{java: paletteName, native: name, javaProps: paletteProps, nativeProps: schema, out: map[string]any{}}
		ruleTarget, ruleMapped, ruleErr := c.forward(key, value)
		rules := javaPropertyResult(ruleTarget, ruleMapped, ruleMapped == consumed, ruleErr)
		if old != rules {
			t.Fatalf("forward %s property %s on native %s:\n old:   %s\n rules: %s", formatJavaStateLine(state.Name, state.Properties), key, name, old, rules)
		}
		switch {
		case err != nil || !consume && target == "":
			failing++
		case !consume:
			value := fmt.Sprintf("%T(%#v)", mapped, mapped)
			if previous, ok := targets[target]; ok && previous != value {
				independent = false
			}
			targets[target] = value
		}
	}
	return independent && failing <= 1
}

var javaRulesRegistryOnce = sync.OnceValue(func() chunk.BlockRegistry {
	registry := NewBlockRegistry()
	registry.Finalize()
	return registry
})

func javaRulesRegistry() chunk.BlockRegistry { return javaRulesRegistryOnce() }

func javaStateCount() uint32 {
	count := uint32(0)
	for {
		if _, ok := gamedata.BlockStateName(count + 1); !ok {
			return count
		}
		count++
	}
}

// javaFuzzVocabulary returns every Java property name, and values of every
// type the codec reads: each Java property value, integers of several types
// and ranges, strings that parse, and values of no type the codec knows.
func javaFuzzVocabulary() ([]string, []any) {
	keys := map[string]struct{}{}
	values := map[string]any{}
	for id := uint32(1); ; id++ {
		_, properties, ok := gamedata.BlockStateFromID(id)
		if !ok {
			break
		}
		for key, value := range properties {
			keys[key] = struct{}{}
			values[fmt.Sprintf("%T %v", value, value)] = value
		}
	}
	for _, value := range []any{
		int32(-1), int32(16), int32(64), int(2), int8(1), uint8(0), uint8(1), uint8(2), int64(3), uint16(4), uint32(5),
		"0", "1", "2", "15", "true", "false", "", "up", "unknown", float64(1), nil, []any{"x"}, map[string]any{},
	} {
		values[fmt.Sprintf("%T %v", value, value)] = value
	}
	sortedValues := make([]any, 0, len(values))
	for _, key := range slices.Sorted(maps.Keys(values)) {
		sortedValues = append(sortedValues, values[key])
	}
	return slices.Sorted(maps.Keys(keys)), sortedValues
}

// javaPropertyResult writes what a codec makes of one Java property: an
// error, the property consumed, or the native property and value.
func javaPropertyResult(target string, mapped any, consume bool, err error) string {
	switch {
	case err != nil:
		return "error " + err.Error()
	case consume:
		return "consumed"
	}
	return fmt.Sprintf("%q %T(%#v)", target, mapped, mapped)
}

// javaReverseDomain returns the native states of TestJavaStatesAreStable.
func javaReverseDomain(registry chunk.BlockRegistry) []NativeState {
	air := registry.AirRuntimeID()
	water, _ := registry.StateToRuntimeID("minecraft:water", waterSourceProperties)
	var states []NativeState
	for rid := range uint32(registry.BlockCount()) {
		states = append(states, NativeState{Block: rid, Liquid: air}, NativeState{Block: rid, Liquid: water})
		if name, _, _ := registry.RuntimeIDToState(rid); isFreeFluid(name) {
			states = append(states, NativeState{Block: air, Liquid: rid})
		}
	}
	return states
}

func javaForwardOld(registry chunk.BlockRegistry, state BlockState) string {
	return javaForwardResult(registry, state, resolveJavaState, nativeBlockState)
}

func javaForwardRules(registry chunk.BlockRegistry, state BlockState) string {
	return javaForwardResult(registry, state, resolveJavaStateByRules, nativeBlockStateByRules)
}

// javaForwardResult writes what a codec makes of a Java state: the native
// block and its properties as mapped, and the layers they resolve to.
func javaForwardResult(registry chunk.BlockRegistry, state BlockState,
	resolve func(chunk.BlockRegistry, BlockState) (NativeState, error),
	native func(chunk.BlockRegistry, string, map[string]any) (string, map[string]any, error),
) (result string) {
	// The registry panics on property values of a type it cannot hash, as
	// both codecs pass them on; the panic is part of the result.
	defer func() {
		if r := recover(); r != nil {
			result += fmt.Sprintf(" panic: %v", r)
		}
	}()
	javaName := NormaliseBlockName(state.Name)
	paletteName, paletteProps := javaActorPaletteState(javaName, gamedata.BlockPropertiesWithDefaults(javaName, state.Properties))
	var mapped string
	if javaName == "" {
		mapped = "no name"
	} else if name, props, err := native(registry, paletteName, paletteProps); err != nil {
		mapped = "error " + err.Error()
	} else {
		mapped = javaTypedState(name, props)
	}
	resolved, err := resolve(registry, state)
	if err != nil {
		return mapped + " | " + err.Error()
	}
	return mapped + " -> " + formatNativeStateLine(registry, resolved)
}

func javaReverseOld(registry chunk.BlockRegistry, state NativeState) string {
	source, err := reverseNativeState(registry, state)
	return javaReverseResult(source, err)
}

func javaReverseRules(registry chunk.BlockRegistry, state NativeState) string {
	source, err := reverseNativeStateByRules(registry, state)
	return javaReverseResult(source, err)
}

func javaReverseResult(source BlockState, err error) string {
	if err != nil {
		return "error " + err.Error()
	}
	return javaTypedState(source.Name, source.Properties)
}

// javaTypedState writes a state with the Go type of every value, so that
// equal lines are deeply equal states.
func javaTypedState(name string, properties map[string]any) string {
	line := name + "{"
	for _, key := range slices.Sorted(maps.Keys(properties)) {
		line += fmt.Sprintf("%s=%T(%#v) ", key, properties[key], properties[key])
	}
	return line + "}"
}
