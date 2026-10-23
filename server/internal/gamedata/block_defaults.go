package gamedata

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"math"
	"sort"
	"strings"
	"sync/atomic"
)

type propertyDomain struct {
	name     string
	kind     byte
	values   []string
	min, max int32
}

// BlockPropertiesWithDefaults returns block properties completed with the pinned
// Java default state. It preserves explicit properties and leaves the input map
// untouched, allocating only when a known default is missing.
func BlockPropertiesWithDefaults(name string, properties map[string]any) map[string]any {
	defaults := blockDefaultProperties(name)
	missing := 0
	for property := range defaults {
		if _, exists := properties[property]; !exists {
			missing++
		}
	}
	if missing == 0 {
		return properties
	}

	completed := make(map[string]any, len(properties)+missing)
	for property, value := range properties {
		completed[property] = value
	}
	for property, value := range defaults {
		if _, exists := completed[property]; !exists {
			completed[property] = value
		}
	}
	return completed
}

// BlockHasProperty reports whether the pinned Java block state defines a property.
func BlockHasProperty(name, property string) bool {
	return blockPropertyDomain(name, property) != nil
}

// HasBlock reports whether name is present in the pinned Java block registry.
func HasBlock(name string) bool {
	_, ok := blockIndex(name)
	return ok
}

// ParseBlockPropertyValue validates and parses a value against the pinned Java property domain.
func ParseBlockPropertyValue(name, property, value string) (any, error) {
	domain := blockPropertyDomain(name, property)
	if domain == nil {
		return nil, fmt.Errorf("unknown property %q for %s", property, name)
	}
	switch domain.kind {
	case 'i':
		n, err := vanilla.ParseSignedInteger(value, 10, 32)
		if err != nil || int32(n) < domain.min || int32(n) > domain.max {
			return nil, fmt.Errorf("invalid value %q for %s.%s", value, name, property)
		}
		return int32(n), nil
	case 'b':
		if value == "true" {
			return true, nil
		}
		if value == "false" {
			return false, nil
		}
	default:
		i := sort.SearchStrings(domain.values, value)
		if i < len(domain.values) && domain.values[i] == value {
			return domain.values[i], nil
		}
	}
	return nil, fmt.Errorf("invalid value %q for %s.%s", value, name, property)
}

func blockPropertyDomain(name, property string) *propertyDomain {
	index, ok := blockIndex(name)
	if !ok {
		return nil
	}
	for _, id := range blockPropertyDomains[index] {
		domain := &propertyDomains[id]
		if domain.name == property {
			return domain
		}
	}
	return nil
}

func blockIndex(name string) (int, bool) {
	if namespace, block, qualified := strings.Cut(name, ":"); qualified {
		if namespace != "minecraft" {
			return 0, false
		}
		name = block
	}
	index := sort.SearchStrings(solidBlockNames[:], name)
	return index, index < len(solidBlockNames) && solidBlockNames[index] == name
}

func blockDefaultProperties(name string) map[string]any {
	index, ok := blockIndex(name)
	if !ok {
		return nil
	}
	return defaultBlockProperties[index]
}

type blockStateCodec struct {
	offset uint32
	stride []uint32
	count  uint32
}

var blockStateCodecs = make([]blockStateCodec, len(blockPropertyDomains))
var blockStateCount uint32
var blockQualifiedNames = make([]string, len(solidBlockNames))

func init() {
	for i, name := range solidBlockNames {
		blockQualifiedNames[i] = "minecraft:" + name
	}
	for blockIndex, propertyIDs := range blockPropertyDomains {
		codec := blockStateCodec{offset: blockStateCount, stride: make([]uint32, len(propertyIDs)), count: 1}
		for i, propertyID := range propertyIDs {
			codec.stride[i] = codec.count
			domain := &propertyDomains[propertyID]
			size := uint32(2)
			switch domain.kind {
			case 's':
				size = uint32(len(domain.values))
			case 'i':
				size = uint32(int64(domain.max)-int64(domain.min)) + 1
			}
			codec.count *= size
		}
		blockStateCodecs[blockIndex] = codec
		blockStateCount += codec.count
	}
	stateBlocks = make([]uint16, blockStateCount)
	for blockIndex := range blockStateCodecs {
		codec := &blockStateCodecs[blockIndex]
		for value := codec.offset; value < codec.offset+codec.count; value++ {
			stateBlocks[value] = uint16(blockIndex)
		}
	}
	stateViews = make([]atomic.Pointer[map[string]any], blockStateCount+1)
}

// stateBlocks maps a state ID minus one to its block index, so decoding a
// state does not search the codecs.
var stateBlocks []uint16

// stateViews holds the shared property maps of BlockStateView by state ID.
var stateViews []atomic.Pointer[map[string]any]

