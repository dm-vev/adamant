package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPlayerGroundDetection(t *testing.T) {
	tests := []struct {
		name     string
		position mgl64.Vec3
		delta    mgl64.Vec3
		blockPos cube.Pos
		ground   bool
	}{
		{
			name:     "falling beside wall",
			position: mgl64.Vec3{0.5, 4, 0.5},
			delta:    mgl64.Vec3{-0.3, -1},
			blockPos: cube.Pos{1, 4, 0},
		},
		{
			name:     "landing after descent",
			position: mgl64.Vec3{0.5, 1.25, 0.5},
			delta:    mgl64.Vec3{0.4, -0.3},
			blockPos: cube.Pos{},
			ground:   true,
		},
		{
			name:     "moving horizontally on ground",
			position: mgl64.Vec3{0.5, 1.02, 0.5},
			delta:    mgl64.Vec3{0.5},
			blockPos: cube.Pos{},
			ground:   true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()

			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(test.blockPos, block.Cobblestone{}, nil)
				handle := world.EntitySpawnOpts{Position: test.position}.New(Type, Config{Position: test.position})
				p := tx.AddEntity(handle).(*Player)
				if got := p.checkOnGround(test.delta); got != test.ground {
					t.Fatalf("checkOnGround(%v) = %v, want %v", test.delta, got, test.ground)
				}
			})
		})
	}
}
