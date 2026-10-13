package effect

import "image/color"

// WindCharged makes its holder burst into a gust of wind when it is killed.
var WindCharged windCharged

type windCharged struct{ nopLasting }

// RGBA returns the Java Wind Charged effect colour.
func (windCharged) RGBA() color.RGBA { return color.RGBA{R: 0xbd, G: 0xc9, B: 0xff, A: 0xff} }

// Weaving spreads cobwebs around its holder when it is killed.
var Weaving weaving

type weaving struct{ nopLasting }

// RGBA returns the Java Weaving effect colour.
func (weaving) RGBA() color.RGBA { return color.RGBA{R: 0x78, G: 0x69, B: 0x5a, A: 0xff} }

// Oozing releases two slimes when its holder is killed.
var Oozing oozing

type oozing struct{ nopLasting }

// RGBA returns the Java Oozing effect colour.
func (oozing) RGBA() color.RGBA { return color.RGBA{R: 0x99, G: 0xff, B: 0xa3, A: 0xff} }

// Infested gives its holder a chance to release silverfish when hurt.
var Infested infested

type infested struct{ nopLasting }

// RGBA returns the Java Infested effect colour.
func (infested) RGBA() color.RGBA { return color.RGBA{R: 0x8c, G: 0x9b, B: 0x8c, A: 0xff} }

// BreathOfTheNautilus lets its holder breathe underwater without refilling
// its air supply there.
var BreathOfTheNautilus breathOfTheNautilus

type breathOfTheNautilus struct{ nopLasting }

// RGBA returns the Java Breath of the Nautilus effect colour.
func (breathOfTheNautilus) RGBA() color.RGBA { return color.RGBA{G: 0xff, B: 0xee, A: 0xff} }
