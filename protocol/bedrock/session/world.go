package session

import (
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// writeBroadcast writes the packets of a broadcast: those the first viewer
// made, encoded once for all, or makes them with build.
func (s *Session) writeBroadcast(b *world.Broadcast, build func(out func(proto.Packet))) {
	pks, ok := b.Load().([]*netconn.Shared)
	if !ok {
		build(func(pk proto.Packet) { pks = append(pks, &netconn.Shared{Packet: pk}) })
		b.Store(pks)
	}
	for _, pk := range pks {
		s.writePacket(pk)
	}
}

// vec32To64 converts a mgl32.Vec3 to a mgl64.Vec3.
func vec32To64(vec3 mgl32.Vec3) mgl64.Vec3 {
	return mgl64.Vec3{float64(vec3[0]), float64(vec3[1]), float64(vec3[2])}
}

// vec64To32 converts a mgl64.Vec3 to a mgl32.Vec3.
func vec64To32(vec3 mgl64.Vec3) mgl32.Vec3 {
	return mgl32.Vec3{float32(vec3[0]), float32(vec3[1]), float32(vec3[2])}
}

// vec2To32 converts a mgl64.Vec2 to a mgl32.Vec2.
func vec2To32(vec2 mgl64.Vec2) mgl32.Vec2 {
	return mgl32.Vec2{float32(vec2[0]), float32(vec2[1])}
}

// blockPosFromProtocol ...
func blockPosFromProtocol(pos mc.BlockPos) cube.Pos {
	return cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}
}

// boolByte returns 1 if the bool passed is true, or 0 if it is false.
func boolByte(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// abs ...
func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}
