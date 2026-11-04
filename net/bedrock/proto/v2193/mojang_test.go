package v2193

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"strings"
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
	// json/StructureEditorData.json: the structure name is a
	// json/Bedrock__Safety__RedactableString.json, the name and the name
	// filtered, which is optional. The 45 bytes of the rest are zero.
	{"structure name filtered", &StructureBlockUpdatePacket{StructureName: "a", FilteredStructureName: proto.Some("b")}, "000000" + "0161" + "01" + "0162" + strings.Repeat("00", 45)},
	{"structure name not filtered", &StructureBlockUpdatePacket{StructureName: "a"}, "000000" + "0161" + "00" + strings.Repeat("00", 45)},
	// json/PlayerVideoCapturePacketPayload.json: the index of the variant
	// as a varuint32, Start Video Capture 0 with its frame rate (uint32)
	// and file prefix, Stop Video Capture 1.
	{"video capture start", &PlayerVideoCapturePacket{Action: PlayerVideoCaptureActionStart, FrameRate: 30, FilePrefix: "f"}, "00" + "1e000000" + "0166"},
	{"video capture stop", &PlayerVideoCapturePacket{Action: PlayerVideoCaptureActionStop}, "01"},
	// json/CameraInstruction__SplineInstruction.json: the total time, the
	// type, the curve and the key frames, then the identifier and the
	// flag of loading from JSON, which have defaults and always go. The
	// instructions before and after the spline are absent.
	{"spline instruction", &CameraInstructionPacket{Spline: proto.Some(CameraSplineInstruction{
		TotalTime: 1, SplineType: 1, ProgressKeyFrames: []CameraProgressOption{{Value: 1, Time: 2, EaseType: 2}},
		SplineIdentifier: "a", LoadFromJson: true,
	})}, "000000000000" + "01" + "0000803f" + "01" + "00" + "01" + "0000803f" + "00000040" + "07696e5f71756164" + "00" + "0161" + "01" + "0000"},
	// json/CameraSplineDefinition.json: the name, the total time, the type,
	// which has a default and always goes, the control points and the key
	// frames, whose easing (json/CameraSplineProgressKeyFrame.json) has
	// none and is optional, by its name.
	{"spline definition", &CameraSplinePacket{Splines: []CameraSplineDefinition{{
		Name: "a:b", TotalTime: 2, SplineType: "linear",
		ProgressKeyFrames: []CameraSplineProgressKeyFrame{{Value: 1, Time: 2}},
		RotationKeyFrames: []CameraSplineRotationKeyFrame{{Time: 1, EaseType: Optional(proto.Some[int32](0))}},
	}}}, "01" + "03613a62" + "00000040" + "066c696e656172" + "00" +
		"01" + "0000803f" + "00000040" + "00" +
		"01" + "000000000000000000000000" + "0000803f" + "01" + "066c696e656172"},
}

// TestMojangLayouts encodes the packets of mojangLayouts to their bytes,
// and decodes them to a packet that encodes to them again.
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
		var again wire.Writer
		back.Encode(&again)
		if r.Err() != nil || r.Len() != 0 || !bytes.Equal(again.B, w.B) {
			t.Errorf("%s: decoded %+v, %v, %d bytes left", c.name, back, r.Err(), r.Len())
		}
	}
}
