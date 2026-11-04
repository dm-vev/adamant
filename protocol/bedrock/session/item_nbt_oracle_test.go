package session

import (
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// The encodings TestItemNBTCorpus holds to the corpus: the adapter's for
// Bedrock's wire and the game's for the world format. The corpus was recorded
// with item.WriteNBT(s, false), WriteNBT(s, true), ReadNBT(data, &s, br) and
// ReadNBT(data, nil, br) in their place.
func corpusNetworkNBT(s item.Stack) map[string]any { return networkItemNBT(s) }
func corpusDiskNBT(s item.Stack) map[string]any    { return item.WriteNBT(s) }
func corpusReadNetworkNBT(data map[string]any, s item.Stack, _ world.BlockRegistry) item.Stack {
	return readNetworkItemNBT(data, s)
}
func corpusReadDiskNBT(data map[string]any, br world.BlockRegistry) item.Stack {
	return item.ReadNBT(data, br)
}
