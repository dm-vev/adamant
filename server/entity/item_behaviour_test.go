package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
)

func TestHazardConsumesItems(t *testing.T) {
	tests := []struct {
		name   string
		block  world.Block
		liquid world.Liquid
		want   bool
	}{
		{name: "fire block", block: block.Fire{}, want: true},
		{name: "lava block", block: block.Lava{}, want: true},
		{name: "lava liquid", liquid: block.Lava{Depth: 8}, want: true},
		{name: "air", block: block.Air{}, want: false},
		{name: "water", liquid: block.Water{Depth: 8}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hazardConsumesItems(tt.block, tt.liquid); got != tt.want {
				t.Fatalf("hazardConsumesItems(%T, %T) = %v, want %v", tt.block, tt.liquid, got, tt.want)
			}
		})
	}
}
