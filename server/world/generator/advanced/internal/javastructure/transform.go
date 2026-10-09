package javastructure

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Rotation is a clockwise rotation around the template origin, viewed from above.
type Rotation uint8

const (
	None Rotation = iota
	Clockwise90
	Clockwise180
	CounterClockwise90
)

// Mirror matches Java StructurePlaceSettings.Mirror.
type Mirror uint8

const (
	NoMirror  Mirror = iota
	LeftRight        // invert Z
	FrontBack        // invert X
)

// Bounds is an inclusive world-space clip cuboid.
type Bounds struct{ Min, Max [3]int32 }

// TransformPosition applies mirror first and then rotation, both around the origin.
func TransformPosition(pos [3]int32, mirror Mirror, rotation Rotation) [3]int32 {
	x, y, z := pos[0], pos[1], pos[2]
	switch mirror {
	case LeftRight:
		z = -z
	case FrontBack:
		x = -x
	}
	switch rotation {
	case Clockwise90:
		return [3]int32{-z, y, x}
	case Clockwise180:
		return [3]int32{-x, y, -z}
	case CounterClockwise90:
		return [3]int32{z, y, -x}
	default:
		return [3]int32{x, y, z}
	}
}

// TransformState applies Java's horizontal block-state rotation/mirror properties without modifying the input.
func TransformState(state world.BlockState, mirror Mirror, rotation Rotation) world.BlockState {
	out := state
	if state.Properties == nil {
		return out
	}
	props := make(map[string]any, len(state.Properties))
	for k, v := range state.Properties {
		if isCardinal(k) {
			k = transformDirection(k, mirror, rotation)
		}
		if str, ok := v.(string); ok {
			switch {
			case k == "facing":
				v = transformDirection(str, mirror, rotation)
			case k == "orientation":
				v = transformOrientation(str, mirror, rotation)
			case isRailShape(str):
				v = transformRail(str, mirror, rotation)
			}
		}
		props[k] = v
	}
	if axis, ok := props["axis"].(string); ok && (rotation == Clockwise90 || rotation == CounterClockwise90) {
		if axis == "x" {
			props["axis"] = "z"
		} else if axis == "z" {
			props["axis"] = "x"
		}
	}
	if value, exists := props["rotation"]; exists {
		n, valid := integer(value)
		if text, ok := value.(string); ok {
			var err error
			n, err = strconv.ParseInt(text, 10, 64)
			valid = err == nil
		}
		if valid {
			props["rotation"] = int32(transformRotation(uint8(n), mirror, rotation))
		}
	}
	if shape, ok := props["shape"].(string); ok && mirror != NoMirror {
		if strings.Contains(shape, "_left") {
			props["shape"] = strings.Replace(shape, "_left", "_right", 1)
		} else if strings.Contains(shape, "_right") {
			props["shape"] = strings.Replace(shape, "_right", "_left", 1)
		}
	}
	if hinge, ok := props["hinge"].(string); ok && mirror != NoMirror {
		if hinge == "left" {
			props["hinge"] = "right"
		} else if hinge == "right" {
			props["hinge"] = "left"
		}
	}
	out.Properties = props
	return out
}

func isCardinal(d string) bool { return d == "north" || d == "south" || d == "east" || d == "west" }
func transformDirection(direction string, mirror Mirror, rotation Rotation) string {
	var vector [3]int32
	switch direction {
	case "north":
		vector[2] = -1
	case "south":
		vector[2] = 1
	case "west":
		vector[0] = -1
	case "east":
		vector[0] = 1
	default:
		return direction
	}
	transformed := TransformPosition(vector, mirror, rotation)
	switch {
	case transformed[0] > 0:
		return "east"
	case transformed[0] < 0:
		return "west"
	case transformed[2] > 0:
		return "south"
	default:
		return "north"
	}
}

func transformOrientation(value string, mirror Mirror, rotation Rotation) string {
	parts := strings.Split(value, "_")
	for i, part := range parts {
		parts[i] = transformDirection(part, mirror, rotation)
	}
	return strings.Join(parts, "_")
}

func transformRotation(value uint8, mirror Mirror, rotation Rotation) uint8 {
	// Java's 16-step rotation property has south=0 and increases clockwise.
	v := int(value & 15)
	if mirror == LeftRight {
		v = (8 - v) & 15
	} else if mirror == FrontBack {
		v = (16 - v) & 15
	}
	switch rotation {
	case Clockwise90:
		v += 4
	case Clockwise180:
		v += 8
	case CounterClockwise90:
		v += 12
	}
	return uint8(v & 15)
}

