package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// SubChunkRequestHandler handles sub-chunk requests from the client. The server will respond with a packet containing
// the requested sub-chunks, once the connection has room for them: see loadChunks.
type SubChunkRequestHandler struct{}

// Handle ...
func (*SubChunkRequestHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, _ Controllable) error {
	pk := p.(*v2193.SubChunkRequestPacket)
	s.subChunkQueue = append(s.subChunkQueue, pk)
	s.subChunkOffsets += len(pk.Offsets)
	return nil
}

// answerSubChunks answers a sub-chunk request of the client with the
// sub-chunks as they are now, and returns the bytes of chunks it took.
func (s *Session) answerSubChunks(pk *v2193.SubChunkRequestPacket, tx *world.Tx) int {
	if dimID, _ := world.DimensionID(tx.World().Dimension()); pk.Dimension != int32(dimID) {
		// Outdated sub chunk request from a previous dimension.
		s.writePacket(&v2193.SubChunkPacket{
			Dimension:       pk.Dimension,
			Position:        pk.Position,
			CacheEnabled:    s.conn.ClientCacheEnabled(),
			SubChunkEntries: []v2193.SubChunkEntry{},
		})
		return 0
	}
	return s.ViewSubChunks(world.SubChunkPos(pk.Position), pk.Offsets, tx)
}
