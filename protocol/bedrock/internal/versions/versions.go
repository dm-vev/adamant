// Package versions knows the protocols of Bedrock Edition the adapter was
// built to speak, and ties each to its codecs.
//
// The session and netconn are compiled against one generated codec
// package, the model: that of the newest protocol, 2225, as only the
// newest can hold every field. A Protocol that is not the model reaches it
// through a chain of steps between adjacent protocols, which Out and In
// walk: 2193 through convert/c2225. The chain of the model to itself is
// empty: its packets go as they are.
package versions

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/convert/c2225"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// Protocol is a protocol of Bedrock Edition the adapter can speak.
type Protocol struct {
	// Number is the number of the protocol, as a client sends it in
	// RequestNetworkSettings and Login.
	Number int32
	// Names are the versions of the game that speak the protocol, oldest
	// first, each with the leading "1." and without it: "1.26.50", "26.50".
	Names []string
	// Version is the version of the game told to clients of the protocol:
	// the first release that speaks it.
	Version string
	// New returns an empty packet of an ID in the codecs of the protocol,
	// nil for an ID it does not know. FromClient reports whether a client
	// sends packets of an ID.
	New        func(id uint32) proto.Packet
	FromClient func(id uint32) bool
	// Out converts a packet of the model to the packets of the protocol
	// that carry it, none if the protocol has no such packet. In converts
	// a packet of the protocol to the packets of the model. Both are nil
	// for the model, whose packets go as they are.
	//
	// A packet of an ID of the same layout in the model and the protocol,
	// as Same reports, needs neither: Out returns the packet of the model
	// as it is, whose bytes are those of the protocol, and its bytes
	// decode as the model. In takes the packets of the other IDs.
	Out, In func(proto.Packet) []proto.Packet
	// Same reports whether packets of an ID have the same layout in the
	// protocol and in the model, through every step between them. It is
	// nil for the model.
	Same func(id uint32) bool
	// Keeps reports whether Out returns a packet of the model as it is,
	// without the cost of Out; nil if only Out tells.
	Keeps func(proto.Packet) bool
	// SetShieldID sets the network ID of the shield the codecs of the
	// protocol find the items of shields by.
	SetShieldID func(id int32)
}

// String returns the version of the protocol.
func (p *Protocol) String() string { return p.Version }

// Converts reports whether packets of the protocol are converted to and
// from those of the model, which is so for every protocol but the model.
func (p *Protocol) Converts() bool { return p.Out != nil || p.In != nil }

// SameLayout reports whether packets of an ID have the same layout in the
// protocol and in the model: always for a protocol that converts nothing,
// as the model, and never for one that converts without a Same.
func (p *Protocol) SameLayout(id uint32) bool {
	if !p.Converts() {
		return true
	}
	return p.Same != nil && p.Same(id)
}

// Range returns the versions of the game that speak the protocol, short:
// "26.50–26.52", or "26.60" for a protocol of one version.
func (p *Protocol) Range() string {
	var short []string
	for _, n := range p.Names {
		if !strings.HasPrefix(n, "1.") {
			short = append(short, n)
		}
	}
	if len(short) == 0 {
		return p.Version
	}
	if len(short) == 1 {
		return short[0]
	}
	return short[0] + "–" + short[len(short)-1]
}

// p2193 is protocol 2193, of 1.26.50, 1.26.51 and 1.26.52, a step before
// the model: c2225 converts its packets.
var p2193 = &Protocol{
	Number:      v2193.Protocol,
	Names:       []string{"1.26.50", "26.50", "1.26.51", "26.51", "1.26.52", "26.52"},
	Version:     v2193.Version,
	New:         v2193.New,
	FromClient:  v2193.FromClient,
	Out:         c2225.Down,
	In:          c2225.Up,
	Same:        c2225.Same,
	Keeps:       c2225.DownKeeps,
	SetShieldID: v2193.SetShieldID,
}

// p2225 is protocol 2225, of 1.26.60 and 1.26.61, the model.
var p2225 = &Protocol{
	Number:      v2225.Protocol,
	Names:       []string{"1.26.60", "26.60", "1.26.61", "26.61"},
	Version:     v2225.Version,
	New:         v2225.New,
	FromClient:  v2225.FromClient,
	SetShieldID: v2225.SetShieldID,
}

// all holds the protocols, oldest first.
var all = []*Protocol{p2193, p2225}

// configurable holds the protocols a config may name, oldest first: those
// whose content the adapter serves. 2225 joins them once its vanilla data
// and tables are in; until then the server speaks it only as the model.
var configurable = []*Protocol{p2193}

// All returns the protocols the adapter was built with, oldest first.
func All() []*Protocol { return slices.Clone(all) }

// Configurable returns the protocols a config may name, oldest first.
func Configurable() []*Protocol { return slices.Clone(configurable) }

// Model returns the protocol of the codec package the session and netconn
// are compiled against.
func Model() *Protocol { return p2225 }

// SetShieldID sets the network ID of the shield for the codecs a
// connection of a protocol encodes and decodes with: those of the protocol,
// if it has codecs of its own, and those of the model, which code the
// packets of the same layout.
func SetShieldID(p *Protocol, id int32) {
	if p.SetShieldID != nil {
		p.SetShieldID(id)
	}
	Model().SetShieldID(id)
}

// Default returns the protocol a server allows and speaks unless its
// config names others: the one it allowed before it could allow more.
func Default() *Protocol { return p2193 }

// ByNumber returns the protocol of a number.
func ByNumber(n int32) (*Protocol, bool) {
	for _, p := range all {
		if p.Number == n {
			return p, true
		}
	}
	return nil, false
}

// Parse returns the protocol a name of a config stands for: a version of
// the game that speaks it, with or without the leading "1." ("26.52",
// "1.26.52"), or its number ("2193"). Only the protocols a config may name
// are found.
func Parse(name string) (*Protocol, error) {
	name = strings.TrimSpace(name)
	if n, err := strconv.ParseInt(name, 10, 32); err == nil {
		if p, ok := ByNumber(int32(n)); ok && slices.Contains(configurable, p) {
			return p, nil
		}
	}
	for _, p := range configurable {
		if slices.Contains(p.Names, name) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("protocol %q is not supported: this build supports %s", name, Supported())
}

// Supported describes the protocols a config may name, by the version each
// tells its clients and the versions that speak it: "1.26.50
// (26.50–26.52)".
func Supported() string {
	s := make([]string, len(configurable))
	for i, p := range configurable {
		s[i] = p.Version + " (" + p.Range() + ")"
	}
	return strings.Join(s, ", ")
}
