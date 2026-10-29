package skin

import (
	"image"
	"image/color"
)

// Cape represents the cape that a skin may additionally have. A cape of the game is 64x32 pixels; a cape
// may be empty.
type Cape struct {
	w, h int

	// Pix holds the colour data of the cape in an RGBA byte array, similarly to the way that the pixels of
	// a Skin are stored: width * height * 4 bytes.
	Pix []uint8
}

// NewCape initialises a new Cape using the width and height passed. Invalid dimensions return an empty cape
// to avoid panics from invalid allocations.
func NewCape(width, height int) Cape {
	if !validCapeSize(width, height) {
		return Cape{}
	}
	pixelCount, ok := pixelBufferSize(width, height)
	if !ok {
		return Cape{}
	}
	return Cape{w: width, h: height, Pix: make([]uint8, pixelCount)}
}

// ColorModel ...
func (c Cape) ColorModel() color.Model {
	return color.RGBAModel
}

// Bounds returns the bounds of the cape, 0x0 for an empty cape.
func (c Cape) Bounds() image.Rectangle {
	return image.Rectangle{
		Max: image.Point{X: c.w, Y: c.h},
	}
}

// At returns the colour at a given position in the cape, provided the X and Y are within the bounds of the
// cape passed during construction.
// The concrete type returned by At is a color.RGBA value.
func (c Cape) At(x, y int) color.Color {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		// Return a zero colour for out-of-bounds access to avoid panics in callers.
		return color.RGBA{}
	}
	offset := x*4 + c.w*y*4
	if offset+3 >= len(c.Pix) {
		// Guard against invalid or truncated pixel data to avoid panics.
		return color.RGBA{}
	}
	return color.RGBA{
		R: c.Pix[offset],
		G: c.Pix[offset+1],
		B: c.Pix[offset+2],
		A: c.Pix[offset+3],
	}
}
