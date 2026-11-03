package session

import "github.com/df-mc/dragonfly/server/world/sound"

// bedrockInstrument returns the ID of Bedrock's note block instrument that
// BlockEvent and the note LevelSoundEvent send for an instrument. The
// protocol has no constants of them.
func bedrockInstrument(i sound.Instrument) int32 {
	switch i {
	case sound.Piano():
		return 0
	case sound.BassDrum():
		return 1
	case sound.Snare():
		return 2
	case sound.ClicksAndSticks():
		return 3
	case sound.Bass():
		return 4
	case sound.Flute():
		return 5
	case sound.Bell():
		return 6
	case sound.Guitar():
		return 7
	case sound.Chimes():
		return 8
	case sound.Xylophone():
		return 9
	case sound.IronXylophone():
		return 10
	case sound.CowBell():
		return 11
	case sound.Didgeridoo():
		return 12
	case sound.Bit():
		return 13
	case sound.Banjo():
		return 14
	case sound.Pling():
		return 15
	}
	return 0
}
