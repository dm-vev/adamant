package session

import (
	"fmt"
	"image/color"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// MapInfoRequestHandler returns the persistent map texture and fixed decorations requested by a client.
type MapInfoRequestHandler struct{}

func (*MapInfoRequestHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, c Controllable) error {
	request := p.(*v2193.MapInfoRequestPacket)
	data, err := tx.UpdateMap(request.MapID, cube.PosFromVec3(c.Position()))
	if err != nil {
		return fmt.Errorf("update map %d: %w", request.MapID, err)
	}
	data.ForceFullTexture = true
	return s.ViewMapData(data)
}

// ViewMapData sends a texture update, tracked objects and fixed decorations to a client.
func (s *Session) ViewMapData(data world.MapData) error {
	if data.Dimension < 0 || data.Dimension > 255 {
		return fmt.Errorf("map %d dimension %d cannot be represented by Bedrock", data.ID, data.Dimension)
	}
	includeFull := data.ForceFullTexture || data.ColorsChanged && data.PixelPatch == nil
	includePatch := !includeFull && data.PixelPatch != nil
	var pixels []color.RGBA
	if includeFull {
		pixels = make([]color.RGBA, len(data.Colors))
		if len(data.NativeColors) == len(data.Colors)*4 {
			for i := range pixels {
				pixels[i] = color.RGBA{R: data.NativeColors[i*4], G: data.NativeColors[i*4+1], B: data.NativeColors[i*4+2], A: data.NativeColors[i*4+3]}
			}
		} else {
			for i, packed := range data.Colors {
				pixels[i] = world.MapColourRGBA(packed)
			}
		}
	} else if includePatch {
		pixels = make([]color.RGBA, len(data.PixelPatch.Colors))
		for i, packed := range data.PixelPatch.Colors {
			pixels[i] = world.MapColourRGBA(packed)
		}
	}
	decorations := make([]v2193.MapDecoration, len(data.Decorations))
	for i, d := range data.Decorations {
		decorations[i] = v2193.MapDecoration{Type: d.Type, Rotation: d.Rotation, X: byte(d.X), Y: byte(d.Z), Label: d.Label, Colour: toColour(color.RGBA{A: 0xff})}
	}
	trackedObjects := make([]v2193.MapTrackedObject, 0, len(data.TrackedObjects))
	for _, object := range data.TrackedObjects {
		if object.Entity != nil {
			if id := s.handleRuntimeID(object.Entity); id != 0 {
				trackedObjects = append(trackedObjects, v2193.MapTrackedObject{
					Type: protocol.MapObjectTypeEntity, EntityUniqueID: proto.Some(s.handleUniqueID(object.Entity)),
				})
			}
		} else if object.Block != nil {
			pos := *object.Block
			trackedObjects = append(trackedObjects, v2193.MapTrackedObject{
				Type:          protocol.MapObjectTypeBlock,
				BlockPosition: proto.Some([3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])}),
			})
		}
	}
	pk := &v2193.ClientBoundMapItemDataPacket{
		MapID:          data.ID,
		Dimension:      byte(data.Dimension),
		LockedMap:      data.Locked,
		Origin:         protocol.BlockPos{int32(data.CenterX), 0, int32(data.CenterZ)},
		TrackedObjects: proto.Some(trackedObjects),
		Scale:          proto.Some(data.Scale),
		Decorations:    proto.Some(decorations),
	}
	if includeFull || includePatch {
		x, y, width, height := 0, 0, 128, 128
		if includePatch {
			x, y = data.PixelPatch.X, data.PixelPatch.Y
			width, height = data.PixelPatch.Width, data.PixelPatch.Height
		}
		pk.Width, pk.Height = proto.Some(int32(width)), proto.Some(int32(height))
		pk.XOffset, pk.YOffset = proto.Some(int32(x)), proto.Some(int32(y))
		pk.Pixels = proto.Some(toColours(pixels))
	}
	s.writePacket(pk)
	return nil
}
