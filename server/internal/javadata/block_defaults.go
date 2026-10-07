package javadata

import (
	"fmt"
	"sort"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/java26"
)

type javaPropertyDomain struct {
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

// HasJavaBlock reports whether name is present in the pinned Java block registry.
func HasJavaBlock(name string) bool {
	_, ok := javaBlockIndex(name)
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
		n, err := java26.ParseSignedInteger(value, 10, 32)
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

func blockPropertyDomain(name, property string) *javaPropertyDomain {
	index, ok := javaBlockIndex(name)
	if !ok {
		return nil
	}
	for _, id := range javaBlockPropertyDomains[index] {
		domain := &javaPropertyDomains[id]
		if domain.name == property {
			return domain
		}
	}
	return nil
}

func javaBlockIndex(name string) (int, bool) {
	if namespace, block, qualified := strings.Cut(name, ":"); qualified {
		if namespace != "minecraft" {
			return 0, false
		}
		name = block
	}
	index := sort.SearchStrings(javaSolidBlockNames[:], name)
	return index, index < len(javaSolidBlockNames) && javaSolidBlockNames[index] == name
}

func blockDefaultProperties(name string) map[string]any {
	index, ok := javaBlockIndex(name)
	if !ok {
		return nil
	}
	return javaBlockDefaultProperties[index]
}
