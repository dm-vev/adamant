package sound

import "github.com/df-mc/dragonfly/server/world"

// Event is the sound of an event, which a client plays as made by an entity
// of a type, or by a block, from which it picks the variant of the sound: a
// horse's "ambient", the "button.click_on" of an oak button.
//
// Name names the event as the level sound events of Bedrock Edition do.
// They are the names the game has for these sounds. The adapter of another
// edition maps a name, with the entity type or block, to a sound of its own.
type Event struct {
	// Name is the name of the event.
	Name string
	// Entity is the name the type of the entity making the sound is
	// registered by (world.EntityType.EncodeEntity), or empty for a sound
	// that is not one of an entity type.
	Entity string
	// Source is the entity making the sound, for a client to play it from;
	// nil if the sound comes from no entity in particular.
	Source *world.EntityHandle
	// Block is the block whose variant of the sound is played: the state a
	// button or plate has after it clicked, the block a container sound
	// comes from. Nil for a sound of no block.
	Block world.Block

	sound
}

// CreakingHeartTrail is the sound of the trail between a creaking heart and
// its creaking, at Progress of the way from the creaking to the heart, from
// 0.2 to 1.
type CreakingHeartTrail struct {
	Progress float64
	sound
}