// BlockStateID returns a canonical, process-local ID for a Java block state.
// ID zero is reserved for states without Java metadata.
func BlockStateID(name string, properties map[string]any) (uint32, error) {
	block, ok := blockIndex(name)
	if !ok {
		return 0, fmt.Errorf("unknown block %q", name)
	}
	domains := blockPropertyDomains[block]
	defaults := defaultBlockProperties[block]
	codec := &blockStateCodecs[block]
	var state uint32
	matched := 0
	for i, propertyID := range domains {
		domain := &propertyDomains[propertyID]
		value, exists := properties[domain.name]
		if exists {
			matched++
		} else {
			value = defaults[domain.name]
		}
		index, err := propertyValueIndex(domain, value)
		if err != nil {
			return 0, fmt.Errorf("invalid value for %s.%s", name, domain.name)
		}
		state += index * codec.stride[i]
	}
	if matched != len(properties) {
		for property := range properties {
			if blockPropertyDomain(name, property) == nil {
				return 0, fmt.Errorf("unknown property %q for %s", property, name)
			}
		}
	}
	return codec.offset + state + 1, nil
}

// BlockStateFromID decodes a canonical process-local block-state ID.
func BlockStateFromID(id uint32) (name string, properties map[string]any, ok bool) {
	block, ok := blockStateIndex(id)
	if !ok {
		return "", nil, false
	}
	codec := &blockStateCodecs[block]
	value := id - 1 - codec.offset
	properties = make(map[string]any, len(blockPropertyDomains[block]))
	for i, propertyID := range blockPropertyDomains[block] {
		domain := &propertyDomains[propertyID]
		size := uint32(2)
		switch domain.kind {
		case 's':
			size = uint32(len(domain.values))
		case 'i':
			size = uint32(int64(domain.max)-int64(domain.min)) + 1
		}
		index := value / codec.stride[i] % size
		switch domain.kind {
		case 's':
			properties[domain.name] = domain.values[index]
		case 'b':
			properties[domain.name] = index != 0
		case 'i':
			properties[domain.name] = int32(int64(domain.min) + int64(index))
		}
	}
	return blockQualifiedNames[block], properties, true
}

// BlockStateView decodes a canonical state like BlockStateFromID, but returns
// a property map shared by every caller of the same ID. Callers must not
// modify it; generation workers read states millions of times per chunk.
func BlockStateView(id uint32) (name string, properties map[string]any, ok bool) {
	block, ok := blockStateIndex(id)
	if !ok {
		return "", nil, false
	}
	if cached := stateViews[id].Load(); cached != nil {
		return blockQualifiedNames[block], *cached, true
	}
	_, decoded, _ := BlockStateFromID(id)
	stateViews[id].CompareAndSwap(nil, &decoded)
	return blockQualifiedNames[block], *stateViews[id].Load(), true
}

// BlockStateName returns the qualified name without materialising properties.
func BlockStateName(id uint32) (string, bool) {
	block, ok := blockStateIndex(id)
	if !ok {
		return "", false
	}
	return blockQualifiedNames[block], true
}

// BlockStateBool reads a boolean property without allocating a property map.
func BlockStateBool(id uint32, property string) (bool, bool) {
	domain, value, _, ok := blockStateProperty(id, property)
	if !ok || domain.kind != 'b' {
		return false, false
	}
	return value != 0, true
}

// BlockStateInt reads an integer property without allocating a property map.
func BlockStateInt(id uint32, property string) (int32, bool) {
	domain, value, _, ok := blockStateProperty(id, property)
	if !ok || domain.kind != 'i' {
		return 0, false
	}
	return int32(int64(domain.min) + int64(value)), true
}

// BlockStateString reads an enum property without allocating a property map.
func BlockStateString(id uint32, property string) (string, bool) {
	domain, value, _, ok := blockStateProperty(id, property)
	if !ok || domain.kind != 's' {
		return "", false
	}
	return domain.values[value], true
}

// BlockStateHasEnumValue checks a property's domain without allocating. Source
// properties with the same name may have different domains, such as FACING and
// HORIZONTAL_FACING.
func BlockStateHasEnumValue(id uint32, property, value string) bool {
	domain, _, _, ok := blockStateProperty(id, property)
	if !ok || domain.kind != 's' {
		return false
	}
	for _, candidate := range domain.values {
		if candidate == value {
			return true
		}
	}
	return false
}

// BlockStateWithProperty changes one property while preserving every other
// property in the canonical state. The successful path does not allocate.
func BlockStateWithProperty(id uint32, property string, value any) (uint32, error) {
	domain, previous, stride, ok := blockStateProperty(id, property)
	if !ok {
		return 0, fmt.Errorf("state %d has no property %q", id, property)
	}
	next, err := propertyValueIndex(domain, value)
	if err != nil {
		return 0, fmt.Errorf("invalid value for state %d property %q", id, property)
	}
	return id - previous*stride + next*stride, nil
}

