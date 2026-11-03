package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world/sound"
)

// TestInstrumentValues holds every note block instrument, and the zero
// Instrument, to the ID Bedrock clients were sent for it when
// Instrument.Int32 returned it: 0 to 15 in the order of the constructors.
func TestInstrumentValues(t *testing.T) {
	checkGameValues(t, []gameValue{
		{"Piano", int64(bedrockInstrument(sound.Piano())), 0},
		{"BassDrum", int64(bedrockInstrument(sound.BassDrum())), 1},
		{"Snare", int64(bedrockInstrument(sound.Snare())), 2},
		{"ClicksAndSticks", int64(bedrockInstrument(sound.ClicksAndSticks())), 3},
		{"Bass", int64(bedrockInstrument(sound.Bass())), 4},
		{"Flute", int64(bedrockInstrument(sound.Flute())), 5},
		{"Bell", int64(bedrockInstrument(sound.Bell())), 6},
		{"Guitar", int64(bedrockInstrument(sound.Guitar())), 7},
		{"Chimes", int64(bedrockInstrument(sound.Chimes())), 8},
		{"Xylophone", int64(bedrockInstrument(sound.Xylophone())), 9},
		{"IronXylophone", int64(bedrockInstrument(sound.IronXylophone())), 10},
		{"CowBell", int64(bedrockInstrument(sound.CowBell())), 11},
		{"Didgeridoo", int64(bedrockInstrument(sound.Didgeridoo())), 12},
		{"Bit", int64(bedrockInstrument(sound.Bit())), 13},
		{"Banjo", int64(bedrockInstrument(sound.Banjo())), 14},
		{"Pling", int64(bedrockInstrument(sound.Pling())), 15},
		{"Instrument{}", int64(bedrockInstrument(sound.Instrument{})), 0},
	})
}
