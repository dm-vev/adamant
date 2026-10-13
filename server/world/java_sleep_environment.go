package world

// IsDarkOutside evaluates Java 26.3's Overworld SKY_LIGHT_LEVEL timeline and
// Level.isDarkOutside predicate. Fixed-time dimensions are never dark outside.
func (tx *Tx) IsDarkOutside() bool {
	if tx.World().Dimension() != Overworld {
		return false
	}
	return int32(float32(15)-javaSleepSkyLight(tx.World().Time(), tx.Raining(), tx.Thundering())) >= 4
}

// javaSleepSkyLight follows the registered minecraft:day MULTIPLY track and
// WeatherAttributes' full-strength rain/thunder ALPHA_BLEND layers. World
// weather currently represents either inactive or full-strength conditions.
func javaSleepSkyLight(tick int, raining, thundering bool) float32 {
	tick %= TimeFull
	if tick < 0 {
		tick += TimeFull
	}
	const night = float32(.26666668)
	var modifier float32
	switch {
	case tick < 133:
		alpha := float32(tick+1670) / 1803
		modifier = night + alpha*(float32(1)-night)
	case tick < 11867:
		modifier = 1
	case tick < 13670:
		alpha := float32(tick-11867) / 1803
		modifier = float32(1) + alpha*(night-float32(1))
	case tick < 22330:
		modifier = night
	default:
		alpha := float32(tick-22330) / 1803
		modifier = night + alpha*(float32(1)-night)
	}
	light := float32(15) * modifier
	if thundering {
		return light + float32(.52734375)*(float32(4)-light)
	}
	if raining {
		return light + float32(.3125)*(float32(4)-light)
	}
	return light
}
