package gamedata

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

type compiledTag struct {
	members map[string]struct{}
	// short holds the minecraft members by path, so names without a
	// namespace are looked up without building the full name.
	short   map[string]struct{}
	ordered []string
	// blocks is the bitset of member block indices of a block tag, for
	// lookups by state ID.
	blocks []uint64
	err    error
}

type tagKey struct {
	kind string
	name string
}

var tagCache = struct {
	sync.Mutex
	tags map[tagKey]compiledTag
}{tags: make(map[tagKey]compiledTag)}

// compiledTags holds every finished tag for lookups without the lock, which
// generation workers make for every block they test.
var compiledTags sync.Map

// BlockTagContains resolves and caches a vanilla block tag and all nested references.
func BlockTagContains(tag, block string) (bool, error) {
	return tagContains("block_tag", tag, block)
}

// BiomeTagContains resolves and caches a vanilla biome tag and all nested references.
func BiomeTagContains(tag, biome string) (bool, error) {
	return tagContains("biome_tag", tag, biome)
}

// EntityTypeTagContains resolves and caches a vanilla entity type tag and all
// nested references. entityType is a Java entity type identifier.
func EntityTypeTagContains(tag, entityType string) (bool, error) {
	return tagContains("entity_type_tag", tag, entityType)
}

// BlockTagMembers returns the ordered, recursively expanded vanilla block-tag entries.
func BlockTagMembers(tag string) ([]string, error) {
	compiled, err := compileTag(tagKey{kind: "block_tag", name: normalizeTagName(tag)})
	if err != nil {
		return nil, err
	}
	return append([]string(nil), compiled.ordered...), nil
}

// GameEventTagMembers returns the ordered, recursively expanded source game-event tag.
func GameEventTagMembers(tag string) ([]string, error) {
	compiled, err := compileTag(tagKey{kind: "game_event_tag", name: normalizeTagName(tag)})
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
	if compiled, ok := compiledTags.Load(key); ok {
		return compiled.(compiledTag), compiled.(compiledTag).err
	}
	tagCache.Lock()
	compiled := compileTagLocked(key, make(map[tagKey]bool))
	tagCache.Unlock()
	compiledTags.Store(key, compiled)
	return compiled, compiled.err
}

// tagLookups holds the compiled tags by kind and raw, unnormalised name. It
// is replaced on every miss, so the lookups generation workers make for every
// block they test neither take a lock nor hash an interface key.
var tagLookups atomic.Pointer[map[tagKey]*compiledTag]

func lookupTag(kind, rawTag string) (*compiledTag, error) {
	key := tagKey{kind: kind, name: rawTag}
	if cache := tagLookups.Load(); cache != nil {
		if tag, ok := (*cache)[key]; ok {
			return tag, tag.err
		}
	}
	compiled, err := compileTag(key)
	compiled.err = err
	if kind == "block_tag" && err == nil {
		compiled.blocks = make([]uint64, (len(solidBlockNames)+63)/64)
		for member := range compiled.members {
			if block, ok := blockIndex(member); ok {
				compiled.blocks[block>>6] |= 1 << (block & 63)
			}
		}
	}
	tag := &compiled
	for {
		old := tagLookups.Load()
		next := make(map[tagKey]*compiledTag, 1)
		if old != nil {
			if existing, ok := (*old)[key]; ok {
				return existing, existing.err
			}
			next = make(map[tagKey]*compiledTag, len(*old)+1)
			for k, v := range *old {
				next[k] = v
			}
		}
		next[key] = tag
		if tagLookups.CompareAndSwap(old, &next) {
			return tag, err
		}
	}
}

// BlockTagContainsID reports whether the block of a canonical state ID is a
// member of a vanilla block tag, without decoding the state.
func BlockTagContainsID(tag string, id uint32) (bool, error) {
	compiled, err := lookupTag("block_tag", tag)
	if err != nil {
		return false, err
	}
	block, ok := blockStateIndex(id)
	if !ok {
		return false, nil
	}
	return compiled.blocks[block>>6]&(1<<(block&63)) != 0, nil
}

func tagContains(kind, rawTag, value string) (bool, error) {
	compiled, err := lookupTag(kind, rawTag)
	if err != nil {
		return false, err
	}
	value = strings.TrimPrefix(value, "#")
	if strings.IndexByte(value, ':') < 0 {
		_, ok := compiled.short[value]
		return ok, nil
	}
	_, ok := compiled.members[value]
	return ok, nil
}

func compileTagLocked(key tagKey, visiting map[tagKey]bool) compiledTag {
	if tag, ok := tagCache.tags[key]; ok {
		return tag
	}
	if visiting[key] {
		category := strings.TrimSuffix(key.kind, "_tag")
		return compiledTag{err: fmt.Errorf("cyclic %s tag %q", category, key.name)}
	}
	visiting[key] = true
	defer delete(visiting, key)

	data, err := LoadJSON(key.kind, key.name)
	if err != nil {
		result := compiledTag{err: err}
		tagCache.tags[key] = result
		return result
	}
	values, ok := data["values"].([]any)
	if !ok {
		result := compiledTag{err: fmt.Errorf("%s tag %q has no values", strings.TrimSuffix(key.kind, "_tag"), key.name)}
		tagCache.tags[key] = result
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
			result := compiledTag{err: fmt.Errorf("%s tag %q has invalid value", strings.TrimSuffix(key.kind, "_tag"), key.name)}
			tagCache.tags[key] = result
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
				tagCache.tags[key] = result
				return result
			}
			continue
		}
		nested := compileTagLocked(tagKey{kind: key.kind, name: name}, visiting)
		if nested.err != nil {
			if required {
				tagCache.tags[key] = nested
				return nested
			}
			continue
		}
		for _, member := range nested.ordered {
			members[member] = struct{}{}
			ordered = append(ordered, member)
		}
	}
	short := make(map[string]struct{}, len(members))
	for member := range members {
		if path, ok := strings.CutPrefix(member, "minecraft:"); ok {
			short[path] = struct{}{}
		}
	}
	result := compiledTag{members: members, short: short, ordered: ordered}
	tagCache.tags[key] = result
	return result
}
