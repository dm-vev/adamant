package javastructure

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"
)

func TestTemplateEntityYawMatchesSourceFloatArithmetic(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_template_entity_yaw.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Yaw         float32  `json:"yaw"`
		Rotation    Rotation `json:"rotation"`
		Mirror      Mirror   `json:"mirror"`
		Transformed float32  `json:"transformed"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%g/%d/%d", fixture.Yaw, fixture.Rotation, fixture.Mirror), func(t *testing.T) {
			template := Template{Entities: []Entity{{NBT: map[string]any{"id": "minecraft:villager", "Rotation": []any{fixture.Yaw, float32(0)}}}}}
			entities := template.PlaceEntities([3]int32{}, fixture.Rotation, fixture.Mirror, Bounds{Min: [3]int32{-1, -1, -1}, Max: [3]int32{1, 1, 1}}, 0)
			if len(entities) != 1 {
				t.Fatalf("template lost the in-bounds entity: %d records", len(entities))
			}
			yaw := entities[0].NBT["Rotation"].([]any)[0].(float32)
			if math.Float32bits(yaw) != math.Float32bits(fixture.Transformed) {
				t.Fatalf("yaw=%g (%08x), Source=%g (%08x)", yaw, math.Float32bits(yaw), fixture.Transformed, math.Float32bits(fixture.Transformed))
			}
		})
	}
}
