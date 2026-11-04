// Package versions knows the protocols of Bedrock Edition the adapter was
// built to speak, and ties each to its codecs.
//
// The session and netconn are compiled against one generated codec
// package, the model. A Protocol that is not the model reaches it through
// a chain of steps between adjacent protocols, which Out and In walk. The
// chain of the model to itself is empty: its packets go as they are.
package versions

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
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
	Out, In func(proto.Packet) []proto.Packet
}

// String returns the version of the protocol.
func (p *Protocol) String() string { return p.Version }

// Converts reports whether packets of the protocol are converted to and
// from those of the model, which is so for every protocol but the model.
func (p *Protocol) Converts() bool { return p.Out != nil || p.In != nil }

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

// p2193 is protocol 2193, of 1.26.50, 1.26.51 and 1.26.52, the model.
var p2193 = &Protocol{
	Number:     v2193.Protocol,
	Names:      []string{"1.26.50", "26.50", "1.26.51", "26.51", "1.26.52", "26.52"},
	Version:    v2193.Version,
	New:        v2193.New,
	FromClient: v2193.FromClient,
}

// all holds the protocols, oldest first.
var all = []*Protocol{p2193}

// All returns the protocols the adapter was built with, oldest first.
func All() []*Protocol { return slices.Clone(all) }

// Model returns the protocol of the codec package the session and netconn
// are compiled against.
func Model() *Protocol { return p2193 }

// ByNumber returns the protocol of a number.
func ByNumber(n int32) (*Protocol, bool) {
	for _, p := range all {
		if p.Number == n {
			return p, true
		}
	}
	return nil, false
}

// Parse returns the protocol a name stands for: a version of the game that
// speaks it, with or without the leading "1." ("26.52", "1.26.52"), or its
// number ("2193").
func Parse(name string) (*Protocol, error) {
	name = strings.TrimSpace(name)
	if n, err := strconv.ParseInt(name, 10, 32); err == nil {
		if p, ok := ByNumber(int32(n)); ok {
			return p, nil
		}
	}
	for _, p := range all {
		if slices.Contains(p.Names, name) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("protocol %q is not supported: this build supports %s", name, Supported())
}

// Supported describes the protocols the adapter was built with, by the
// version each tells its clients and the versions that speak it:
// "1.26.50 (26.50–26.52)".
func Supported() string {
	s := make([]string, len(all))
	for i, p := range all {
		s[i] = p.Version + " (" + p.Range() + ")"
	}
	return strings.Join(s, ", ")
}
