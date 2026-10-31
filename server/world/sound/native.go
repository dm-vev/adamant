package sound

import "github.com/df-mc/dragonfly/server/block/cube"

// NativeEvent sends a named Bedrock sound event without translating it to a Java sound ID.
type NativeEvent struct {
	SoundType      string
	EntityType     string
	ExtraData      int32
	EntityUniqueID int64
	// AfterBlock queues the sound behind the pending mutation at this position.
	AfterBlock *cube.Pos

	sound
}
