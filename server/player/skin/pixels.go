package skin

// The textures a client sends are trusted for nothing: the functions below
// keep a texture only if its pixels fill it exactly, so that every skin a
// server holds goes to other clients as one they can read, and bound what
// a server keeps of each player. The game draws persona skins itself, at
// sizes no classic skin has; BDS takes any of them as it takes any cape and
// animation, and passes them on as they are.
const (
	// maxPersonaPixels bounds the pixels of a persona skin: 512x512.
	maxPersonaPixels = 512 * 512
	// maxCapePixels bounds the pixels of a cape: 256x256.
	maxCapePixels = 256 * 256
	// maxAnimationPixels bounds the pixels of the texture of an animation,
	// which holds every frame: 512x512.
	maxAnimationPixels = 512 * 512
	// maxFrames bounds the frames of an animation.
	maxFrames = 1024
	// MaxAnimations bounds the animations a skin keeps.
	MaxAnimations = 8
)

// FromPixels returns the skin of a texture as a client sends it, its pixels
// copied. As BDS does, it returns the empty skin, which the game shows as
// a default one, unless the game shows a texture of the size: 64x32, 64x64
// or 128x128 pixels, or, for a persona skin, any size up to 512x512; and
// unless the pixels fill it exactly.
func FromPixels(width, height int, pix []byte, persona bool) Skin {
	if !fills(width, height, pix, maxPersonaPixels) || !validSkinSize(width, height) && !persona {
		return Skin{}
	}
	return Skin{w: width, h: height, Persona: persona, Pix: append([]byte(nil), pix...)}
}

// CapeFromPixels returns the cape of a texture as a client sends it, its
// pixels copied: the empty cape unless the pixels fill a texture of up to
// 256x256 pixels exactly. The capes of the game are 64x32.
func CapeFromPixels(width, height int, pix []byte) Cape {
	if !fills(width, height, pix, maxCapePixels) {
		return Cape{}
	}
	return Cape{w: width, h: height, Pix: append([]byte(nil), pix...)}
}

// AnimationFromPixels returns an animation as a client sends it, its pixels
// copied, and false for one the game does not play: of no type it knows,
// of no frames or of more than 1024, or whose pixels do not fill a texture
// of up to 512x512 pixels exactly. The texture holds every frame.
func AnimationFromPixels(width, height int, pix []byte, t AnimationType, frames, expression int) (Animation, bool) {
	if t < AnimationHead || t > AnimationBody128x128 || frames < 1 || frames > maxFrames || !fills(width, height, pix, maxAnimationPixels) {
		return Animation{}, false
	}
	return Animation{w: width, h: height, aType: t, Pix: append([]byte(nil), pix...), FrameCount: frames, AnimationExpression: expression}, true
}

// fills reports whether pix holds exactly the RGBA values of a texture of
// a size, of no more than max pixels.
func fills(width, height int, pix []byte, max int) bool {
	n, ok := pixelBufferSize(width, height)
	return ok && n/bytesPerPixel <= max && n == len(pix)
}
