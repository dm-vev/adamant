package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// SubChunkRequestHandler handles sub-chunk requests from the client. The server will respond with a packet containing
// the requested sub-chunks.
type SubChunkRequestHandler struct{}

// Handle ...
func (*SubChunkRequestHandler) Handle(p proto.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	pk := p.(*v2193.SubChunkRequestPacket)
	if dimID, _ := world.DimensionID(tx.World().Dimension()); pk.Dimension != int32(dimID) {
		// Outdated sub chunk request from a previous dimension.
		s.writePacket(&v2193.SubChunkPacket{
			Dimension:       pk.Dimension,
			Position:        pk.Position,
			CacheEnabled:    s.conn.ClientCacheEnabled(),
			SubChunkEntries: []v2193.SubChunkEntry{},
		})
		return nil
	}
	s.ViewSubChunks(world.SubChunkPos(pk.Position), pk.Offsets, tx)
	return nil
}
