package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ResinClump is a multi-face block similar to lichen.
type ResinClump struct {
	transparent
	replaceable
	empty
	sourceWaterDisplacer

	Down  bool
	Up    bool
	North bool
	South bool
	West  bool
	East  bool
}

// BreakInfo ...
func (r ResinClump) BreakInfo() BreakInfo {
	return newBreakInfo(0.2, func(t item.Tool) bool {
		return t.ToolType() == item.TypeAxe || t.ToolType() == item.TypeShears
	}, func(t item.Tool) bool {
		return t.ToolType() == item.TypeAxe || t.ToolType() == item.TypeShears
	}, oneOf(r))
}

// UseOnBlock ...
func (r ResinClump) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, r)
	if !used {
		return false
	}

	target := tx.Block(pos.Side(face.Opposite()))
	if !target.Model().FaceSolid(pos.Side(face.Opposite()), face, tx) {
		return false
	}

	if existing, ok := tx.Block(pos).(ResinClump); ok {
		r = existing
	}
	r = r.withFace(face, true)

	place(tx, pos, r, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (ResinClump) EncodeItem() (name string, meta int16) {
	return "minecraft:resin_clump", 0
}

// EncodeBlock ...
func (r ResinClump) EncodeBlock() (string, map[string]any) {
	var bits uint8
	if r.Down {
		bits |= 1 << 0
	}
	if r.Up {
		bits |= 1 << 1
	}
	if r.South {
		bits |= 1 << 2
	}
	if r.West {
		bits |= 1 << 3
	}
	if r.North {
		bits |= 1 << 4
	}
	if r.East {
		bits |= 1 << 5
	}
	return "minecraft:resin_clump", map[string]any{"multi_face_direction_bits": int32(bits)}
}

func (r ResinClump) withFace(face cube.Face, attached bool) ResinClump {
	switch face {
	case cube.FaceDown:
		r.Down = attached
	case cube.FaceUp:
		r.Up = attached
	case cube.FaceNorth:
		r.North = attached
	case cube.FaceSouth:
		r.South = attached
	case cube.FaceWest:
		r.West = attached
	case cube.FaceEast:
		r.East = attached
	}
	return r
}

func (r ResinClump) directionBits() uint8 {
	var bits uint8
	if r.Down {
		bits |= 1 << 0
	}
	if r.Up {
		bits |= 1 << 1
	}
	if r.South {
		bits |= 1 << 2
	}
	if r.West {
		bits |= 1 << 3
	}
	if r.North {
		bits |= 1 << 4
	}
	if r.East {
		bits |= 1 << 5
	}
	return bits
}

// allResinClumps returns all resin clump states.
func allResinClumps() (b []world.Block) {
	for i := 0; i < 64; i++ {
		b = append(b, ResinClump{
			Down:  i&(1<<0) != 0,
			Up:    i&(1<<1) != 0,
			South: i&(1<<2) != 0,
			West:  i&(1<<3) != 0,
			North: i&(1<<4) != 0,
			East:  i&(1<<5) != 0,
		})
	}
	return
}
