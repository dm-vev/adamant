package javastructure

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/javadata"
)

// ProcessorSpec is one exact entry from a Java data/minecraft/worldgen/processor_list resource.
type ProcessorSpec struct {
	Type string
	Config map[string]any
}

// LoadProcessorList loads actual Java processor definitions embedded from the client data pack.
// name is relative to processor_list, without a suffix or minecraft namespace prefix.
func LoadProcessorList(name string) ([]ProcessorSpec, error) {
	name = strings.TrimPrefix(name, "minecraft:")
	name = strings.TrimPrefix(name, "/")
	if strings.Contains(name, "..") || strings.HasSuffix(name, ".json") { return nil, fmt.Errorf("java processor list: invalid name %q", name) }
	data, err := javadata.Assets().ReadFile("assets/data/minecraft/worldgen/processor_list/" + name + ".json")
	if err != nil { return nil, fmt.Errorf("java processor list %q: %w", name, err) }
	var root struct { Processors []map[string]any `json:"processors"` }
	if err := json.Unmarshal(data, &root); err != nil { return nil, fmt.Errorf("java processor list %q: %w", name, err) }
	if root.Processors == nil { return nil, fmt.Errorf("java processor list %q has no processors array", name) }
	out := make([]ProcessorSpec, len(root.Processors))
	for i, value := range root.Processors {
		typeName, ok := value["processor_type"].(string)
		if !ok || typeName == "" { return nil, fmt.Errorf("java processor list %q entry %d has no processor_type", name, i) }
		out[i] = ProcessorSpec{Type: strings.TrimPrefix(typeName,"minecraft:"), Config: value}
	}
	return out, nil
}

