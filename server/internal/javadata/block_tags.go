package javadata

import (
	"fmt"
	"strings"
	"sync"
)

type compiledTag struct {
	members map[string]struct{}
	ordered []string
	err error
}

type tagKey struct {
	kind string
	name string
}

var javaTagCache = struct {
	sync.Mutex
	tags map[tagKey]compiledTag
}{tags: make(map[tagKey]compiledTag)}

// BlockTagContains resolves and caches a vanilla block tag and all nested references.
func BlockTagContains(tag, block string) (bool, error) {
	return tagContains("block_tag", tag, block)
}

// BiomeTagContains resolves and caches a vanilla biome tag and all nested references.
func BiomeTagContains(tag, biome string) (bool, error) {
	return tagContains("biome_tag", tag, biome)
}

// BlockTagMembers returns the ordered, recursively expanded vanilla block-tag entries.
func BlockTagMembers(tag string) ([]string, error) {
	compiled, err := compileTag(tagKey{kind: "block_tag", name: normalizeTagName(tag)})
	if err != nil {
		return nil, err
	}
	return append([]string(nil), compiled.ordered...), nil
}

func normalizeTagName(raw string) string {
	tag := strings.TrimPrefix(raw, "#")
	namespace, name, found := strings.Cut(tag, ":")
	if !found {
		return tag
	}
	if namespace != "minecraft" {
		return tag
	}
	return name
}

func compileTag(key tagKey) (compiledTag, error) {
	namespace, _, found := strings.Cut(strings.TrimPrefix(key.name, "#"), ":")
	if found && namespace != "minecraft" {
		return compiledTag{}, fmt.Errorf("unsupported %s tag namespace %q", strings.TrimSuffix(key.kind, "_tag"), namespace)
	}
	key.name = normalizeTagName(key.name)
	javaTagCache.Lock()
	compiled := compileTagLocked(key, make(map[tagKey]bool))
	javaTagCache.Unlock()
	return compiled, compiled.err
}

func tagContains(kind, rawTag, value string) (bool, error) {
	compiled, err := compileTag(tagKey{kind: kind, name: rawTag})
	if err != nil {
		return false, err
	}
	_, ok := compiled.members[canonicalResourceName(value)]
	return ok, nil
}

func canonicalResourceName(name string) string {
	name = strings.TrimPrefix(name, "#")
	if !strings.Contains(name, ":") {
		return "minecraft:" + name
	}
	return name
}

func compileTagLocked(key tagKey, visiting map[tagKey]bool) compiledTag {
	if tag, ok := javaTagCache.tags[key]; ok {
		return tag
	}
	if visiting[key] {
		category := strings.TrimSuffix(key.kind, "_tag")
		return compiledTag{err: fmt.Errorf("cyclic Java %s tag %q", category, key.name)}
	}
	visiting[key] = true
	defer delete(visiting, key)

	data, err := LoadJSON(key.kind, key.name)
	if err != nil {
		result := compiledTag{err: err}
		javaTagCache.tags[key] = result
		return result
	}
	values, ok := data["values"].([]any)
	if !ok {
		result := compiledTag{err: fmt.Errorf("Java %s tag %q has no values", strings.TrimSuffix(key.kind, "_tag"), key.name)}
		javaTagCache.tags[key] = result
		return result
	}
	members := make(map[string]struct{})
	ordered := make([]string, 0, len(values))
	for _, raw := range values {
		value, required := raw.(string), true
		if entry, ok := raw.(map[string]any); ok {
			value, _ = entry["id"].(string)
			if optional, ok := entry["required"].(bool); ok {
				required = optional
			}
		}
		if value == "" {
			result := compiledTag{err: fmt.Errorf("Java %s tag %q has invalid value", strings.TrimSuffix(key.kind, "_tag"), key.name)}
			javaTagCache.tags[key] = result
			return result
		}
		if !strings.HasPrefix(value, "#") {
			members[value] = struct{}{}
			ordered = append(ordered, value)
			continue
		}
		namespace, name, found := strings.Cut(strings.TrimPrefix(value, "#"), ":")
		if !found {
			namespace, name = "minecraft", strings.TrimPrefix(value, "#")
		}
		if namespace != "minecraft" {
			if required {
				result := compiledTag{err: fmt.Errorf("unsupported %s tag namespace %q", strings.TrimSuffix(key.kind, "_tag"), namespace)}
				javaTagCache.tags[key] = result
				return result
			}
			continue
		}
		nested := compileTagLocked(tagKey{kind: key.kind, name: name}, visiting)
		if nested.err != nil {
			if required {
				javaTagCache.tags[key] = nested
				return nested
			}
			continue
		}
		for _, member := range nested.ordered {
			members[member] = struct{}{}
			ordered = append(ordered, member)
		}
	}
	result := compiledTag{members: members, ordered: ordered}
	javaTagCache.tags[key] = result
	return result
}
