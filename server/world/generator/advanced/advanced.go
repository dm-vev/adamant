package advanced

import (
	"github.com/df-mc/dragonfly/server/world/generator/advanced/end"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/nether"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

// NewEnd returns the Java 26.3-based End generator.
func NewEnd(seed int64) *end.End {
	return end.New(seed)
}

// NewOverworld returns the Java 26.3-based Overworld generator.
func NewOverworld(seed int64) *overworld.Overworld {
	return overworld.New(seed)
}

// NewNether returns the Java 26.3-based Nether generator.
func NewNether(seed int64) *nether.Nether {
	return nether.New(seed)
}
