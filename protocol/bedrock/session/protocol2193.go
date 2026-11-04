package session

import (
	"image/color"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// toMetadata returns entity metadata, which the session builds with the
// helpers of gophertunnel's map, as the metadata of the codecs.
func toMetadata(m mc.Metadata) mc.Metadata { return mc.Metadata(m) }

// toColour returns a colour as the codecs hold it.
func toColour(c color.RGBA) proto.Color { return proto.Color{c.R, c.G, c.B, c.A} }

// toColours returns colours as the codecs hold them.
func toColours(l []color.RGBA) []proto.Color {
	out := make([]proto.Color, len(l))
	for i, c := range l {
		out[i] = toColour(c)
	}
	return out
}
