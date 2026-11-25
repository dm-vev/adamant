package api

import (
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

const defaultHologramLineSpacing = 0.28

var hologramEntityType = NewCustomEntityType(CustomEntitySpec{
	Name:        "adamant:hologram",
	NetworkType: "minecraft:armor_stand",
	BBox:        cube.Box(0, 0, 0, 0, 0, 0),
	CanDespawn:  false,
	OnMeta: func(e world.Entity, patch *world.ActorMetaPatch) {
		inv := true
		always := true
		noAI := true
		scale := float32(0.001)
		zero := float32(0)
		patch.Invisible = &inv
		patch.AlwaysShowNameTag = &always
		patch.NoAI = &noAI
		patch.Scale = &scale
		patch.Width = &zero
		patch.Height = &zero
	},
})

// HologramSpec configures a multi-line floating text hologram.
type HologramSpec struct {
	Position    mgl64.Vec3
	Lines       []string
	LineSpacing float64
}

// Hologram manages a stack of invisible nametag entities rendering floating
// text in a world. Call methods from inside a transaction on the hologram's world.
type Hologram struct {
	pos     mgl64.Vec3
	spacing float64
	lines   []string
	handles []*world.EntityHandle
}

// SpawnHologram spawns a floating multi-line text hologram at pos in tx.
func SpawnHologram(tx *world.Tx, pos mgl64.Vec3, lines ...string) *Hologram {
	return NewHologram(tx, HologramSpec{Position: pos, Lines: lines})
}

// NewHologram spawns a Hologram from spec into tx.
func NewHologram(tx *world.Tx, spec HologramSpec) *Hologram {
	spacing := spec.LineSpacing
	if spacing <= 0 {
		spacing = defaultHologramLineSpacing
	}
	h := &Hologram{
		pos:     spec.Position,
		spacing: spacing,
	}
	if tx != nil && !tx.Closed() {
		h.SetLines(tx, spec.Lines...)
	}
	return h
}

// Position returns the anchor position of the bottom line of the hologram.
func (h *Hologram) Position() mgl64.Vec3 {
	if h == nil {
		return mgl64.Vec3{}
	}
	return h.pos
}

// Lines returns a copy of the hologram's current lines (top to bottom).
func (h *Hologram) Lines() []string {
	if h == nil {
		return nil
	}
	return slices.Clone(h.lines)
}

// Handles returns a copy of the underlying entity handles for each line.
func (h *Hologram) Handles() []*world.EntityHandle {
	if h == nil {
		return nil
	}
	return slices.Clone(h.handles)
}

func (h *Hologram) linePosition(index, total int) mgl64.Vec3 {
	offsetY := float64(total-1-index) * h.spacing
	return mgl64.Vec3{h.pos[0], h.pos[1] + offsetY, h.pos[2]}
}

// SetLines updates the hologram's displayed lines (top to bottom), reusing
// existing line entities where possible and spawning/closing any difference.
func (h *Hologram) SetLines(tx *world.Tx, lines ...string) {
	if h == nil || tx == nil || tx.Closed() {
		return
	}
	total := len(lines)
	for i := total; i < len(h.handles); i++ {
		if ent, ok := h.handles[i].Entity(tx); ok {
			tx.RemoveEntity(ent)
		}
		_ = h.handles[i].Close()
	}
	if len(h.handles) > total {
		h.handles = h.handles[:total]
	}
	for i, text := range lines {
		pos := h.linePosition(i, total)
		if i < len(h.handles) {
			if ent, ok := h.handles[i].Entity(tx); ok {
				if ce, ok := ent.(*CustomEntity); ok {
					ce.SetNameTag(text)
					ce.Teleport(pos)
					continue
				}
			}
		}
		handle := hologramEntityType.Spawn(world.EntitySpawnOpts{
			Position: pos,
			NameTag:  text,
		})
		ent := tx.AddEntity(handle)
		if i < len(h.handles) {
			h.handles[i] = ent.H()
		} else {
			h.handles = append(h.handles, ent.H())
		}
	}
	h.lines = slices.Clone(lines)
}

// Move moves the entire hologram stack to pos in tx.
func (h *Hologram) Move(tx *world.Tx, pos mgl64.Vec3) {
	if h == nil || tx == nil || tx.Closed() {
		return
	}
	h.pos = pos
	total := len(h.handles)
	for i, handle := range h.handles {
		if ent, ok := handle.Entity(tx); ok {
			if ce, ok := ent.(*CustomEntity); ok {
				ce.Teleport(h.linePosition(i, total))
			}
		}
	}
}

// Close removes all hologram line entities from tx.
func (h *Hologram) Close(tx *world.Tx) {
	if h == nil {
		return
	}
	for _, handle := range h.handles {
		if tx != nil && !tx.Closed() {
			if ent, ok := handle.Entity(tx); ok {
				tx.RemoveEntity(ent)
			}
		}
		_ = handle.Close()
	}
	h.handles = nil
	h.lines = nil
}
