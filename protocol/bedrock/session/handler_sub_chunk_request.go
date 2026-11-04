package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// SubChunkRequestHandler handles sub-chunk requests from the client. The server will respond with a packet containing
// the requested sub-chunks, once the connection has room for them: see loadChunks. A request for more than
// maxSubChunkOffsets sub-chunks is answered in parts of as many, each with a packet of its own.
type SubChunkRequestHandler struct{}

// Handle ...
func (*SubChunkRequestHandler) Handle(p proto.Packet, s *Session, _ *world.Tx, _ Controllable) error {
	pk := p.(*mc.SubChunkRequestPacket)
	for len(pk.Offsets) > maxSubChunkOffsets {
		part, rest := *pk, *pk
		part.Offsets, rest.Offsets = pk.Offsets[:maxSubChunkOffsets:maxSubChunkOffsets], pk.Offsets[maxSubChunkOffsets:]
		s.subChunkQueue = append(s.subChunkQueue, &part)
		s.subChunkOffsets += len(part.Offsets)
		pk = &rest
	}
	s.subChunkQueue = append(s.subChunkQueue, pk)
	s.subChunkOffsets += len(pk.Offsets)
	return nil
}

// answerSubChunks answers a sub-chunk request of the client with the
// sub-chunks as they are now, and returns the bytes of chunks it took.
func (s *Session) answerSubChunks(pk *mc.SubChunkRequestPacket, tx *world.Tx) int {
	if dimID, _ := world.DimensionID(tx.World().Dimension()); pk.Dimension != int32(dimID) {
		// Outdated sub chunk request from a previous dimension.
		s.writePacket(&mc.SubChunkPacket{
			Dimension:       pk.Dimension,
			Position:        pk.Position,
			CacheEnabled:    s.conn.ClientCacheEnabled(),
			SubChunkEntries: []mc.SubChunkEntry{},
		})
		return 0
	}
	return s.ViewSubChunks(world.SubChunkPos(pk.Position), pk.Offsets, tx)
}
