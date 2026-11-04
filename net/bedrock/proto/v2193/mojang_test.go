package v2193

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// mojangLayouts are packets whose layout protoimport corrects after
// Mojang's protocol documentation of 1.26.50 (json/*.json of
// Mojang/bedrock-protocol-docs v1.26.50), with the bytes Mojang's layout
// gives them.
var mojangLayouts = []struct {
	name string
	pk   proto.Packet
	hex  string
}{
	// json/PlayerVideoCapturePacketPayload.json: the index of the variant
	// as a varuint32, Start Video Capture 0 with its frame rate (uint32)
	// and file prefix, Stop Video Capture 1.
	{"video capture start", &PlayerVideoCapturePacket{Action: PlayerVideoCaptureActionStart, FrameRate: 30, FilePrefix: "f"}, "00" + "1e000000" + "0166"},
	{"video capture stop", &PlayerVideoCapturePacket{Action: PlayerVideoCaptureActionStop}, "01"},
}

// TestMojangLayouts encodes the packets of mojangLayouts to their bytes,
// and decodes them back.
func TestMojangLayouts(t *testing.T) {
	for _, c := range mojangLayouts {
		var w wire.Writer
		c.pk.Encode(&w)
		if got := hex.EncodeToString(w.B); got != c.hex {
			t.Errorf("%s: encoded % x, Mojang's layout gives %s", c.name, w.B, c.hex)
			continue
		}
		back := reflect.New(reflect.TypeOf(c.pk).Elem()).Interface().(proto.Packet)
		r := wire.NewReader(bytes.Clone(w.B))
		back.Decode(r)
		if r.Err() != nil || r.Len() != 0 || !reflect.DeepEqual(back, c.pk) {
			t.Errorf("%s: decoded %+v, %v, %d bytes left", c.name, back, r.Err(), r.Len())
		}
	}
}
