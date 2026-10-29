package session

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/dm-vev/adamant/net/bedrock/wire"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// encodable checks a skin as it goes to clients: each texture holds the
// pixels of its size, which gophertunnel's encoder panicked over in the
// session writer, and the codecs encode it.
func encodable(t *testing.T, name string, s v2193.Skin) {
	t.Helper()
	if int(s.SkinImageWidth)*int(s.SkinImageHeight)*4 != len(s.SkinData) {
		t.Fatalf("%s: skin %dx%d of %d bytes", name, s.SkinImageWidth, s.SkinImageHeight, len(s.SkinData))
	}
	if int(s.CapeImageWidth)*int(s.CapeImageHeight)*4 != len(s.CapeData) {
		t.Fatalf("%s: cape %dx%d of %d bytes", name, s.CapeImageWidth, s.CapeImageHeight, len(s.CapeData))
	}
	gt := protocol.Skin{
		SkinID: s.SkinID, SkinImageWidth: s.SkinImageWidth, SkinImageHeight: s.SkinImageHeight, SkinData: s.SkinData,
		CapeImageWidth: s.CapeImageWidth, CapeImageHeight: s.CapeImageHeight, CapeData: s.CapeData, SkinGeometry: s.SkinGeometry,
	}
	for _, a := range s.Animations {
		if int(a.ImageWidth)*int(a.ImageHeight)*4 != len(a.ImageData) {
			t.Fatalf("%s: animation %dx%d of %d bytes", name, a.ImageWidth, a.ImageHeight, len(a.ImageData))
		}
		gt.Animations = append(gt.Animations, protocol.SkinAnimation{ImageWidth: a.ImageWidth, ImageHeight: a.ImageHeight, ImageData: a.ImageData, AnimationType: a.AnimationType, FrameCount: a.FrameCount})
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("%s: gophertunnel's encoder panics: %v", name, r)
			}
		}()
		var buf bytes.Buffer
		gt.Marshal(protocol.NewWriter(&buf, 0))
	}()
	var w wire.Writer
	s.Encode(&w)
	var back v2193.Skin
	r := wire.NewReader(w.B)
	back.Decode(r)
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("%s: does not decode back: %v", name, r.Err())
	}
}

func clientData(width, height, n int, persona bool) login.ClientData {
	return login.ClientData{
		SkinID:          "skin",
		SkinImageWidth:  width,
		SkinImageHeight: height,
		SkinData:        base64.StdEncoding.EncodeToString(make([]byte, n)),
		PersonaSkin:     persona,
		SkinGeometry:    base64.StdEncoding.EncodeToString([]byte(`{"format_version":"1.12.0"}`)),
	}
}

// TestPersonaSkinOfAnyPlayer joins with the persona skin of 256x256 that
// took the server down: the skin kept 0x0 as its size and the pixels the
// client sent, which gophertunnel refused to encode in the PlayerList.
func TestPersonaSkinOfAnyPlayer(t *testing.T) {
	s := ClientSkin(clientData(256, 256, 256*256*4, true))
	if s.Bounds().Dx() != 256 || s.Bounds().Dy() != 256 || len(s.Pix) != 256*256*4 || !s.Persona {
		t.Fatalf("persona skin %v of %d bytes", s.Bounds(), len(s.Pix))
	}
	encodable(t, "persona 256x256", skinToProtocol(s))
}

func TestClientSkinTrustsNothing(t *testing.T) {
	for name, data := range map[string]login.ClientData{
		"256x256 not persona":  clientData(256, 256, 256*256*4, false),
		"negative size":        clientData(-64, -64, 64*64*4, false),
		"pixels of another":    clientData(64, 64, 64*32*4, false),
		"persona too large":    clientData(1024, 1024, 1024*1024*4, true),
		"pixels not in base64": {SkinImageWidth: 64, SkinImageHeight: 64, SkinData: "!!!"},
		"geometry of no JSON":  {SkinImageWidth: 64, SkinImageHeight: 64, SkinGeometry: base64.StdEncoding.EncodeToString([]byte("{"))},
		"patch of no JSON":     {SkinImageWidth: 64, SkinImageHeight: 32, SkinResourcePatch: "e30="},
		"cape short": func() login.ClientData {
			d := clientData(64, 64, 64*64*4, false)
			d.CapeImageWidth, d.CapeImageHeight, d.CapeData = 64, 32, "AAAA"
			return d
		}(),
		"cape of negative size": func() login.ClientData {
			d := clientData(64, 64, 64*64*4, false)
			d.CapeImageWidth, d.CapeImageHeight = -1, -4
			return d
		}(),
	} {
		s := ClientSkin(data)
		encodable(t, name, skinToProtocol(s))
	}
	if s := ClientSkin(clientData(256, 256, 256*256*4, false)); s.Bounds().Dx() != 0 || s.Pix != nil {
		t.Fatal("a skin of 256x256 that is no persona is kept, which BDS empties")
	}
	if s := ClientSkin(clientData(64, 64, 64*64*4, false)); string(s.Model) != `{"format_version":"1.12.0"}` {
		t.Fatalf("geometry %q", s.Model)
	}

	d := clientData(64, 64, 64*64*4, false)
	d.CapeImageWidth, d.CapeImageHeight, d.CapeData = 64, 32, base64.StdEncoding.EncodeToString(make([]byte, 64*32*4))
	frame := base64.StdEncoding.EncodeToString(make([]byte, 32*64*4))
	for range skin.MaxAnimations + 2 {
		d.AnimatedImageData = append(d.AnimatedImageData, login.SkinAnimation{Frames: 2, Image: frame, ImageWidth: 32, ImageHeight: 64, Type: v2193.SkinAnimationHead})
	}
	d.AnimatedImageData = append(d.AnimatedImageData[:0:0],
		login.SkinAnimation{Frames: 2, Image: frame, ImageWidth: 32, ImageHeight: 32, Type: v2193.SkinAnimationHead},
		login.SkinAnimation{Frames: 1e300, Image: frame, ImageWidth: 32, ImageHeight: 64, Type: v2193.SkinAnimationHead},
		login.SkinAnimation{Frames: 2, Image: frame, ImageWidth: 32, ImageHeight: 64, Type: 1 << 32},
	)
	for range skin.MaxAnimations + 2 {
		d.AnimatedImageData = append(d.AnimatedImageData, login.SkinAnimation{Frames: 2, Image: frame, ImageWidth: 32, ImageHeight: 64, Type: v2193.SkinAnimationHead})
	}
	s := ClientSkin(d)
	if s.Cape.Bounds().Dx() != 64 || len(s.Cape.Pix) != 64*32*4 {
		t.Fatalf("cape of the game %v", s.Cape.Bounds())
	}
	if len(s.Animations) != skin.MaxAnimations {
		t.Fatalf("%d animations kept, want the %d valid ones up to the bound", len(s.Animations), skin.MaxAnimations)
	}
	encodable(t, "cape and animations", skinToProtocol(s))
}

