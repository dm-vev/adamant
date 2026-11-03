package session_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
)

// TestGoldenChunks records the chunks a client is sent around its spawn:
// the columns, four a tick, then the sub-chunks it asks for, a block
// changed in a loaded chunk, and the chunks of where the player is moved
// to.
func TestGoldenChunks(t *testing.T) {
	if isolated(t) {
		return
	}
	s := newScenario(t, "chunks", scenarioConfig{chunks: true})

	s.tick("columns", 3)
	s.client("sub-chunks asked for", s.subChunkRequests()...)
	s.step("block changed", func(tx *world.Tx, _ *player.Player) {
		tx.SetBlock(cube.Pos{1, -61, 1}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{2, -60, 1}, block.Chest{}, nil)
	})
	s.tick("block changes in the tick", 1)
	s.step("player moved two chunks away", func(_ *world.Tx, p *player.Player) {
		p.Teleport(mgl64.Vec3{40.5, -60, 0.5})
	})
	s.tick("columns of the new place", 3)
	s.client("sub-chunks of the new place asked for", s.subChunkRequests()...)
	s.snapshot()
}

// subChunkRequests returns the requests a client sends for the chunks it was
// sent since the last call: the sub-chunk of the ground and the one above,
// for each column.
func (s *scenario) subChunkRequests() []proto.Packet {
	var out []proto.Packet
	for _, pos := range s.chunks {
		out = append(out, &v2193.SubChunkRequestPacket{
			Position: v2193.SubChunkPos{pos[0], -4, pos[1]},
			Offsets:  []v2193.SubChunkOffset{{0, 0, 0}, {0, 1, 0}},
		})
	}
	s.chunks = nil
	return out
}
