package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// RequestChunkRadiusHandler handles the RequestChunkRadius packet.
type RequestChunkRadiusHandler struct{}

// Handle ...
func (*RequestChunkRadiusHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	pk := p.(*mc.RequestChunkRadiusPacket)

	chunkRadius := pk.ChunkRadius
	if chunkRadius < 0 {
		chunkRadius = 0
	}
	if chunkRadius > s.maxChunkRadius {
		chunkRadius = s.maxChunkRadius
	}
	s.chunkRadius.Store(chunkRadius)

	// The chunk loader is initialised during Spawn, so it may be nil early on.
	if s.chunkLoader != nil {
		s.chunkLoader.ChangeRadius(tx, int(chunkRadius))
	}

	s.writePacket(&mc.ChunkRadiusUpdatedPacket{ChunkRadius: chunkRadius})
	return nil
}
