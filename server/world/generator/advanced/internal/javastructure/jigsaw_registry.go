package javastructure

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/javadata"
)

// A graph owns its decoded pool and template cache. Weighted choices reference
// elements instead of copying their processor lists and nested definitions.
type jigsawPoolRegistry struct {
	pools      map[string]*jigsawPool
	templates  map[string]*Template
	parts      map[*poolElement][]jigsawPart
	connectors map[jigsawPaletteKey][]connector
}
type jigsawPool struct {
	name         string
	fallback     string
	elements     []poolElement
	weight       int
	maxSize      int32
	maxSizeKnown bool
}

var emptyJigsawPool = jigsawPool{name: "empty", fallback: "empty", maxSizeKnown: true}

func newJigsawPoolRegistry() *jigsawPoolRegistry {
	return &jigsawPoolRegistry{pools: make(map[string]*jigsawPool), templates: make(map[string]*Template), parts: make(map[*poolElement][]jigsawPart), connectors: make(map[jigsawPaletteKey][]connector)}
}
func (r *jigsawPoolRegistry) pool(name string) (*jigsawPool, error) {
	name = strings.TrimPrefix(name, "minecraft:")
	if name == "empty" {
		return &emptyJigsawPool, nil
	}
	if pool, loaded := r.pools[name]; loaded {
		return pool, nil
	}
	root, err := javadata.LoadJSON("template_pool", name)
	if errors.Is(err, fs.ErrNotExist) {
		r.pools[name] = nil
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	elements, err := decodePoolElements(root["elements"])
	if err != nil {
		return nil, fmt.Errorf("Java pool %s: %w", name, err)
	}
	fallback, _ := root["fallback"].(string)
	pool := &jigsawPool{name: name, fallback: strings.TrimPrefix(fallback, "minecraft:"), elements: elements}
	for i := range elements {
		pool.weight += int(elements[i].weight)
	}
	r.pools[name] = pool
	return pool, nil
}
func (r *jigsawPoolRegistry) startPool(name string, aliases map[string]string) (*jigsawPool, error) {
	name = strings.TrimPrefix(name, "minecraft:")
	resolved := lookupPoolAlias(name, aliases)
	pool, err := r.pool(resolved)
	if err != nil {
		return nil, err
	}
	if pool == nil && resolved != name {
		pool, err = r.pool(name)
	}
	if pool == nil && err == nil {
		err = fmt.Errorf("Java start pool %q: %w", name, fs.ErrNotExist)
	}
	return pool, err
}
func (r *jigsawPoolRegistry) elementParts(element *poolElement) ([]jigsawPart, error) {
	if parts, loaded := r.parts[element]; loaded {
		return parts, nil
	}
	var parts []jigsawPart
	switch {
	case element.empty:
		parts = []jigsawPart{{}}
	case len(element.children) > 0:
		for i := range element.children {
			child, err := r.elementParts(&element.children[i])
			if err != nil {
				return nil, err
			}
			parts = append(parts, child...)
		}
	case element.feature != nil:
		parts = []jigsawPart{{feature: element.feature}}
	default:
		template, loaded := r.templates[element.template]
		if !loaded {
			var err error
			template, err = LoadOrCreate(element.template)
			if err != nil {
				return nil, err
			}
			r.templates[element.template] = template
		}
		parts = []jigsawPart{{template: template, processors: element.processors, processorSpecs: element.processorSpecs, liquidSettings: element.liquidSettings, overrideLiquidSettings: element.overrideLiquidSettings, legacySingle: element.legacySingle}}
	}
	r.parts[element] = parts
	return parts, nil
}
func (r *jigsawPoolRegistry) maxPoolSize(pool *jigsawPool) (int32, error) {
	if pool == nil {
		return 0, nil
	}
	if pool.maxSizeKnown {
		return pool.maxSize, nil
	}
	var size int32
	for i := range pool.elements {
		element := &pool.elements[i]
		if element.empty {
			continue
		}
		parts, err := r.elementParts(element)
		if err != nil {
			return 0, err
		}
		if !hasPoolPlacement(parts) {
			return 0, fmt.Errorf("Java pool %s: list element has no bounding box", pool.name)
		}
		bounds := makeJigsawPoolPiece(parts, [3]int32{}, None, element, ApplyWaterlogging).Bounds
		size = max(size, bounds.Max[1]-bounds.Min[1]+1)
	}
	pool.maxSize, pool.maxSizeKnown = size, true
	return size, nil
}
