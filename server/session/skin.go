package session

import (
	"encoding/base64"
	"encoding/json"
	"image"
	"math"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/google/uuid"
)

// A skin is what a client says it is: the functions below trust nothing in
// it. A texture that does not fill its size exactly becomes the empty one,
// as BDS makes it, a cape or an animation that does not becomes none, and
// geometry that is no JSON is left out. Every skin a session holds goes to
// other clients as one they can read, whatever a client sent.

// ClientSkin returns the skin of the client data of a login.
func ClientSkin(data login.ClientData) skin.Skin {
	pix, _ := base64.StdEncoding.DecodeString(data.SkinData)
	s := skin.FromPixels(data.SkinImageWidth, data.SkinImageHeight, pix, data.PersonaSkin)
	if geometry, err := base64.StdEncoding.DecodeString(data.SkinGeometry); err == nil && json.Valid(geometry) {
		s.Model = geometry
	}
	patch, _ := base64.StdEncoding.DecodeString(data.SkinResourcePatch)
	s.ModelConfig, _ = skin.DecodeModelConfig(patch)
	s.PlayFabID = data.PlayFabID
	s.FullID = data.SkinID

	capePix, _ := base64.StdEncoding.DecodeString(data.CapeData)
	s.Cape = skin.CapeFromPixels(data.CapeImageWidth, data.CapeImageHeight, capePix)
	for _, a := range data.AnimatedImageData {
		t, ok := animationType(int64(a.Type))
		if !ok || len(s.Animations) == skin.MaxAnimations {
			continue
		}
		animPix, _ := base64.StdEncoding.DecodeString(a.Image)
		if anim, ok := skin.AnimationFromPixels(a.ImageWidth, a.ImageHeight, animPix, t, frameCount(a.Frames), a.AnimationExpression); ok {
			s.Animations = append(s.Animations, anim)
		}
	}
	return s
}

// protocolToSkin returns the skin a PlayerSkin packet carries.
func protocolToSkin(sk v2193.Skin) skin.Skin {
	s := skin.FromPixels(int(sk.SkinImageWidth), int(sk.SkinImageHeight), sk.SkinData, sk.PersonaSkin)
	if json.Valid(sk.SkinGeometry) {
		s.Model = append([]byte(nil), sk.SkinGeometry...)
	}
	s.ModelConfig, _ = skin.DecodeModelConfig(sk.SkinResourcePatch)
	s.PlayFabID = sk.PlayFabID
	s.FullID = sk.FullID

	s.Cape = skin.CapeFromPixels(int(sk.CapeImageWidth), int(sk.CapeImageHeight), sk.CapeData)
	for _, a := range sk.Animations {
		t, ok := animationType(int64(a.AnimationType))
		if !ok || len(s.Animations) == skin.MaxAnimations {
			continue
		}
		if anim, ok := skin.AnimationFromPixels(int(a.ImageWidth), int(a.ImageHeight), a.ImageData, t, frameCount(float64(a.FrameCount)), int(a.ExpressionType)); ok {
			s.Animations = append(s.Animations, anim)
		}
	}
	return s
}

// animationType returns the animation type of the protocol's, and false for
// one the game does not know.
func animationType(t int64) (skin.AnimationType, bool) {
	switch t {
	case v2193.SkinAnimationHead:
		return skin.AnimationHead, true
	case v2193.SkinAnimationBody32x32:
		return skin.AnimationBody32x32, true
	case v2193.SkinAnimationBody128x128:
		return skin.AnimationBody128x128, true
	}
	return 0, false
}

// frameCount returns the frames a client names as an int, zero for a count
// out of the range of one.
func frameCount(f float64) int {
	if !(f >= 1 && f <= math.MaxInt32) {
		return 0
	}
	return int(f)
}

