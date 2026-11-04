// Package c2225 converts the packets of protocol 2225 (1.26.60) to and
// from those of 2193 (1.26.50), the protocol before it. It converts layouts
// and the values of enums; it never maps blocks, items or biomes, which the
// adapter encodes for each protocol itself.
//
// same_gen.go and convert_gen.go are generated from the schemas of both
// protocols by proto/gen; the converters of what the codec packages code by
// hand are in hand_convert.go, and the fixes of what a copy of fields by
// name gets wrong in fixups.go.
package c2225

//go:generate go run ../../gen -compare ../../v2193/schema.txt -schema ../../v2225/schema.txt -from github.com/dm-vev/adamant/net/bedrock/proto/v2193 -to github.com/dm-vev/adamant/net/bedrock/proto/v2225 -pkg c2225 -o same_gen.go -convert convert_gen.go

import (
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// From and To are the protocols of the step: 2193 and 2225 after it.
const From, To = v2193.Protocol, v2225.Protocol

// Up converts a packet of 2193 to the packets of 2225 that carry it, none
// if 2225 has no such packet or no such value of an enum the packet holds
// (noForm lists them). A packet of an ID that is Same is returned as it
// is: its bytes are valid in both.
func Up(pk proto.Packet) []proto.Packet {
	if !upValues(pk) {
		return nil
	}
	if Same(pk.ID()) {
		return []proto.Packet{pk}
	}
	if out, ok := upPacket(pk); ok {
		return []proto.Packet{out}
	}
	return nil
}

// Down converts a packet of 2225 to the packets of 2193 that carry it, none
// if 2193 has no such packet or value, as Up converts the other way.
func Down(pk proto.Packet) []proto.Packet {
	if !downValues(pk) {
		return nil
	}
	if Same(pk.ID()) {
		return []proto.Packet{pk}
	}
	if out, ok := downPacket(pk); ok {
		return []proto.Packet{out}
	}
	return nil
}