func isRailShape(s string) bool {
	return s == "north_south" || s == "east_west" || strings.HasPrefix(s, "ascending_") || strings.Contains(s, "_north") || strings.Contains(s, "_south") || strings.Contains(s, "_east") || strings.Contains(s, "_west")
}
func transformRail(s string, m Mirror, r Rotation) string {
	if s == "north_south" || s == "east_west" {
		if r == Clockwise90 || r == CounterClockwise90 {
			if s == "north_south" {
				return "east_west"
			}
			return "north_south"
		}
		return s
	}
	parts := strings.Split(s, "_")
	for i, part := range parts {
		parts[i] = transformDirection(part, m, r)
	}
	return strings.Join(parts, "_")
}

// ProcessContext provides the stable template-space and world-space coordinates and deterministic placement seed.
type ProcessContext struct {
	Origin, Position, RelativePosition [3]int32
	BlockIndex                         int
	Seed                               int64
	Rotation                           Rotation
	Mirror                             Mirror
}

// Processor implements one Java template processor. EvaluatesEntirePieceState
// disables Java's initial chunk-local filtering before per-block processing.
type Processor struct {
	Process func(ProcessContext, Block) (block Block, keep bool, err error)
	// finalize runs after every per-block processor, before state transforms and clipping.
	// Original palette indices avoid copying complete contexts for surviving records.
	finalize                  func(ProcessContext, []Block, []int, []Block) ([]Block, error)
	EvaluatesEntirePieceState bool
}

func processor(fn func(ProcessContext, Block) (Block, bool, error)) Processor {
	return Processor{Process: fn}
}

// Placement contains transformed records and generation-time updates associated with them.
type Placement struct {
	Blocks               []Block
	Entities             []Entity
	PostProcessing       []cube.Pos
	ScheduledTicks       []chunk.ScheduledTick
	StructurePieceWrites bool
}

// Place selects one palette, transforms block positions, processes records in order,
// then transforms states and applies the final clip. As in Java, processors marked
// EvaluatesEntirePieceState disable the earlier chunk-local processing filter.
// The origin is the world position of the untransformed template origin; bounds are inclusive.
func (t *Template) Place(palette int, origin [3]int32, rotation Rotation, mirror Mirror, clip Bounds, seed int64, processors ...Processor) (Placement, error) {
	return t.place(palette, origin, rotation, mirror, clip, seed, true, processors...)
}

// PlaceBlocks places only palette blocks, avoiding entity transformation for preview generation.
func (t *Template) PlaceBlocks(palette int, origin [3]int32, rotation Rotation, mirror Mirror, clip Bounds, seed int64, processors ...Processor) (Placement, error) {
	return t.place(palette, origin, rotation, mirror, clip, seed, false, processors...)
}

