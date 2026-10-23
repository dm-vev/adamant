package world

import (
	"cmp"
	"maps"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// StructureAnimation spreads placing a structure over time, as the animation
// modes of Bedrock's structure blocks.
type StructureAnimation uint8

const (
	StructureAnimationNone StructureAnimation = iota
	// StructureAnimationByLayer places the structure one horizontal layer at a
	// time, bottom up.
	StructureAnimationByLayer
	// StructureAnimationByBlock places the structure one block at a time.
	StructureAnimationByBlock
)

// StructurePlacement configures placing a StructureTemplate, ported from
// Java's StructurePlaceSettings.
type StructurePlacement struct {
	Mirror   StructureMirror
	Rotation StructureRotation
	// Integrity is the chance, from 0 to 1, that each block of the template is
	// placed, as BlockRotProcessor. Set it to 1 to place every block.
	Integrity float64
	// Seed seeds which blocks Integrity leaves out and the loot of the
	// containers placed. Zero picks a random seed.
	Seed int64
	// IgnoreEntities leaves out the entities of the template.
	IgnoreEntities bool
	// IgnoreBlocks leaves out the blocks of the template, placing only its
	// entities.
	IgnoreBlocks bool
	// Animation and AnimationDuration spread placing the blocks over time.
	Animation         StructureAnimation
	AnimationDuration time.Duration
	// KnownShape places the blocks as they are, without block updates around
	// them.
	KnownShape bool
}

// CaptureStructure ports StructureTemplate.fillFromWorld: it copies the
// blocks, with their block entity data, of the box with its lowest corner at
// origin and the size passed, leaving out structure voids. If entities is
// true, the entities in the box other than players are copied too.
func (tx *Tx) CaptureStructure(origin cube.Pos, size [3]int, entities bool) *StructureTemplate {
	t := NewStructureTemplate(size)
	t.origin = origin
	if size[0] < 1 || size[1] < 1 || size[2] < 1 {
		return t
	}
	w := tx.World()
	r := tx.Range()
	for i := range t.blocks {
		rel := t.position(i)
		pos := origin.Add(rel)
		if pos.OutOfBounds(r) {
			t.blocks[i] = w.conf.Blocks.Air()
			continue
		}
		b := tx.Block(pos)
		if name, _ := b.EncodeBlock(); name == "minecraft:structure_void" {
			continue
		}
		var l Liquid
		if _, liquid := b.(Liquid); !liquid {
			l, _ = tx.Liquid(pos)
		}
		var data map[string]any
		if n, ok := b.(NBTer); ok {
			data = n.EncodeNBT()
		}
		t.SetBlock(rel, b, l, data)
	}
	if entities {
		// Java takes the entities whose bounding box overlaps the box.
		box := cube.Box(float64(origin[0]), float64(origin[1]), float64(origin[2]), float64(origin[0]+size[0]), float64(origin[1]+size[1]), float64(origin[2]+size[2]))
		for e := range tx.EntitiesWithin(box.Grow(4)) {
			if e.H().Type().EncodeEntity() == "minecraft:player" || !e.H().Type().BBox(e).Translate(e.Position()).IntersectsWith(box) {
				continue
			}
			t.entities = append(t.entities, w.EntityNBT(e.H()))
		}
	}
	return t
}

// structurePlacement is a block of a template on its way into the world.
type structurePlacement struct {
	pos  cube.Pos
	b    Block
	l    Liquid
	data map[string]any
}

// PlaceStructure ports StructureTemplate.placeInWorld: it places the
// template with the position of its lowest corner at pos, mirrored and
// rotated around pos. Structure voids leave the world untouched. Blocks are
// placed without block updates, which their neighbours receive once the
// blocks around them are placed. It reports false, placing nothing, for an
// empty template or one with only entities that are ignored.
func (tx *Tx) PlaceStructure(t *StructureTemplate, pos cube.Pos, p StructurePlacement) bool {
	hasBlocks := slices.ContainsFunc(t.blocks, func(b Block) bool { return b != nil })
	if t.size[0] < 1 || t.size[1] < 1 || t.size[2] < 1 || !hasBlocks && (p.IgnoreEntities || len(t.entities) == 0) {
		return false
	}
	w := tx.World()
	seed := p.Seed
	if seed == 0 {
		// StructureBlockEntity.createRandom seeds random placements with the
		// time.
		seed = time.Now().UnixMilli()
	}
	integrity := vanilla.NewLegacyRandom(seed)
	loot := vanilla.NewLegacyRandom(seed)
	var placements []structurePlacement
	if !p.IgnoreBlocks {
		r := tx.Range()
		for i, b := range t.blocks {
			if b == nil {
				continue
			}
			if p.Integrity < 1 && !(integrity.NextFloat() <= float32(p.Integrity)) {
				continue
			}
			rel := t.position(i)
			target := pos.Add(TransformStructurePos(rel, p.Mirror, p.Rotation))
			if target.OutOfBounds(r) {
				continue
			}
			placement := structurePlacement{pos: target, b: b, l: t.liquids[i]}
			if data, ok := t.actors[i]; ok {
				placement.data = transformStructureActor(data, t.origin.Add(rel), target, p.Mirror, p.Rotation)
				if _, ok := placement.data["LootTable"]; ok {
					placement.data["LootTableSeed"] = loot.NextLong()
				}
			}
			if p.Mirror != StructureMirrorNone || p.Rotation != StructureRotationNone {
				placement.b = transformStructureBlock(w.conf.Blocks, b, p.Mirror, p.Rotation)
			}
			placements = append(placements, placement)
		}
	}
	placeEntities := func(tx *Tx) {
		if !p.IgnoreEntities {
			tx.placeStructureEntities(t, pos, p.Mirror, p.Rotation)
		}
	}
	if p.Animation == StructureAnimationNone || p.AnimationDuration <= 0 || len(placements) < 2 {
		tx.placeStructureBlocks(placements, p.KnownShape)
		placeEntities(tx)
		return true
	}
	// Bedrock animates structures bottom up.
	slices.SortStableFunc(placements, func(a, b structurePlacement) int {
		return cmp.Or(cmp.Compare(a.pos[1], b.pos[1]), cmp.Compare(a.pos[0], b.pos[0]), cmp.Compare(a.pos[2], b.pos[2]))
	})
	var steps [][]structurePlacement
	for i, placement := range placements {
		if i == 0 || p.Animation == StructureAnimationByBlock || placement.pos[1] != placements[i-1].pos[1] {
			steps = append(steps, nil)
		}
		steps[len(steps)-1] = append(steps[len(steps)-1], placement)
	}
	interval := p.AnimationDuration / time.Duration(len(steps))
	tx.placeStructureBlocks(steps[0], p.KnownShape)
	for i, step := range steps[1:] {
		last := i == len(steps)-2
		w.DoAfter(interval*time.Duration(i+1), func(tx *Tx) {
			tx.placeStructureBlocks(step, p.KnownShape)
			if last {
				placeEntities(tx)
			}
		})
	}
	return true
}

// placeStructureBlocks sets the blocks of a structure, then sends block
// updates around them unless their shape is known.
func (tx *Tx) placeStructureBlocks(placements []structurePlacement, knownShape bool) {
	opts := &SetOpts{DisableBlockUpdates: true}
	reg := tx.World().conf.Blocks
	for _, p := range placements {
		b := p.b
		if p.data != nil {
			if n, ok := b.(NBTer); ok {
				b = DecodeNBT(n, p.data, reg).(Block)
			}
		}
		tx.SetBlock(p.pos, b, opts)
		if p.l != nil {
			tx.SetLiquid(p.pos, p.l)
		}
	}
	if knownShape {
		return
	}
	for _, p := range placements {
		tx.DoBlockUpdatesAround(p.pos)
	}
}

// placeStructureEntities ports StructureTemplate.placeEntities: the entities
// of the template are placed with fresh identities, mirrored and rotated
// around pos.
func (tx *Tx) placeStructureEntities(t *StructureTemplate, pos cube.Pos, mirror StructureMirror, rotation StructureRotation) {
	w := tx.World()
	for _, source := range t.entities {
		id, _ := source["identifier"].(string)
		typ, ok := w.conf.Entities.Lookup(id)
		if !ok || id == "minecraft:player" {
			continue
		}
		data := maps.Clone(source)
		if javaData, ok := data["adamant:java"].(map[string]any); ok {
			// The copy is not the original entity, and does not ride its
			// vehicle.
			javaData = maps.Clone(javaData)
			delete(javaData, "UUID")
			delete(javaData, "VehicleUUID")
			data["adamant:java"] = javaData
		}
		delete(data, "LinksTag")
		h := w.entityFromNBT(typ, rand.Int64(), data)
		h.data.Pos = pos.Vec3().Add(transformStructureVec3(t.structureEntityPos(source), mirror, rotation))
		h.data.Rot[0] = transformStructureYaw(h.data.Rot[0], mirror, rotation)
		tx.AddEntity(h)
	}
}

// transformStructureBlock mirrors and rotates the state of a block through
// its Java block state, which Java's BlockState.mirror and rotate act on.
func transformStructureBlock(reg BlockRegistry, b Block, mirror StructureMirror, rotation StructureRotation) Block {
	state, err := CanonicalState(reg, NativeState{Block: reg.BlockRuntimeID(b), Liquid: reg.AirRuntimeID()})
	if err != nil {
		return b
	}
	native, err := ResolveState(reg, TransformState(state, mirror, rotation))
	if err != nil {
		return b
	}
	if transformed, ok := reg.BlockByRuntimeID(native.Block); ok {
		return transformed
	}
	return b
}

// transformStructureActor returns a copy of the block entity data of a block
// moved from src to dst, mirrored and rotated. Bedrock keeps chest pairs as
// absolute positions and some orientations outside of block states.
func transformStructureActor(data map[string]any, src, dst cube.Pos, mirror StructureMirror, rotation StructureRotation) map[string]any {
	data = maps.Clone(data)
	data["x"], data["y"], data["z"] = int32(dst[0]), int32(dst[1]), int32(dst[2])
	if x, ok := data["pairx"].(int32); ok {
		if z, ok := data["pairz"].(int32); ok {
			pair := TransformStructurePos(cube.Pos{int(x) - src[0], 0, int(z) - src[2]}, mirror, rotation)
			data["pairx"], data["pairz"] = int32(dst[0]+pair[0]), int32(dst[2]+pair[2])
		}
	}
	if mirror == StructureMirrorNone && rotation == StructureRotationNone {
		return data
	}
	if yaw, ok := data["Rotation"].(float32); ok {
		// Floor skulls keep their sixteenth rotation as a yaw.
		data["Rotation"] = float32(transformStructureYaw(float64(yaw), mirror, rotation))
	}
	if id, _ := data["id"].(string); id == "ShulkerBox" {
		// Bedrock shulker boxes face where their block entity says.
		if facing, ok := data["facing"].(uint8); ok && facing > 1 && facing < 6 {
			v := TransformStructurePos(cube.Pos{}.Side(cube.Face(facing)), mirror, rotation)
			for f := cube.FaceNorth; f <= cube.FaceEast; f++ {
				if (cube.Pos{}).Side(f) == v {
					data["facing"] = uint8(f)
				}
			}
		}
	}
	return data
}
