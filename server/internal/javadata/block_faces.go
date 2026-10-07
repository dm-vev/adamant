package javadata

import (
	"fmt"
	"sort"
	"strings"
)

type generationFaceProperty struct {
	name   string
	values []any
}

type generationFaceBlock struct {
	properties []generationFaceProperty
	masks      []uint32
}

// GenerationShape describes source shapes during fresh world generation, with
// closed shulkers and no active moving-piston block entities. Face masks use
// down, up, north, south, west, east order.
type GenerationShape struct {
	Attachment uint8 // Full support OR collision face.
	Sturdy uint8 // SupportType.FULL.
	Center uint8 // SupportType.CENTER; caller still applies any block-tag rules.
	CollisionFull bool
	CollisionEmpty bool
	Replaceable bool
}

// BlockGenerationShape returns source state-sensitive support and collision
// properties. It is not an entity-dependent runtime collision-shape API.
func BlockGenerationShape(name string, properties map[string]any) (GenerationShape, error) {
	namespace, block, qualified := strings.Cut(name, ":")
	if qualified {
		if namespace != "minecraft" {
			return GenerationShape{}, fmt.Errorf("Java generation faces: unsupported namespace %q", namespace)
		}
		name = block
	}
	index := sort.SearchStrings(javaSolidBlockNames[:], name)
	if index == len(javaSolidBlockNames) || javaSolidBlockNames[index] != name {
		return GenerationShape{}, fmt.Errorf("Java generation faces: unknown block %q", name)
	}
	entry := &javaGenerationFaceBlocks[index]
	stateIndex := 0
	for _, property := range entry.properties {
		value, exists := properties[property.name]
		if !exists {
			value = javaBlockDefaultProperties[index][property.name]
		}
		valueIndex := 0
		for valueIndex < len(property.values) && property.values[valueIndex] != value {
			valueIndex++
		}
		if valueIndex == len(property.values) {
			return GenerationShape{}, fmt.Errorf("Java generation faces: invalid %s.%s=%v", name, property.name, value)
		}
		stateIndex = stateIndex*len(property.values) + valueIndex
	}
	mask := entry.masks[stateIndex]
	return GenerationShape{
		Attachment: uint8(mask & 63),
		Sturdy: uint8(mask >> 6 & 63),
		Center: uint8(mask >> 12 & 63),
		CollisionFull: mask&(1<<18) != 0,
		CollisionEmpty: mask&(1<<19) != 0,
		Replaceable: mask&(1<<20) != 0,
	}, nil
}
