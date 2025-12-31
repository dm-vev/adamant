package skin

const bytesPerPixel = 4

// pixelBufferSize returns the RGBA byte slice length for a given image size.
// It returns false if width/height are non-positive or the size would overflow.
func pixelBufferSize(width, height int) (int, bool) {
	if width <= 0 || height <= 0 {
		return 0, false
	}
	maxInt := int(^uint(0) >> 1)
	if width > maxInt/bytesPerPixel/height {
		return 0, false
	}
	return width * height * bytesPerPixel, true
}

func validSkinSize(width, height int) bool {
	switch {
	case width == 64 && (height == 32 || height == 64):
		return true
	case width == 128 && height == 128:
		return true
	default:
		return false
	}
}

func validCapeSize(width, height int) bool {
	return width == 32 && height == 64
}
