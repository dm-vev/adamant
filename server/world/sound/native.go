package sound

// NativeEvent sends a named Bedrock sound event without translating it to a Java sound ID.
type NativeEvent struct {
	SoundType      string
	EntityType     string
	ExtraData      int32
	EntityUniqueID int64

	sound
}