// BlockStateWithPropertiesOf copies only properties with the same pinned Java
// domain. A shared name alone is insufficient: FACING and HORIZONTAL_FACING,
// for example, must not be copied between each other.
func BlockStateWithPropertiesOf(id, source uint32) (uint32, error) {
	block, ok := blockStateIndex(id)
	if !ok {
		return 0, fmt.Errorf("invalid state %d", id)
	}
	sourceBlock, ok := blockStateIndex(source)
	if !ok {
		return 0, fmt.Errorf("invalid source state %d", source)
	}
	codec, sourceCodec := &blockStateCodecs[block], &blockStateCodecs[sourceBlock]
	value, sourceValue := id-1-codec.offset, source-1-sourceCodec.offset
	result := id
	for i, propertyID := range blockPropertyDomains[block] {
		for j, sourcePropertyID := range blockPropertyDomains[sourceBlock] {
			if propertyID != sourcePropertyID {
				continue
			}
			limit, sourceLimit := codec.count, sourceCodec.count
			if i+1 < len(codec.stride) {
				limit = codec.stride[i+1]
			}
			if j+1 < len(sourceCodec.stride) {
				sourceLimit = sourceCodec.stride[j+1]
			}
			previous := value % limit / codec.stride[i]
			next := sourceValue % sourceLimit / sourceCodec.stride[j]
			result = result - previous*codec.stride[i] + next*codec.stride[i]
			break
		}
	}
	return result, nil
}

func blockStateIndex(id uint32) (int, bool) {
	if id == 0 || id > blockStateCount {
		return 0, false
	}
	return int(stateBlocks[id-1]), true
}

func blockStateProperty(id uint32, property string) (*propertyDomain, uint32, uint32, bool) {
	block, ok := blockStateIndex(id)
	if !ok {
		return nil, 0, 0, false
	}
	codec := &blockStateCodecs[block]
	for i, propertyID := range blockPropertyDomains[block] {
		domain := &propertyDomains[propertyID]
		if domain.name != property {
			continue
		}
		limit := codec.count
		if i+1 < len(codec.stride) {
			limit = codec.stride[i+1]
		}
		return domain, (id - 1 - codec.offset) % limit / codec.stride[i], codec.stride[i], true
	}
	return nil, 0, 0, false
}

func propertyValueIndex(domain *propertyDomain, value any) (uint32, error) {
	switch domain.kind {
	case 'b':
		switch v := value.(type) {
		case bool:
			if v {
				return 1, nil
			}
			return 0, nil
		case string:
			if v == "true" {
				return 1, nil
			}
			if v == "false" {
				return 0, nil
			}
		case uint8:
			if v == 0 || v == 1 {
				return uint32(v), nil
			}
		}
	case 'i':
		if n, ok := integerPropertyValue(value, domain); ok {
			return uint32(n - int64(domain.min)), nil
		}
		if v, ok := value.(string); ok {
			n, err := vanilla.ParseSignedInteger(v, 10, 32)
			if err == nil && int32(n) >= domain.min && int32(n) <= domain.max {
				return uint32(int64(n) - int64(domain.min)), nil
			}
		}
	case 's':
		if v, ok := value.(string); ok {
			i := sort.SearchStrings(domain.values, v)
			if i < len(domain.values) && domain.values[i] == v {
				return uint32(i), nil
			}
		}
	}
	return 0, fmt.Errorf("invalid value")
}

func integerPropertyValue(value any, domain *propertyDomain) (int64, bool) {
	inDomain := func(n int64) (int64, bool) {
		return n, n >= int64(domain.min) && n <= int64(domain.max)
	}
	switch v := value.(type) {
	case int:
		return inDomain(int64(v))
	case int8:
		return inDomain(int64(v))
	case int16:
		return inDomain(int64(v))
	case int32:
		return inDomain(int64(v))
	case int64:
		return inDomain(v)
	case uint:
		if uint64(v) <= uint64(domain.max) {
			return inDomain(int64(v))
		}
	case uint8:
		return inDomain(int64(v))
	case uint16:
		return inDomain(int64(v))
	case uint32:
		if uint64(v) <= uint64(domain.max) {
			return inDomain(int64(v))
		}
	case uint64:
		if v <= uint64(domain.max) {
			return inDomain(int64(v))
		}
	case float64:
		if !math.IsNaN(v) && !math.IsInf(v, 0) && math.Trunc(v) == v && v >= float64(domain.min) && v <= float64(domain.max) {
			return int64(v), true
		}
	}
	return 0, false
}
