// Package c2169 converts the packets of protocol 2169 (1.26.45) to and
// from those of 2168 (1.26.40–1.26.44), the protocol before it. Only
// SetScore (ScoreboardEntry) differs on the wire between 2168 and 2169.
package c2169

//go:generate go run ../../gen -compare ../../v2168/schema.txt -schema ../../v2169/schema.txt -from github.com/dm-vev/adamant/net/bedrock/proto/v2168 -to github.com/dm-vev/adamant/net/bedrock/proto/v2169 -pkg c2169 -o same_gen.go -convert convert_gen.go

import (
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2168"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2169"
)

// From and To are the protocols of the step: 2168 and 2169 after it.
const From, To = v2168.Protocol, v2169.Protocol

// Up converts a packet of 2168 to the packets of 2169 that carry it.
func Up(pk proto.Packet) []proto.Packet {
	if Same(pk.ID()) {
		return []proto.Packet{pk}
	}
	if out, ok := upPacket(pk); ok {
		return []proto.Packet{out}
	}
	return nil
}

// Down converts a packet of 2169 to the packets of 2168 that carry it.
func Down(pk proto.Packet) []proto.Packet {
	if Same(pk.ID()) {
		return []proto.Packet{pk}
	}
	if out, ok := downPacket(pk); ok {
		return []proto.Packet{out}
	}
	return nil
}

// DownKeeps reports whether Down returns a packet of 2169 as it is.
func DownKeeps(pk proto.Packet) bool { return Same(pk.ID()) }