// TestPlayerSkinTrustsNothing changes skins with PlayerSkin packets: none
// is refused, which closed the session, and each goes on as one clients
// can read.
func TestPlayerSkinTrustsNothing(t *testing.T) {
	persona := protocolToSkin(v2193.Skin{SkinImageWidth: 256, SkinImageHeight: 256, SkinData: make([]byte, 256*256*4), PersonaSkin: true, SkinGeometry: []byte("{}")})
	if persona.Bounds().Dx() != 256 || len(persona.Pix) != 256*256*4 {
		t.Fatalf("persona skin %v", persona.Bounds())
	}
	animated := protocolToSkin(v2193.Skin{SkinImageWidth: 64, SkinImageHeight: 64, SkinData: make([]byte, 64*64*4), Animations: []v2193.SkinAnimation{
		// The texture of an animation holds every frame.
		{ImageWidth: 32, ImageHeight: 64, ImageData: make([]byte, 32*64*4), AnimationType: v2193.SkinAnimationHead, FrameCount: 2},
		{ImageWidth: 32, ImageHeight: 32, ImageData: make([]byte, 32*64*4), AnimationType: v2193.SkinAnimationHead, FrameCount: 2},
	}})
	if len(animated.Animations) != 1 || animated.Animations[0].FrameCount != 2 {
		t.Fatalf("animations %+v", animated.Animations)
	}
	encodable(t, "animated", skinToProtocol(animated))

	rng := rand.New(rand.NewPCG(1, 2))
	size := func() uint32 {
		return []uint32{0, 1, 32, 64, 128, 256, 512, 1024, 1 << 31, 1<<32 - 1}[rng.IntN(10)]
	}
	data := func(w, h uint32) []byte {
		if rng.IntN(2) == 0 && uint64(w)*uint64(h)*4 <= 4<<20 {
			return make([]byte, w*h*4)
		}
		return make([]byte, rng.IntN(1<<16))
	}
	for i := range 2000 {
		sk := v2193.Skin{SkinImageWidth: size(), SkinImageHeight: size(), CapeImageWidth: size(), CapeImageHeight: size(), PersonaSkin: rng.IntN(2) == 0}
		sk.SkinData, sk.CapeData = data(sk.SkinImageWidth, sk.SkinImageHeight), data(sk.CapeImageWidth, sk.CapeImageHeight)
		for range rng.IntN(4) {
			a := v2193.SkinAnimation{ImageWidth: size(), ImageHeight: size(), AnimationType: uint32(rng.IntN(5)), FrameCount: float32(rng.NormFloat64() * 1e6)}
			a.ImageData = data(a.ImageWidth, a.ImageHeight)
			sk.Animations = append(sk.Animations, a)
		}
		encodable(t, fmt.Sprintf("random skin %d", i), skinToProtocol(protocolToSkin(sk)))
	}
}

// TestSkinChangedByHand sends a skin whose pixels were changed to fill no
// texture: it goes as the empty one rather than as a skin no client reads.
func TestSkinChangedByHand(t *testing.T) {
	s := skin.New(64, 64)
	s.Pix = make([]byte, 10)
	s.Cape = skin.NewCape(64, 32)
	s.Cape.Pix = nil
	pk := skinToProtocol(s)
	if pk.SkinImageWidth != 0 || pk.SkinData != nil || pk.CapeImageWidth != 0 {
		t.Fatalf("skin %dx%d of %d bytes", pk.SkinImageWidth, pk.SkinImageHeight, len(pk.SkinData))
	}
	encodable(t, "changed by hand", pk)
}
