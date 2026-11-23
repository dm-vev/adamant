// Package c2193 converts the packets of protocol 2193 (1.26.50–1.26.52) to
// and from those of 2169 (1.26.45), the protocol before it. It converts
// layouts and enum values; blocks, items and biomes are mapped by the
// adapter.
package c2193

//go:generate go run ../../gen -compare ../../v2169/schema.txt -schema ../../v2193/schema.txt -from github.com/dm-vev/adamant/net/bedrock/proto/v2169 -to github.com/dm-vev/adamant/net/bedrock/proto/v2193 -pkg c2193 -o same_gen.go -convert convert_gen.go

import (
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2169"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// From and To are the protocols of the step: 2169 and 2193 after it.
const From, To = v2169.Protocol, v2193.Protocol

// Up converts a packet of 2169 to the packets of 2193 that carry it.
func Up(pk proto.Packet) []proto.Packet {
	if Same(pk.ID()) {
		return []proto.Packet{pk}
	}
	if out, ok := upPacket(pk); ok {
		return []proto.Packet{out}
	}
	return nil
}

// Down converts a packet of 2193 to the packets of 2169 that carry it.
func Down(pk proto.Packet) []proto.Packet {
	if Same(pk.ID()) {
		return []proto.Packet{pk}
	}
	if out, ok := downPacket(pk); ok {
		return []proto.Packet{out}
	}
	return nil
}

// DownKeeps reports whether Down returns a packet of 2193 as it is.
func DownKeeps(pk proto.Packet) bool { return Same(pk.ID()) }
