package session

import (
	"fmt"
	"image/color"
	"maps"
	"slices"

	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// SendGameRules sends all the provided game rules to the player. Once sent, they will be immediately updated
// on the client if they are valid.
func (s *Session) sendGameRules(gameRules []mc.GameRule) {
	s.writePacket(&mc.GameRulesChangedPacket{GameRules: gameRules})
}

// EnableCoordinates will either enable or disable coordinates for the player depending on the value given.
func (s *Session) EnableCoordinates(enable bool) {
	//noinspection SpellCheckingInspection
	s.sendGameRules([]mc.GameRule{{Name: "showcoordinates", Value: enable}})
}

// EnableInstantRespawn will either enable or disable instant respawn for the player depending on the value given.
func (s *Session) EnableInstantRespawn(enable bool) {
	//noinspection SpellCheckingInspection
	s.sendGameRules([]mc.GameRule{{Name: "doimmediaterespawn", Value: enable}})
}

// ShowHudElement shows a HUD element to the player if it is not already shown. If the element is waiting to
// be hidden, it will be removed from the updates and remain visible to the player.
func (s *Session) ShowHudElement(e hud.Element) {
	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	if _, ok := s.hiddenHud[e]; ok {
		s.hudUpdates[e] = true
	} else if _, ok = s.hudUpdates[e]; ok {
		delete(s.hudUpdates, e)
	}
}

// HideHudElement hides a HUD element from the player if it is not already hidden. If the element is waiting
// to be shown, it will be removed from the updates and remain hidden from the player.
func (s *Session) HideHudElement(e hud.Element) {
	s.hudMu.Lock()
	defer s.hudMu.Unlock()

	if _, ok := s.hiddenHud[e]; !ok {
		s.hudUpdates[e] = false
	} else if _, ok = s.hudUpdates[e]; ok {
		delete(s.hudUpdates, e)
	}
}

// HudElementHidden checks if a HUD element is currently hidden from the player.
func (s *Session) HudElementHidden(e hud.Element) bool {
	s.hudMu.RLock()
	defer s.hudMu.RUnlock()

	if _, ok := s.hiddenHud[e]; ok {
		return true
	}
	vis, ok := s.hudUpdates[e]
	return ok && !vis
}

// SendHudUpdates sends any pending HUD updates to the player. The updates are batched to reduce the number
// of packets being sent. Up to 2 packets will be sent, one for showing elements and one for hiding elements.
func (s *Session) SendHudUpdates() {
	s.hudMu.Lock()
	if len(s.hudUpdates) == 0 {
		s.hudMu.Unlock()
		return
	}
	var show, hide []int32
	for e, vis := range s.hudUpdates {
		if vis {
			show = append(show, bedrockHudElement(e))
			delete(s.hiddenHud, e)
		} else {
			hide = append(hide, bedrockHudElement(e))
			s.hiddenHud[e] = struct{}{}
		}
	}
	s.hudUpdates = make(map[hud.Element]bool)
	s.hudMu.Unlock()

	if len(show) > 0 {
		s.writePacket(&mc.SetHudPacket{Elements: show, Visibility: mc.HudVisibilityReset})
	}
	if len(hide) > 0 {
		s.writePacket(&mc.SetHudPacket{Elements: hide, Visibility: mc.HudVisibilityHide})
	}
}

// AddDebugShape adds a debug shape to be rendered to the player. If the shape already exists, it will be
// updated with the new information.
func (s *Session) AddDebugShape(shape debug.Shape) {
	if s == Nop {
		return
	}
	s.queueDebugShapeUpdate(debugShapeUpdate{id: shape.ShapeID(), shape: shape})
}

// RemoveDebugShape removes a debug shape from the player by its unique identifier.
func (s *Session) RemoveDebugShape(shape debug.Shape) {
	if s == Nop {
		return
	}
	s.queueDebugShapeUpdate(debugShapeUpdate{id: shape.ShapeID()})
}

// VisibleDebugShapes returns a slice of all debug shapes that are currently being shown to the player.
func (s *Session) VisibleDebugShapes() []debug.Shape {
	s.debugShapesMu.RLock()
	defer s.debugShapesMu.RUnlock()

	return slices.Collect(maps.Values(s.debugShapes))
}

// RemoveAllDebugShapes removes all rendered debug shapes from the player, as well as any shapes that have
// not yet been rendered.
func (s *Session) RemoveAllDebugShapes() {
	if s == Nop {
		return
	}
	s.debugShapesMu.Lock()
	s.debugShapeUpdates = s.debugShapeUpdates[:0]
	for id := range s.debugShapes {
		s.debugShapeUpdates = append(s.debugShapeUpdates, debugShapeUpdate{id: id})
	}
	s.debugShapesMu.Unlock()
}

// SendDebugShapes sends any pending additions/removals of debug shapes to the player. Shapes should be sent
// every tick to allow for batching and time-efficient updates.
func (s *Session) SendDebugShapes(dim world.Dimension) {
	if s == Nop {
		return
	}
	s.debugShapesMu.Lock()
	updates := s.debugShapeUpdates
	if len(updates) == 0 {
		s.debugShapesMu.Unlock()
		return
	}

	shapes := make([]mc.PrimitiveShape, 0, len(updates))
	for _, update := range updates {
		if update.shape == nil {
			delete(s.debugShapes, update.id)
			shapes = append(shapes, mc.PrimitiveShape{
				NetworkID:      uint64(update.id),
				DimensionID:    proto.Some(s.dimensionID(dim)),
				ExtraShapeData: &mc.LastShape{},
			})
			continue
		}
		s.debugShapes[update.id] = update.shape
		shapes = append(shapes, debugShapeToProtocol(update.shape, dim, s.shapeAttachedEntityRuntimeID(update.shape), update.id))
	}
	s.debugShapeUpdates = s.debugShapeUpdates[:0]
	s.debugShapesMu.Unlock()

	s.writePacket(&mc.PrimitiveShapesPacket{Shapes: shapes})
}

// LockInput applies an input lock to the player, disabling the specified input. If the lock is already
// applied, this is a no-op.
func (s *Session) LockInput(l input.Lock) {
	s.inputLocksMu.Lock()
	defer s.inputLocksMu.Unlock()
	s.inputLocks |= bedrockLock(l)
}

// UnlockInput removes an input lock from the player, re-enabling the specified input. If the lock is not
// currently applied, this is a no-op.
func (s *Session) UnlockInput(l input.Lock) {
	s.inputLocksMu.Lock()
	defer s.inputLocksMu.Unlock()
	s.inputLocks &^= bedrockLock(l)
}

// ClearInputLocks removes all input locks from the player, re-enabling all inputs.
func (s *Session) ClearInputLocks() {
	s.inputLocksMu.Lock()
	defer s.inputLocksMu.Unlock()
	s.inputLocks = 0
}

// InputLocked checks if a specific input lock is currently applied to the player.
func (s *Session) InputLocked(l input.Lock) bool {
	s.inputLocksMu.RLock()
	defer s.inputLocksMu.RUnlock()
	return s.inputLocks&bedrockLock(l) != 0
}

// SendInputLocks sends the current input lock state to the client.
func (s *Session) SendInputLocks() {
	s.inputLocksMu.RLock()
	defer s.inputLocksMu.RUnlock()
	s.writePacket(&mc.UpdateClientInputLocksPacket{
		Locks: s.inputLocks,
	})
}

// queueDebugShapeUpdate queues a debug shape mutation to be applied the next time debug shapes are sent.
func (s *Session) queueDebugShapeUpdate(update debugShapeUpdate) {
	s.debugShapesMu.Lock()
	s.debugShapeUpdates = append(s.debugShapeUpdates, update)
	s.debugShapesMu.Unlock()
}

// shapeAttachedEntityRuntimeID returns the runtime ID of the entity attached to a debug shape.
func (s *Session) shapeAttachedEntityRuntimeID(shape debug.Shape) uint64 {
	var handle *world.EntityHandle
	switch shape := shape.(type) {
	case *debug.Arrow:
		handle = shape.Entity
	case *debug.Box:
		handle = shape.Entity
	case *debug.Circle:
		handle = shape.Entity
	case *debug.Line:
		handle = shape.Entity
	case *debug.Sphere:
		handle = shape.Entity
	case *debug.Text:
		handle = shape.Entity
	case *debug.Cylinder:
		handle = shape.Entity
	case *debug.Pyramid:
		handle = shape.Entity
	case *debug.Ellipsoid:
		handle = shape.Entity
	case *debug.Cone:
		handle = shape.Entity
	}
	if handle == nil {
		return 0
	}
	return s.handleRuntimeID(handle)
}

// debugShapeToProtocol converts a debug shape to its protocol representation. It also provides defaults
// for some fields such as colour, scale and other per-shape properties.
func debugShapeToProtocol(shape debug.Shape, dim world.Dimension, attachedEntityID uint64, networkID int) mc.PrimitiveShape {
	dimID, _ := world.DimensionID(dim)
	ps := mc.PrimitiveShape{
		NetworkID:   uint64(networkID),
		DimensionID: proto.Some(int32(dimID)),
	}
	if attachedEntityID > 0 {
		ps.AttachedToEntityID = proto.Some(int64(attachedEntityID))
	}
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	switch shape := shape.(type) {
	case *debug.Arrow:
		ps.Type = proto.Some(mc.PrimitiveShapeArrow)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.ExtraShapeData = &mc.ArrowShape{
			ArrowEndLocation: proto.Some([3]float32(vec64To32(shape.EndPosition))),
			ArrowHeadLength:  proto.Some(valueOrDefault(float32(shape.HeadLength), 1)),
			ArrowHeadRadius:  proto.Some(valueOrDefault(float32(shape.HeadRadius), 0.5)),
			Segments:         proto.Some(valueOrDefault(uint8(shape.HeadSegments), 4)),
		}
	case *debug.Box:
		ps.Type = proto.Some(mc.PrimitiveShapeBox)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &mc.BoxShape{BoxBound: valueOrDefault(vec64To32(shape.Bounds), mgl32.Vec3{1, 1, 1})}
	case *debug.Circle:
		ps.Type = proto.Some(mc.PrimitiveShapeCircle)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &mc.SphereShape{Segments: valueOrDefault(uint8(shape.Segments), 20)}
	case *debug.Line:
		ps.Type = proto.Some(mc.PrimitiveShapeLine)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.ExtraShapeData = &mc.LineShape{LineEndLocation: vec64To32(shape.EndPosition)}
	case *debug.Sphere:
		ps.Type = proto.Some(mc.PrimitiveShapeSphere)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &mc.SphereShape{Segments: valueOrDefault(uint8(shape.Segments), 20)}
	case *debug.Text:
		ps.Type = proto.Some(mc.PrimitiveShapeText)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		if shape.LockRotation {
			ps.Rotation = proto.Some([3]float32(vec64To32(shape.Rotation)))
		}
		textData := &mc.TextShape{
			Text:             shape.Text,
			UseRotation:      shape.LockRotation,
			DepthTest:        !shape.DisableDepthTest,
			ShowBackface:     !shape.HideBackface,
			ShowBackfaceText: !shape.HideBackfaceText,
		}
		switch {
		case shape.HideBackground:
			textData.BackgroundColour = proto.Some(proto.Color{})
		case shape.BackgroundColour != (color.RGBA{}):
			textData.BackgroundColour = proto.Some(toColour(shape.BackgroundColour))
		}
		ps.ExtraShapeData = textData
	case *debug.Cylinder:
		ps.Type = proto.Some(mc.PrimitiveShapeCylinder)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		base := valueOrDefault(shape.BaseRadius, mgl64.Vec2{1, 1})
		top := valueOrDefault(shape.TopRadius, base)
		ps.ExtraShapeData = &mc.CylinderShape{
			RadiusX:     mgl32.Vec2{float32(base[0]), float32(top[0])},
			RadiusZ:     mgl32.Vec2{float32(base[1]), float32(top[1])},
			Height:      valueOrDefault(float32(shape.Height), 1),
			NumSegments: valueOrDefault(uint8(shape.Segments), 20),
		}
	case *debug.Pyramid:
		ps.Type = proto.Some(mc.PrimitiveShapePyramid)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		pyramid := &mc.PyramidShape{
			Width:  valueOrDefault(float32(shape.Width), 1),
			Height: valueOrDefault(float32(shape.Height), 1),
		}
		if shape.Depth != 0 {
			pyramid.Depth = proto.Some(float32(shape.Depth))
		}
		ps.ExtraShapeData = pyramid
	case *debug.Ellipsoid:
		ps.Type = proto.Some(mc.PrimitiveShapeEllipsoid)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &mc.EllipsoidShape{
			Radii:           valueOrDefault(vec64To32(shape.Radii), mgl32.Vec3{1, 1, 1}),
			SegmentsPerAxis: valueOrDefault(uint8(shape.SegmentsPerAxis), 20),
		}
	case *debug.Cone:
		ps.Type = proto.Some(mc.PrimitiveShapeCone)
		ps.Colour = proto.Some(toColour(valueOrDefault(shape.Colour, white)))
		ps.Location = proto.Some([3]float32(vec64To32(shape.Position)))
		ps.Scale = proto.Some(valueOrDefault(float32(shape.Scale), 1))
		ps.ExtraShapeData = &mc.ConeShape{
			Radii:       valueOrDefault(vec2To32(shape.Radii), mgl32.Vec2{1, 1}),
			Height:      valueOrDefault(float32(shape.Height), 1),
			NumSegments: valueOrDefault(uint8(shape.Segments), 20),
		}
	default:
		panic(fmt.Sprintf("unknown debug shape type %T", shape))
	}
	return ps
}
