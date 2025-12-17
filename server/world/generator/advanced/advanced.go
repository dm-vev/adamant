package advanced

import (
	"github.com/df-mc/dragonfly/server/world/generator/advanced/end"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
)

// NewEnd returns a Java Edition 1.12-style End terrain generator.
func NewEnd(seed int64) *end.End {
	return end.New(seed)
}

// NewOverworld returns a Java Edition 1.12-style Overworld terrain generator.
func NewOverworld(seed int64) *overworld.Overworld {
	return overworld.New(seed)
}
