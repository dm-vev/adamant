package structure

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// ResolvePoolAliases resolves a decoded pool_aliases value for the original
// sampled structure start position. Alias results are deliberately one-hop.
func ResolvePoolAliases(raw any, pos [3]int32, seed int64) (map[string]string, error) {
	bindings, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("pool_aliases must be an array")
	}
	if len(bindings) == 0 {
		return nil, nil
	}
	for i, binding := range bindings {
		if err := validatePoolAliasBinding(binding); err != nil {
			return nil, fmt.Errorf("pool_aliases[%d]: %w", i, err)
		}
	}
	aliases := make(map[string]string)
	source := vanilla.NewLegacyRandom(seed)
	random := source.ForkPositional().At(pos[0], pos[1], pos[2])
	for i, binding := range bindings {
		if err := resolvePoolAliasBinding(binding, &random, aliases); err != nil {
			return nil, fmt.Errorf("pool_aliases[%d]: %w", i, err)
		}
	}
	return aliases, nil
}

func validatePoolAliasBinding(raw any) error {
	binding, ok := raw.(map[string]any)
	if !ok {
		return fmt.Errorf("binding must be an object")
	}
	kind, _ := binding["type"].(string)
	kind = strings.TrimPrefix(kind, "minecraft:")
	switch kind {
	case "direct":
		if _, ok := binding["alias"].(string); !ok {
			return fmt.Errorf("direct binding requires string alias")
		}
		if _, ok := binding["target"].(string); !ok {
			return fmt.Errorf("direct binding requires string target")
		}
	case "random":
		if _, ok := binding["alias"].(string); !ok {
			return fmt.Errorf("random binding requires string alias")
		}
		_, err := validateAliasWeightedList(binding["targets"], true)
		return err
	case "random_group":
		groups, err := validateAliasWeightedList(binding["groups"], false)
		if err != nil {
			return err
		}
		for i, rawEntry := range groups {
			entry := rawEntry.(map[string]any)
			groupData, ok := entry["data"].([]any)
			if !ok {
				return fmt.Errorf("groups[%d].data must be an array", i)
			}
			for j, item := range groupData {
				if err := validatePoolAliasBinding(item); err != nil {
					return fmt.Errorf("groups[%d].data[%d]: %w", i, j, err)
				}
			}
		}
	default:
		return fmt.Errorf("unknown binding type %q", kind)
	}
	return nil
}

// validateAliasWeightedList checks codec constraints and returns the original
// entries so resolution can inspect only the selected value without an AST.
func validateAliasWeightedList(raw any, requirePoolData bool) ([]any, error) {
	entries, ok := raw.([]any)
	if !ok || len(entries) == 0 {
		return nil, fmt.Errorf("weighted list must be a non-empty array")
	}
	var total int64
	for i, rawEntry := range entries {
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("entry %d must be an object", i)
		}
		weight, ok := int32Value(entry["weight"])
		if !ok || weight < 0 {
			return nil, fmt.Errorf("entry %d has invalid weight", i)
		}
		data, exists := entry["data"]
		if !exists || data == nil {
			return nil, fmt.Errorf("entry %d is missing data", i)
		}
		if requirePoolData {
			if _, ok := data.(string); !ok {
				return nil, fmt.Errorf("entry %d data must be a pool string", i)
			}
		}
		total += int64(weight)
		if total > 1<<31-1 {
			return nil, fmt.Errorf("weighted list total exceeds integer range")
		}
	}
	if total == 0 {
		return nil, fmt.Errorf("weighted list has no positive weight")
	}
	return entries, nil
}

func resolvePoolAliasBinding(raw any, random *vanilla.Random, aliases map[string]string) error {
	binding := raw.(map[string]any)
	kind, _ := binding["type"].(string)
	kind = strings.TrimPrefix(kind, "minecraft:")
	switch kind {
	case "direct":
		alias := normalizeAliasPool(binding["alias"].(string))
		target := normalizeAliasPool(binding["target"].(string))
		return putPoolAlias(aliases, alias, target)
	case "random":
		value := chooseAliasWeighted(binding["targets"].([]any), random)
		alias := normalizeAliasPool(binding["alias"].(string))
		return putPoolAlias(aliases, alias, normalizeAliasPool(value.(string)))
	case "random_group":
		group := chooseAliasWeighted(binding["groups"].([]any), random).([]any)
		for _, item := range group {
			if err := resolvePoolAliasBinding(item, random, aliases); err != nil {
				return err
			}
		}
	}
	return nil
}

func chooseAliasWeighted(entries []any, random *vanilla.Random) any {
	var total int32
	for _, rawEntry := range entries {
		weight, _ := int32Value(rawEntry.(map[string]any)["weight"])
		total += weight
	}
	choice := random.NextInt(total)
	for _, rawEntry := range entries {
		entry := rawEntry.(map[string]any)
		weight, _ := int32Value(entry["weight"])
		if choice < weight {
			return entry["data"]
		}
		choice -= weight
	}
	panic("pool alias: exhausted validated weighted list")
}

func putPoolAlias(aliases map[string]string, alias, target string) error {
	if _, exists := aliases[alias]; exists {
		return fmt.Errorf("duplicate alias %q", alias)
	}
	aliases[alias] = target
	return nil
}

func normalizeAliasPool(pool string) string {
	return strings.TrimPrefix(pool, "minecraft:")
}

func lookupPoolAlias(pool string, aliases map[string]string) string {
	pool = normalizeAliasPool(pool)
	if target, ok := aliases[pool]; ok {
		return target
	}
	return pool
}

func int32Value(raw any) (int32, bool) {
	switch value := raw.(type) {
	case int:
		if int64(value) < -1<<31 || int64(value) > 1<<31-1 {
			return 0, false
		}
		return int32(value), true
	case int32:
		return value, true
	case int64:
		if value < -1<<31 || value > 1<<31-1 {
			return 0, false
		}
		return int32(value), true
	case float64:
		if value < -1<<31 || value > 1<<31-1 || value != float64(int32(value)) {
			return 0, false
		}
		return int32(value), true
	default:
		return 0, false
	}
}
