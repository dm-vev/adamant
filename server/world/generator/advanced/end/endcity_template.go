package end

import (
	"fmt"
	"sync"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type endCityTemplate struct {
	Size [3]int
	Java *javastructure.Template
}

var (
	endCityTemplatesOnce sync.Once
	endCityTemplates     map[string]*endCityTemplate
	endCityTemplatesErr  error
)

func getEndCityTemplate(name string) (*endCityTemplate, error) {
	endCityTemplatesOnce.Do(func() {
		endCityTemplates, endCityTemplatesErr = loadEndCityTemplates()
	})
	if endCityTemplatesErr != nil {
		return nil, endCityTemplatesErr
	}
	t, ok := endCityTemplates[name]
	if !ok {
		return nil, fmt.Errorf("java structure: missing End City template %q", name)
	}
	return t, nil
}

func loadEndCityTemplates() (map[string]*endCityTemplate, error) {
	names := [...]string{
		"base_floor", "base_roof", "bridge_end", "bridge_gentle_stairs", "bridge_piece", "bridge_steep_stairs",
		"fat_tower_base", "fat_tower_middle", "fat_tower_top", "second_floor_1", "second_floor_2", "second_roof", "ship",
		"third_floor_1", "third_floor_2", "third_roof", "tower_base", "tower_floor", "tower_piece", "tower_top",
	}
	out := make(map[string]*endCityTemplate, len(names))
	for _, name := range names {
		javaTemplate, err := javastructure.Load("end_city/" + name)
		if err != nil {
			return nil, err
		}
		if len(javaTemplate.Palettes) == 0 {
			return nil, fmt.Errorf("java structure end_city/%s has no palette", name)
		}
		out[name] = &endCityTemplate{
			Size: [3]int{int(javaTemplate.Size[0]), int(javaTemplate.Size[1]), int(javaTemplate.Size[2])},
			Java: javaTemplate,
		}
	}
	return out, nil
}