func (t *Template) place(palette int, origin [3]int32, rotation Rotation, mirror Mirror, clip Bounds, seed int64, includeEntities bool, processors ...Processor) (Placement, error) {
	if len(t.Palettes) == 0 {
		return Placement{}, nil
	}
	if palette < 0 || palette >= len(t.Palettes) {
		return Placement{}, fmt.Errorf("java structure: palette %d outside [0,%d)", palette, len(t.Palettes))
	}
	out := Placement{Blocks: make([]Block, 0, len(t.Palettes[palette]))}
	processEntirePiece, hasFinalizer := false, false
	for _, processor := range processors {
		processEntirePiece = processEntirePiece || processor.EvaluatesEntirePieceState
		hasFinalizer = hasFinalizer || processor.finalize != nil
	}
	var originalIndices []int
	if hasFinalizer {
		originalIndices = make([]int, 0, len(t.Palettes[palette]))
	}
	for blockIndex, source := range t.Palettes[palette] {
		pos := TransformPosition(source.Pos, mirror, rotation)
		for axis := range 3 {
			pos[axis] += origin[axis]
		}
		if !processEntirePiece && !inside(pos, clip) {
			continue
		}
		block := source
		block.Pos = pos
		ctx := ProcessContext{
			Origin: origin, Position: pos, RelativePosition: source.Pos, BlockIndex: blockIndex,
			Seed: seed, Rotation: rotation, Mirror: mirror,
		}
		keep := true
		for _, processor := range processors {
			if processor.Process == nil {
				continue
			}
			ctx.Position = block.Pos
			var err error
			block, keep, err = processor.Process(ctx, block)
			if err != nil {
				return Placement{}, fmt.Errorf("java structure processor at %v: %w", ctx.Position, err)
			}
			if !keep {
				break
			}
		}
		if keep {
			out.Blocks = append(out.Blocks, block)
			if hasFinalizer {
				originalIndices = append(originalIndices, blockIndex)
			}
		}
	}
	for _, processor := range processors {
		if processor.finalize != nil {
			var err error
			ctx := ProcessContext{Origin: origin, Seed: seed, Rotation: rotation, Mirror: mirror}
			out.Blocks, err = processor.finalize(ctx, t.Palettes[palette], originalIndices, out.Blocks)
			if err != nil {
				return Placement{}, fmt.Errorf("java structure finalizer at %v: %w", origin, err)
			}
		}
	}
	placed := out.Blocks[:0]
	for _, block := range out.Blocks {
		if !inside(block.Pos, clip) {
			continue
		}
		block.State = TransformState(block.State, mirror, rotation)
		nativeBlock, adapted, err := NativeBlockForJavaState(block.State)
		if err != nil {
			return Placement{}, fmt.Errorf("java structure state at %v: %w", block.Pos, err)
		}
		if adapted {
			block.NativeBlock = nativeBlock
		} else if stateDerivedActor(block.NativeBlock) {
			block.NativeBlock = nil
		}
		placed = append(placed, block)
	}
	out.Blocks = placed
	if includeEntities {
		out.Entities = t.PlaceEntities(origin, rotation, mirror, clip, seed)
	}
	return out, nil
}

// PlaceEntities transforms only entity records and clips them to a world-space box.
// It is useful for structure pieces whose block records are handled by another generator.
func (t *Template) PlaceEntities(origin [3]int32, rotation Rotation, mirror Mirror, clip Bounds, seed int64) []Entity {
	out := make([]Entity, 0, len(t.Entities))
	for _, source := range t.Entities {
		bp := TransformPosition(source.BlockPos, mirror, rotation)
		for axis := range 3 {
			bp[axis] += origin[axis]
		}
		if !inside(bp, clip) {
			continue
		}
		e := source
		e.BlockPos = bp
		e.Seed = seed
		x := transformDouble(source.Pos[0], source.Pos[2], mirror, rotation)
		e.Pos = [3]float64{x[0] + float64(origin[0]), source.Pos[1] + float64(origin[1]), x[1] + float64(origin[2])}
		e.NBT = transformEntityNBT(source.NBT, mirror, rotation, e.Pos, bp)
		out = append(out, e)
	}
	return out
}

// EntitiesInBounds returns stored entity records whose block positions are inside the supplied template-space box.
func (t *Template) EntitiesInBounds(bounds Bounds) []Entity {
	out := make([]Entity, 0, len(t.Entities))
	for _, entity := range t.Entities {
		if inside(entity.BlockPos, bounds) {
			out = append(out, entity)
		}
	}
	return out
}

func transformDouble(x, z float64, mirror Mirror, rotation Rotation) [2]float64 {
	switch mirror {
	case LeftRight:
		z = 1 - z
	case FrontBack:
		x = 1 - x
	}
	switch rotation {
	case Clockwise90:
		return [2]float64{1 - z, x}
	case Clockwise180:
		return [2]float64{1 - x, 1 - z}
	case CounterClockwise90:
		return [2]float64{z, 1 - x}
	default:
		return [2]float64{x, z}
	}
}
func transformVector(x, z float64, mirror Mirror, rotation Rotation) [2]float64 {
	switch rotation {
	case Clockwise90:
		x, z = -z, x
	case Clockwise180:
		x, z = -x, -z
	case CounterClockwise90:
		x, z = z, -x
	}
	switch mirror {
	case LeftRight:
		z = -z
	case FrontBack:
		x = -x
	}
	return [2]float64{x, z}
}
func inside(p [3]int32, b Bounds) bool {
	for axis := range 3 {
		if p[axis] < b.Min[axis] || p[axis] > b.Max[axis] {
			return false
		}
	}
	return true
}
