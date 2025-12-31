package item

// FireworkExplosion represents an explosion of a firework.
type FireworkExplosion struct {
	// Shape represents the shape of the explosion.
	Shape FireworkShape
	// Colour is the colour of the explosion.
	Colour Colour
	// Fade is the colour the explosion should fade into. Fades must be set to true in order for this to function.
	Fade Colour
	// Fades is true if the explosion should fade into the fade colour.
	Fades bool
	// Twinkle is true if the explosion should twinkle on explode.
	Twinkle bool
	// Trail is true if the explosion should have a trail.
	Trail bool
}

// EncodeNBT ...
func (f FireworkExplosion) EncodeNBT() map[string]any {
	data := map[string]any{
		"FireworkType":    f.Shape.Uint8(),
		"FireworkColor":   [1]uint8{uint8(invertColour(f.Colour))},
		"FireworkFade":    [0]uint8{},
		"FireworkFlicker": boolByte(f.Twinkle),
		"FireworkTrail":   boolByte(f.Trail),
	}
	if f.Fades {
		data["FireworkFade"] = [1]uint8{uint8(invertColour(f.Fade))}
	}
	return data
}

// DecodeNBT ...
func (f FireworkExplosion) DecodeNBT(data map[string]any) any {
	if rawShape, ok := readUint8(data["FireworkType"]); ok {
		shapes := FireworkShapes()
		if int(rawShape) < len(shapes) {
			f.Shape = shapes[rawShape]
		}
	}
	if twinkle, ok := readUint8(data["FireworkFlicker"]); ok {
		f.Twinkle = twinkle == 1
	}
	if trail, ok := readUint8(data["FireworkTrail"]); ok {
		f.Trail = trail == 1
	}

	colours := data["FireworkColor"]
	if diskColour, ok := colours.([1]uint8); ok {
		f.Colour = invertColourID(int16(diskColour[0]))
	} else if networkColours, ok := colours.([]any); ok {
		if len(networkColours) > 0 {
			if colour, ok := readUint8(networkColours[0]); ok {
				f.Colour = invertColourID(int16(colour))
			}
		}
	}

	if fades, ok := data["FireworkFade"].([1]uint8); ok {
		f.Fade, f.Fades = invertColourID(int16(fades[0])), true
	} else if networkFades, ok := data["FireworkFade"].([]any); ok {
		if len(networkFades) > 0 {
			if fade, ok := readUint8(networkFades[0]); ok {
				f.Fade, f.Fades = invertColourID(int16(fade)), true
			}
		}
	}
	return f
}
