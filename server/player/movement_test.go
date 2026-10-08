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

func TestPlayerMovementVibrations(t *testing.T) {
	for _, test := range []struct {
		name                                      string
		water, swimming, climbing, seam, displace bool
		sneakingMoves                             int
		want                                      []bool
	}{
		{name: "fractional cadence", want: []bool{false, true, true, false, true, true, true, false}},
		{name: "crouching does not accumulate distance", sneakingMoves: 1, want: []bool{false, true}},
		{name: "walking in water still steps", water: true, want: []bool{false, true}},
		{name: "swimming emits swim", water: true, swimming: true, want: []bool{false, true}},
		{name: "vertical climbing counts distance", climbing: true, want: []bool{false, true}},
		{name: "nearest support resolves wool seam", seam: true, want: []bool{true}},
		{name: "server displacement emits movement", displace: true, want: []bool{false, true, true}},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				for x := -2; x <= 13; x++ {
					tx.SetBlock(cube.Pos{x, 64, 0}, block.Stone{}, nil)
					if test.water {
						tx.SetLiquid(cube.Pos{x, 65, 0}, block.Water{Depth: 8, Still: true})
					}
				}
				position, delta := mgl64.Vec3{.5, 65, .5}, mgl64.Vec3{1.25, 0, 0}
				if test.climbing {
					for y := 65; y <= 70; y++ {
						tx.SetBlock(cube.Pos{0, y, 0}, block.Ladder{Facing: cube.North}, nil)
					}
					delta = mgl64.Vec3{0, 1.25, 0}
				}
				if test.seam {
					wool, ok := w.BlockRegistry().BlockByName("minecraft:white_wool", nil)
					if !ok {
						t.Fatal("missing wool")
					}
					tx.SetBlock(cube.Pos{0, 64, 0}, wool, nil)
					position, delta = mgl64.Vec3{-1, 65, .5}, mgl64.Vec3{2, 0, 0}
				}
				handle := world.EntitySpawnOpts{}.New(Type, Config{Position: position, GameMode: world.GameModeSurvival})
				p := tx.AddEntity(handle).(*Player)
				if test.swimming {
					p.StartSwimming()
				}
				if test.sneakingMoves != 0 {
					p.StartSneaking()
					for range test.sneakingMoves {
						p.Move(delta, 0, 0)
					}
					p.StopSneaking()
				}
				sensorPos := cube.Pos{0, 65, 4}
				for i, want := range test.want {
					tx.SetBlock(sensorPos, block.CalibratedSculkSensor{}, nil)
					if test.displace {
						p.Displace(delta)
					} else {
						p.Move(delta, 0, 0)
					}
					data := tx.Block(sensorPos).(block.CalibratedSculkSensor).VibrationData()
					got := data != nil && data.Selector.HasCandidate
					if got != want {
						t.Fatalf("move %d vibration=%v, want %v", i+1, got, want)
					}
					if got {
						event := world.GameEventStep
						if test.swimming {
							event = world.GameEventSwim
						}
						if data.Selector.Candidate.Event != event {
							t.Fatalf("move %d event=%s, want %s", i+1, data.Selector.Candidate.Event, event)
						}
					}
				}
			})
		})
	}
}