// skinToProtocol returns a skin as the protocol holds it. A texture whose
// pixels do not fill it, which a skin changed by hand may have, goes as
// the empty one, so that what goes to a client is always one it can read.
func skinToProtocol(s skin.Skin) v2193.Skin {
	var animations []v2193.SkinAnimation
	for _, animation := range s.Animations {
		if !fills(animation.Bounds(), animation.Pix) {
			continue
		}
		protocolAnim := v2193.SkinAnimation{
			ImageWidth:     uint32(animation.Bounds().Dx()),
			ImageHeight:    uint32(animation.Bounds().Dy()),
			ImageData:      animation.Pix,
			FrameCount:     float32(animation.FrameCount),
			ExpressionType: uint32(animation.AnimationExpression),
		}
		switch animation.Type() {
		case skin.AnimationHead:
			protocolAnim.AnimationType = v2193.SkinAnimationHead
		case skin.AnimationBody32x32:
			protocolAnim.AnimationType = v2193.SkinAnimationBody32x32
		case skin.AnimationBody128x128:
			protocolAnim.AnimationType = v2193.SkinAnimationBody128x128
		}
		animations = append(animations, protocolAnim)
	}
	bounds, pix := s.Bounds(), s.Pix
	if !fills(bounds, pix) {
		bounds, pix = image.Rectangle{}, nil
	}
	capeBounds, capePix := s.Cape.Bounds(), s.Cape.Pix
	if !fills(capeBounds, capePix) {
		capeBounds, capePix = image.Rectangle{}, nil
	}

	fullID := s.FullID
	if fullID == "" {
		fullID = uuid.New().String()
	}
	model := s.Model
	if len(model) == 0 {
		model = []byte("{}")
	}
	return v2193.Skin{
		PlayFabID:                 s.PlayFabID,
		SkinID:                    uuid.New().String(),
		SkinResourcePatch:         s.ModelConfig.Encode(),
		SkinImageWidth:            uint32(bounds.Dx()),
		SkinImageHeight:           uint32(bounds.Dy()),
		SkinData:                  pix,
		CapeImageWidth:            uint32(capeBounds.Dx()),
		CapeImageHeight:           uint32(capeBounds.Dy()),
		CapeData:                  capePix,
		SkinGeometry:              model,
		PersonaSkin:               s.Persona,
		CapeID:                    uuid.New().String(),
		FullID:                    fullID,
		Animations:                animations,
		Trusted:                   true,
		OverrideAppearance:        true,
		GeometryDataEngineVersion: []byte(v2193.CurrentVersion),
	}
}

// fills reports whether pix holds exactly the RGBA values of a texture of
// the bounds.
func fills(b image.Rectangle, pix []byte) bool {
	w, h := b.Dx(), b.Dy()
	return w >= 0 && h >= 0 && (w == 0 || h <= math.MaxInt/4/w) && w*h*4 == len(pix)
}

// newListEntry returns the PlayerList packet adding the player of the
// session with a skin, for every session.
func (s *Session) newListEntry(sk skin.Skin) *sharedPacket {
	return &sharedPacket{Packet: &v2193.PlayerListPacket{Entries: []v2193.PlayerListEntry{{
		ActionType:     v2193.PlayerListActionAdd,
		UUID:           s.ent.UUID(),
		EntityUniqueID: s.ent.UniqueID(),
		Username:       s.conn.IdentityData().DisplayName,
		XUID:           s.conn.IdentityData().XUID,
		BuildPlatform:  int32(v2193.DeviceUnknown),
		Skin:           skinToProtocol(sk),
	}}}}
}

// playerListEntry returns the PlayerList packet adding the player of the
// session, made with an empty skin for a session that never set one.
func (s *Session) playerListEntry() *sharedPacket {
	if p := s.listEntry.Load(); p != nil {
		return p
	}
	s.listEntry.CompareAndSwap(nil, s.newListEntry(skin.Skin{}))
	return s.listEntry.Load()
}

// ShowSkin shows every session the new skin of the player of the session,
// whether it sees the player or only lists it, with one PlayerSkin encoded
// once, and lists the player with it for sessions that join later.
func (s *Session) ShowSkin(sk skin.Skin) {
	if s == Nop {
		return
	}
	s.listEntry.Store(s.newListEntry(sk))
	pk := &sharedPacket{Packet: &v2193.PlayerSkinPacket{UUID: s.ent.UUID(), Skin: skinToProtocol(sk)}}
	for _, other := range sessions.all() {
		other.writePacket(pk)
	}
}
