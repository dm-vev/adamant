package world

import (
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// StructureRotation is a clockwise rotation of a structure around the
// vertical axis, viewed from above, as Java's Rotation.
type StructureRotation uint8

const (
	StructureRotationNone StructureRotation = iota
	StructureRotationClockwise90
	StructureRotationClockwise180
	StructureRotationCounterClockwise90
)

// StructureMirror mirrors a structure, as Java's Mirror.
type StructureMirror uint8

const (
	StructureMirrorNone StructureMirror = iota
	// StructureMirrorLeftRight inverts the Z axis.
	StructureMirrorLeftRight
	// StructureMirrorFrontBack inverts the X axis.
	StructureMirrorFrontBack
)

// TransformStructurePos ports StructureTemplate.transform for block
// positions with the pivot at the origin: the mirror applies first, then the
// rotation.
func TransformStructurePos(pos cube.Pos, mirror StructureMirror, rotation StructureRotation) cube.Pos {
	x, y, z := pos[0], pos[1], pos[2]
	switch mirror {
	case StructureMirrorLeftRight:
		z = -z
	case StructureMirrorFrontBack:
		x = -x
	}
	switch rotation {
	case StructureRotationClockwise90:
		return cube.Pos{-z, y, x}
	case StructureRotationClockwise180:
		return cube.Pos{-x, y, -z}
	case StructureRotationCounterClockwise90:
		return cube.Pos{z, y, -x}
	default:
		return cube.Pos{x, y, z}
	}
}

// transformStructureVec3 ports StructureTemplate.transform for entity
// positions with the pivot at the origin. A point inside the block at a
// position ends up inside the block that position transforms to.
func transformStructureVec3(pos mgl64.Vec3, mirror StructureMirror, rotation StructureRotation) mgl64.Vec3 {
	x, y, z := pos[0], pos[1], pos[2]
	switch mirror {
	case StructureMirrorLeftRight:
		z = 1 - z
	case StructureMirrorFrontBack:
		x = 1 - x
	}
	switch rotation {
	case StructureRotationClockwise90:
		return mgl64.Vec3{1 - z, y, x}
	case StructureRotationClockwise180:
		return mgl64.Vec3{1 - x, y, 1 - z}
	case StructureRotationCounterClockwise90:
		return mgl64.Vec3{z, y, 1 - x}
	default:
		return mgl64.Vec3{x, y, z}
	}
}

// transformStructureYaw ports Entity.mirror and Entity.rotate.
func transformStructureYaw(yaw float64, mirror StructureMirror, rotation StructureRotation) float64 {
	switch mirror {
	case StructureMirrorLeftRight:
		yaw = 180 - yaw
	case StructureMirrorFrontBack:
		yaw = -yaw
	}
	switch rotation {
	case StructureRotationClockwise90:
		yaw += 90
	case StructureRotationClockwise180:
		yaw += 180
	case StructureRotationCounterClockwise90:
		yaw += 270
	}
	for yaw >= 180 {
		yaw -= 360
	}
	for yaw < -180 {
		yaw += 360
	}
	return yaw
}

// TransformState applies Java's BlockState.mirror and BlockState.rotate to
// the horizontal properties of a Java block state, without modifying the
// state passed.
func TransformState(state BlockState, mirror StructureMirror, rotation StructureRotation) BlockState {
	out := state
	if state.Properties == nil {
		return out
	}
	props := make(map[string]any, len(state.Properties))
	for k, v := range state.Properties {
		if isCardinalDirection(k) {
			k = TransformDirection(k, mirror, rotation)
		}
		if str, ok := v.(string); ok {
			switch {
			case k == "facing":
				v = TransformDirection(str, mirror, rotation)
			case k == "orientation":
				v = transformJavaOrientation(str, mirror, rotation)
			case isRailShape(str):
				v = transformRailShape(str, mirror, rotation)
			}
		}
		props[k] = v
	}
	if axis, ok := props["axis"].(string); ok && (rotation == StructureRotationClockwise90 || rotation == StructureRotationCounterClockwise90) {
		if axis == "x" {
			props["axis"] = "z"
		} else if axis == "z" {
			props["axis"] = "x"
		}
	}
	if value, exists := props["rotation"]; exists {
		n, valid := javaStateInteger(value)
		if valid {
			props["rotation"] = int32(transformSixteenthRotation(uint8(n), mirror, rotation))
		}
	}
	if shape, ok := props["shape"].(string); ok && mirror != StructureMirrorNone {
		if strings.Contains(shape, "_left") {
			props["shape"] = strings.Replace(shape, "_left", "_right", 1)
		} else if strings.Contains(shape, "_right") {
			props["shape"] = strings.Replace(shape, "_right", "_left", 1)
		}
	}
	if hinge, ok := props["hinge"].(string); ok && mirror != StructureMirrorNone {
		if hinge == "left" {
			props["hinge"] = "right"
		} else if hinge == "right" {
			props["hinge"] = "left"
		}
	}
	out.Properties = props
	return out
}

// javaStateInteger reads an integer property value, which Java states may hold
// as a number or as its decimal text.
func javaStateInteger(value any) (int64, bool) {
	switch v := value.(type) {
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	case int:
		return int64(v), true
	case int8:
		return int64(v), true
	case int16:
		return int64(v), true
	case int32:
		return int64(v), true
	case int64:
		return v, true
	case uint8:
		return int64(v), true
	case uint16:
		return int64(v), true
	case uint32:
		return int64(v), true
	}
	return 0, false
}

func isCardinalDirection(d string) bool {
	return d == "north" || d == "south" || d == "east" || d == "west"
}

// TransformDirection mirrors and rotates a horizontal Java direction name.
// Other values, like up and down, are returned unchanged.
func TransformDirection(direction string, mirror StructureMirror, rotation StructureRotation) string {
	var vector cube.Pos
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
	transformed := TransformStructurePos(vector, mirror, rotation)
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

func transformJavaOrientation(value string, mirror StructureMirror, rotation StructureRotation) string {
	parts := strings.Split(value, "_")
	for i, part := range parts {
		parts[i] = TransformDirection(part, mirror, rotation)
	}
	return strings.Join(parts, "_")
}

// transformSixteenthRotation transforms Java's 16-step rotation property,
// which is south at 0 and increases clockwise.
func transformSixteenthRotation(value uint8, mirror StructureMirror, rotation StructureRotation) uint8 {
	v := int(value & 15)
	if mirror == StructureMirrorLeftRight {
		v = (8 - v) & 15
	} else if mirror == StructureMirrorFrontBack {
		v = (16 - v) & 15
	}
	switch rotation {
	case StructureRotationClockwise90:
		v += 4
	case StructureRotationClockwise180:
		v += 8
	case StructureRotationCounterClockwise90:
		v += 12
	}
	return uint8(v & 15)
}

func isRailShape(s string) bool {
	return s == "north_south" || s == "east_west" || strings.HasPrefix(s, "ascending_") || strings.Contains(s, "_north") || strings.Contains(s, "_south") || strings.Contains(s, "_east") || strings.Contains(s, "_west")
}

func transformRailShape(s string, m StructureMirror, r StructureRotation) string {
	if s == "north_south" || s == "east_west" {
		if r == StructureRotationClockwise90 || r == StructureRotationCounterClockwise90 {
			if s == "north_south" {
				return "east_west"
			}
			return "north_south"
		}
		return s
	}
	if strings.HasPrefix(s, "ascending_") {
		return "ascending_" + TransformDirection(strings.TrimPrefix(s, "ascending_"), m, r)
	}
	first, second, _ := strings.Cut(s, "_")
	first, second = TransformDirection(first, m, r), TransformDirection(second, m, r)
	// Java's corner names always put the north/south direction first.
	if first == "east" || first == "west" {
		first, second = second, first
	}
	return first + "_" + second
}
