package skin

import (
	"math"
	"testing"
)

func TestFromPixels(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		n             int
		persona, want bool
	}{
		{"64x32", 64, 32, 64 * 32 * 4, false, true},
		{"64x64", 64, 64, 64 * 64 * 4, false, true},
		{"128x128", 128, 128, 128 * 128 * 4, false, true},
		{"persona 256x256", 256, 256, 256 * 256 * 4, true, true},
		{"persona 128x64", 128, 64, 128 * 64 * 4, true, true},
		{"persona 512x512", 512, 512, 512 * 512 * 4, true, true},
		{"256x256", 256, 256, 256 * 256 * 4, false, false},
		{"128x64", 128, 64, 128 * 64 * 4, false, false},
		{"persona 1024x1024", 1024, 1024, 1024 * 1024 * 4, true, false},
		{"persona short", 256, 256, 1000, true, false},
		{"64x64 long", 64, 64, 64*64*4 + 1, false, false},
		{"negative", -64, -64, 64 * 64 * 4, true, false},
		{"overflowing", math.MaxInt, 2, 0, true, false},
	} {
		pix := make([]byte, tc.n)
		s := FromPixels(tc.width, tc.height, pix, tc.persona)
		if got := s.Bounds().Dx() == tc.width && s.Bounds().Dy() == tc.height && len(s.Pix) == tc.n; got != tc.want {
			t.Errorf("%s: kept %v, want %v", tc.name, got, tc.want)
		}
		if !tc.want && (s.Bounds().Dx() != 0 || s.Pix != nil || s.Persona) {
			t.Errorf("%s: not the empty skin: %dx%d, %d bytes", tc.name, s.Bounds().Dx(), s.Bounds().Dy(), len(s.Pix))
		}
		if tc.want && len(pix) > 0 {
			// The pixels are copied: the client's buffer is not the skin's.
			pix[0] = 1
			if s.Pix[0] != 0 {
				t.Errorf("%s: pixels shared", tc.name)
			}
		}
	}
}

func TestFromNoPixels(t *testing.T) {
	if s := FromPixels(0, 0, nil, true); s.Bounds().Dx() != 0 || s.Pix != nil || s.Persona {
		t.Fatalf("a texture of no pixels is not the empty skin: %+v", s.Bounds())
	}
}

func TestCapeFromPixels(t *testing.T) {
	for _, tc := range []struct {
		width, height, n int
		want             bool
	}{
		{64, 32, 64 * 32 * 4, true},
		{32, 64, 64 * 32 * 4, true},
		{256, 256, 256 * 256 * 4, true},
		{512, 512, 512 * 512 * 4, false},
		{64, 32, 100, false},
		{-64, -32, 64 * 32 * 4, false},
	} {
		c := CapeFromPixels(tc.width, tc.height, make([]byte, tc.n))
		if got := len(c.Pix) == tc.n && c.Bounds().Dx() == tc.width; got != tc.want {
			t.Errorf("%dx%d of %d bytes: kept %v, want %v", tc.width, tc.height, tc.n, got, tc.want)
		}
	}
	if c := NewCape(64, 32); len(c.Pix) != 64*32*4 {
		t.Fatal("a cape of the game is not made")
	}
}

func TestAnimationFromPixels(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		n             int
		t             AnimationType
		frames        int
		want          bool
	}{
		{"face of two frames", 32, 64, 32 * 64 * 4, AnimationHead, 2, true},
		{"body", 128, 128, 128 * 128 * 4, AnimationBody128x128, 1, true},
		{"pixels of every frame twice", 32, 32, 32 * 32 * 4 * 2, AnimationHead, 2, false},
		{"no frames", 32, 64, 32 * 64 * 4, AnimationHead, 0, false},
		{"too many frames", 32, 64, 32 * 64 * 4, AnimationHead, 1 << 20, false},
		{"unknown type", 32, 64, 32 * 64 * 4, AnimationType(7), 1, false},
		{"too large", 1024, 1024, 1024 * 1024 * 4, AnimationHead, 1, false},
		{"negative", -32, -64, 32 * 64 * 4, AnimationHead, 1, false},
	} {
		a, ok := AnimationFromPixels(tc.width, tc.height, make([]byte, tc.n), tc.t, tc.frames, 1)
		if ok != tc.want || ok && (a.FrameCount != tc.frames || len(a.Pix) != tc.n || a.Type() != tc.t) {
			t.Errorf("%s: %v, %+v", tc.name, ok, a.Bounds())
		}
	}
}
