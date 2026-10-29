package v2193

// Positions as the packets hold them.
type (
	// BlockPos is the position of a block.
	BlockPos = [3]int32
	// ChunkPos is the position of a chunk column.
	ChunkPos = [2]int32
	// SubChunkPos is the position of a sub-chunk.
	SubChunkPos = [3]int32
)

// NewMetadata returns metadata with the flags of an entity all unset.
func NewMetadata() Metadata {
	return Metadata{
		EntityDataKeyFlags:       int64(0),
		EntityDataKeyFlagsTwo:    int64(0),
		EntityDataKeyPlayerFlags: byte(0),
	}
}

// SetFlag sets the flag of an index in the flags of a key.
func (m Metadata) SetFlag(key uint32, index uint8) {
	v := m[key]
	switch key {
	case EntityDataKeyPlayerFlags:
		m[key] = v.(byte) | 1<<index
	default:
		m[key] = v.(int64) | 1<<int64(index)
	}
}

// UnsetFlag unsets the flag of an index in the flags of a key.
func (m Metadata) UnsetFlag(key uint32, index uint8) {
	v := m[key]
	switch key {
	case EntityDataKeyPlayerFlags:
		m[key] = v.(byte) &^ (1 << index)
	default:
		m[key] = v.(int64) &^ (1 << int64(index))
	}
}

// Flag reports whether the flag of an index in the flags of a key is set.
func (m Metadata) Flag(key uint32, index uint8) bool {
	v := m[key]
	switch key {
	case EntityDataKeyPlayerFlags:
		return v.(byte)&(1<<index) != 0
	default:
		return v.(int64)&(1<<int64(index)) != 0
	}
}
