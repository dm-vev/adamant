package javadata

import (
	"sort"
	"strings"
)

// BlockPropertiesWithDefaults returns block properties completed with the pinned
// Java default state. It preserves explicit properties and leaves the input map
// untouched, allocating only when a known default is missing.
func BlockPropertiesWithDefaults(name string, properties map[string]any) map[string]any {
	if namespace, block, qualified := strings.Cut(name, ":"); qualified {
		if namespace != "minecraft" {
			return properties
		}
		name = block
	}
	if name == "" {
		return properties
	}
	index := sort.SearchStrings(javaSolidBlockNames[:], name)
	if index == len(javaSolidBlockNames) || javaSolidBlockNames[index] != name {
		return properties
	}

	defaults := javaBlockDefaultProperties[index]
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
