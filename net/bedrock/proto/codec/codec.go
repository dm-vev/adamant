// Package codec ties the generated codec packages of the protocols of
// Bedrock Edition to the steps between them, for the code that speaks
// several: a server's connections and a client.
//
// Such code is compiled against one codec package, the model: that of the
// newest protocol, v2225, as only the newest can hold every field. A Codec
// of another protocol reaches the model through the chain of steps between
// adjacent protocols, which its Out and In walk: 2193 through
// convert/c2225. The chain of the model to itself is empty, and its
// packets go as they are.
package codec

import (
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/convert/c2225"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// Codec is a protocol of Bedrock Edition as its codecs see it.
type Codec struct {
	// Number is the number of the protocol, as a client sends it in
	// RequestNetworkSettings and Login.
	Number int32
	// Version is the version of the game of the first release that speaks
	// the protocol.
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

// Converts reports whether packets of the protocol are converted to and
// from those of the model, which is so for every protocol but the model.
func (c *Codec) Converts() bool { return c.Out != nil || c.In != nil }

// SameLayout reports whether packets of an ID have the same layout in the
// protocol and in the model: always for a protocol that converts nothing,
// as the model, and never for one that converts without a Same.
func (c *Codec) SameLayout(id uint32) bool {
	if !c.Converts() {
		return true
	}
	return c.Same != nil && c.Same(id)
}

// Decoder returns the codecs a packet of an ID of the protocol decodes
// with: those of the model if its layout is the model's, else those of
// the protocol, whose packet In converts.
func (c *Codec) Decoder(id uint32) *Codec {
	if c.SameLayout(id) {
		return Model()
	}
	return c
}

// Decode decodes the fields of a packet of an ID of the protocol with the
// reader passed, as the packets of the model that carry it: with the
// codecs of the model if the layout of the ID is the model's, else with
// those of the protocol, then converted by In. It returns nil and no packet
// for an ID the protocol does not know; errors stay in the reader.
func (c *Codec) Decode(id uint32, r *wire.Reader) []proto.Packet {
	d := c.Decoder(id)
	pk := d.New(id)
	if pk == nil {
		return nil
	}
	pk.Decode(r)
	if d == c && c.Converts() && r.Err() == nil {
		return c.In(pk)
	}
	return []proto.Packet{pk}
}

// Encode hands write the packets of the protocol that carry a packet of
// the model, model, as Out gives them; or pk itself, the packet written,
// such as a wrapper of model that keeps its bytes, if model goes as it is,
// as Keeps tells or Out returns it. It allocates nothing for a packet that
// goes as it is and Keeps tells of, and returns the first error of write.
func (c *Codec) Encode(pk, model proto.Packet, write func(proto.Packet) error) error {
	if !c.Converts() || c.Keeps != nil && c.Keeps(model) {
		return write(pk)
	}
	out := c.Out(model)
	if len(out) == 1 && out[0] == model {
		return write(pk)
	}
	for _, pk := range out {
		if err := write(pk); err != nil {
			return err
		}
	}
	return nil
}

// V2193 is protocol 2193, of 1.26.50, 1.26.51 and 1.26.52, a step before
// the model: c2225 converts its packets.
var V2193 = &Codec{
	Number:      v2193.Protocol,
	Version:     v2193.Version,
	New:         v2193.New,
	FromClient:  v2193.FromClient,
	Out:         c2225.Down,
	In:          c2225.Up,
	Same:        c2225.Same,
	Keeps:       c2225.DownKeeps,
	SetShieldID: v2193.SetShieldID,
}

// V2225 is protocol 2225, of 1.26.60 and 1.26.61, the model.
var V2225 = &Codec{
	Number:      v2225.Protocol,
	Version:     v2225.Version,
	New:         v2225.New,
	FromClient:  v2225.FromClient,
	SetShieldID: v2225.SetShieldID,
}

// all holds the codecs, oldest first.
var all = []*Codec{V2193, V2225}

// All returns the codecs of every protocol, oldest first.
func All() []*Codec { return append([]*Codec(nil), all...) }

// Model returns the codec of the model, the newest protocol.
func Model() *Codec { return V2225 }

// ByNumber returns the codec of a protocol by its number.
func ByNumber(n int32) (*Codec, bool) {
	for _, c := range all {
		if c.Number == n {
			return c, true
		}
	}
	return nil, false
}

// SetShieldID sets the network ID of the shield for the codecs a
// connection of a protocol encodes and decodes with: those of the protocol,
// if it has codecs of its own, and those of the model, which code the
// packets of the same layout.
func SetShieldID(c *Codec, id int32) {
	if c.SetShieldID != nil {
		c.SetShieldID(id)
	}
	Model().SetShieldID(id)
}
