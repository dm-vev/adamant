package javadata

import (
	"sort"
	"strings"
)

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

// BlockHasProperty reports whether the pinned Java block state defines a
// property, without copying its defaults or allocating a property map.
func BlockHasProperty(name, property string) bool {
	_, exists := blockDefaultProperties(name)[property]
	return exists
}

func blockDefaultProperties(name string) map[string]any {
	if namespace, block, qualified := strings.Cut(name, ":"); qualified {
		if namespace != "minecraft" {
			return nil
		}
		name = block
	}
	if name == "" {
		return nil
	}
	index := sort.SearchStrings(javaSolidBlockNames[:], name)
	if index == len(javaSolidBlockNames) || javaSolidBlockNames[index] != name {
		return nil
	}
	return javaBlockDefaultProperties[index]
}
