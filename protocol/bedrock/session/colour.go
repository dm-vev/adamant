package session

import (
	"encoding/binary"
	"image/color"
)

// int32FromRGBA converts a color.RGBA into an int32. These int32s are present in, for example, signs.
func int32FromRGBA(x color.RGBA) int32 {
	if x.R == 0 && x.G == 0 && x.B == 0 {
		// Default to black colour. The default (0x000000) is a transparent colour. Text with this colour will not show
		// up on the sign.
		return int32(-0x1000000)
	}
	return int32(binary.BigEndian.Uint32([]byte{x.A, x.R, x.G, x.B}))
}
