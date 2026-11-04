// Package versions knows the protocols of Bedrock Edition the adapter was
// built to speak, and ties each to its codecs.
//
// The session and netconn are compiled against one generated codec
// package, the model: that of the newest protocol, 2225, as only the
// newest can hold every field. A Protocol that is not the model reaches it
// through a chain of steps between adjacent protocols, which its codec,
// from package codec of the net module, walks: 2193 through
// convert/c2225. The chain of the model to itself is empty: its packets go
// as they are.
package versions

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/dm-vev/adamant/net/bedrock/proto/codec"
)

// Protocol is a protocol of Bedrock Edition the adapter can speak: its
// codecs, with the chain of steps that reach the model (Number, Version,
// New, FromClient, Out, In, Same, Keeps and SetShieldID, see codec.Codec),
// and the versions of the game that speak it.
type Protocol struct {
	codec.Codec
	// Names are the versions of the game that speak the protocol, oldest
	// first, each with the leading "1." and without it: "1.26.50", "26.50".
	// The first is Version, the version of the game told to clients of the
	// protocol.
	Names []string
}

// String returns the version of the protocol.
func (p *Protocol) String() string { return p.Version }

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
	Codec: *codec.V2193,
	Names: []string{"1.26.50", "26.50", "1.26.51", "26.51", "1.26.52", "26.52"},
}

// p2225 is protocol 2225, of 1.26.60 and 1.26.61, the model.
var p2225 = &Protocol{
	Codec: *codec.V2225,
	Names: []string{"1.26.60", "26.60", "1.26.61", "26.61"},
}

// all holds the protocols, oldest first.
var all = []*Protocol{p2193, p2225}

// configurable holds the protocols a config may name, oldest first: those
// whose content the adapter serves.
var configurable = []*Protocol{p2193, p2225}

// newestPrimary is the newest protocol a config may name as the primary:
// the session speaks the features of no newer one yet, though the model
// is newer.
var newestPrimary = p2193

// All returns the protocols the adapter was built with, oldest first.
func All() []*Protocol { return slices.Clone(all) }

// Configurable returns the protocols a config may name, oldest first.
func Configurable() []*Protocol { return slices.Clone(configurable) }

// Model returns the protocol of the codec package the session and netconn
// are compiled against.
func Model() *Protocol { return p2225 }

// NewestPrimary returns the newest protocol a config may name as the
// primary, the protocol the session speaks: no newer than the model.
func NewestPrimary() *Protocol { return newestPrimary }

// SetShieldID sets the network ID of the shield for the codecs a
// connection of a protocol encodes and decodes with, as codec.SetShieldID.
func SetShieldID(p *Protocol, id int32) { codec.SetShieldID(&p.Codec, id) }

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
