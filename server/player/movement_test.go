package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

type blockedMovementGenerator struct {
	started chan struct{}
	release chan struct{}
}

func (g *blockedMovementGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{} }

func (g *blockedMovementGenerator) GenerateChunk(pos world.ChunkPos, _ *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	if pos == (world.ChunkPos{2, 0}) {
		close(g.started)
		<-g.release
	}
	return world.GenerationResult{}
}

func TestPlayerMoveDoesNotWaitForGeneratingCollisionChunk(t *testing.T) {
	g := &blockedMovementGenerator{started: make(chan struct{}), release: make(chan struct{})}
	w := world.Config{Generator: g, GeneratorWorkers: 1}.New()
	pos := mgl64.Vec3{31.5, 64, 0.5}
	var p *Player
	var loader *world.Loader
	var ready <-chan struct{}
	requested := make(chan struct{})
	aboutToMove := make(chan struct{})
	moveDone := make(chan struct{})
	task := w.Exec(func(tx *world.Tx) {
		p = tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos, GameMode: world.GameModeSurvival})).(*Player)
		loader = world.NewLoader(1, w, world.NopViewer{})
		loader.Move(tx, pos)
		loader.Load(tx, 9)
		ready, _ = tx.ChunkReadySignal(world.ChunkPos{2, 0})
		close(requested)
		select {
		case <-g.started:
		case <-g.release:
			close(moveDone)
			return
		}
		close(aboutToMove)
		p.Move(mgl64.Vec3{0.2}, 0, 0)
		close(moveDone)
	})
	released := false
	release := func() {
		if !released {
			close(g.release)
			released = true
		}
	}
	defer func() {
		release()
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	}()
	select {
	case <-aboutToMove:
	case <-time.After(5 * time.Second):
		t.Fatal("generation did not start")
	}
	select {
	case <-moveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("movement waited for an unready chunk")
	}
	select {
	case <-task:
	case <-time.After(2 * time.Second):
		t.Fatal("movement transaction did not finish")
	}
	var moved mgl64.Vec3
	<-w.Exec(func(*world.Tx) { moved = p.Position() })
	if moved[0] != pos[0]+0.2 {
		t.Fatalf("movement position = %v, want %v", moved, pos[0]+0.2)
	}

	release()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("chunk did not become ready after generation was released")
	}
	var collided, burned bool
	<-w.Exec(func(tx *world.Tx) {
		previous := p.tx
		p.tx = tx
		defer func() { p.tx = previous }()
		tx.SetBlock(cube.Pos{34, 64, 0}, block.Cobblestone{}, nil)
		p.Move(mgl64.Vec3{1.4}, 0, 0)
		collided = p.collidedHorizontally
		tx.SetBlock(cube.Pos{33, 64, 0}, block.Lava{Still: true, Depth: 8}, nil)
		health := p.Health()
		p.Move(mgl64.Vec3{0.1}, 0, 0)
		burned = p.Health() < health
	})
	if !collided {
		t.Fatal("ready-world movement did not detect the collision block")
	}
	if !burned {
		t.Fatal("ready-world movement did not apply inside-lava damage")
	}
}
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
