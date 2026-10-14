package javastructure

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
)

type connector struct {
	pos                                   [3]int32
	placementPriority, selectionPriority  int32
	wildcard                              bool
	front, top, name, target, pool, joint string
}
type jigsawPaletteKey struct {
	template *Template
	palette  int
}

func decodeJigsawConnectors(blocks []Block) ([]connector, error) {
	var out []connector
	for i := range blocks {
		block := &blocks[i]
		if block.State.Name != "minecraft:jigsaw" {
			continue
		}
		if block.NBT == nil {
			return nil, fmt.Errorf("Java jigsaw at %v has no NBT", block.Pos)
		}
		orientation, _ := block.State.Properties["orientation"].(string)
		front, top, _ := strings.Cut(orientation, "_")
		joint, _ := block.NBT["joint"].(string)
		if joint != "aligned" && joint != "rollable" {
			joint = "aligned"
			if front == "up" || front == "down" {
				joint = "rollable"
			}
		}
		name, ok := block.NBT["name"].(string)
		if !ok {
			name = "minecraft:empty"
		}
		target, ok := block.NBT["target"].(string)
		if !ok {
			target = "minecraft:empty"
		}
		pool, ok := block.NBT["pool"].(string)
		if !ok {
			pool = "minecraft:empty"
		}
		placement, _ := int32Value(block.NBT["placement_priority"])
		selection, _ := int32Value(block.NBT["selection_priority"])
		out = append(out, connector{pos: block.Pos, front: front, top: top, name: name, target: target, pool: pool, joint: joint, placementPriority: placement, selectionPriority: selection})
	}
	// StructureTemplate.load sorts block-entity records before caching jigsaws.
	slices.SortStableFunc(out, func(a, b connector) int {
		if order := cmp.Compare(a.pos[1], b.pos[1]); order != 0 {
			return order
		}
		if order := cmp.Compare(a.pos[0], b.pos[0]); order != 0 {
			return order
		}
		return cmp.Compare(a.pos[2], b.pos[2])
	})
	return out, nil
}

func (r *jigsawPoolRegistry) shuffledConnectors(out []connector, parts []jigsawPart, origin [3]int32, rotation Rotation, mirror Mirror, random *java26.Random) ([]connector, error) {
	out = out[:0]
	// ListPoolElement delegates connector selection to its first child only.
	if len(parts) == 0 {
		return out, nil
	}
	part := &parts[0]
	if part.feature != nil {
		return append(out, connector{pos: origin, front: "down", top: "south", joint: "rollable", pool: "minecraft:empty", target: "minecraft:empty", wildcard: true}), nil
	}
	template := part.template
	if template == nil || len(template.Palettes) == 0 {
		return out, nil
	}
	palette := 0
	if len(template.Palettes) > 1 {
		positional := java26.NewLegacyRandom(javaBlockPositionSeed(origin))
		palette = int(positional.NextInt(int32(len(template.Palettes))))
	}
	key := jigsawPaletteKey{template: template, palette: palette}
	raw, loaded := r.connectors[key]
	if !loaded {
		var err error
		raw, err = decodeJigsawConnectors(template.Palettes[palette])
		if err != nil {
			return nil, err
		}
		r.connectors[key] = raw
	}
	out = append(out, raw...)
	for i := range out {
		joint := &out[i]
		joint.pos = TransformPosition(joint.pos, mirror, rotation)
		for axis := range 3 {
			joint.pos[axis] += origin[axis]
		}
		joint.front = world.TransformJavaDirection(joint.front, mirror, rotation)
		joint.top = world.TransformJavaDirection(joint.top, mirror, rotation)
	}
	for i := len(out) - 1; i > 0; i-- {
		j := int(random.NextInt(int32(i + 1)))
		out[i], out[j] = out[j], out[i]
	}
	slices.SortStableFunc(out, func(a, b connector) int { return cmp.Compare(b.selectionPriority, a.selectionPriority) })
	return out, nil
}
