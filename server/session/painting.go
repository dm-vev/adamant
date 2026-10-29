package session

import (
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// viewPainting spawns a painting: Bedrock sends its centre, legacy 2D
// direction and motive in a dedicated AddPainting packet.
func (s *Session) viewPainting(e *entity.Painting) {
	s.writePacket(&v2193.AddPaintingPacket{
		EntityUniqueID:  s.entityUniqueID(e),
		EntityRuntimeID: s.entityRuntimeID(e),
		Position:        vec64To32(e.Position()),
		Direction:       int32(entity.BedrockDirection(e.Facing())),
		Title:           e.Motive().Name,
	})
}
