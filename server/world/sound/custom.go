package sound

// Custom is a sound identified by name: the ID of a Java Edition sound event,
// such as "entity.allay.item_given", which each adapter maps to a sound
// of its edition, or the name of a sound defined by a resource pack, which
// adapters pass on as it is.
type Custom struct {
	// Name is the identifier of the sound.
	Name string
	// Volume is the volume of the sound.
	Volume float64
	// Pitch is the pitch of the sound.
	Pitch float64

	sound
}
