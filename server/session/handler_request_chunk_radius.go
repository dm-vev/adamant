package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// RequestChunkRadiusHandler handles the RequestChunkRadius packet.
type RequestChunkRadiusHandler struct{}

// Handle ...
func (*RequestChunkRadiusHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	pk := p.(*packet.RequestChunkRadius)

	chunkRadius := pk.ChunkRadius
	if chunkRadius < 0 {
		chunkRadius = 0
	}
	if chunkRadius > s.maxChunkRadius {
		chunkRadius = s.maxChunkRadius
	}
	s.chunkRadius.Store(chunkRadius)

	s.chunkLoader.ChangeRadius(tx, int(chunkRadius))

	s.writePacket(&packet.ChunkRadiusUpdated{ChunkRadius: chunkRadius})
	return nil
}
