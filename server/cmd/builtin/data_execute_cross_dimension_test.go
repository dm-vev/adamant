package builtin

import (
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"iter"
	"sync"
	"testing"
	"time"
)

type executeDimensionAdapter struct {
	serverAdapter
	world, nether, end *world.World
}

func (a executeDimensionAdapter) Players(*world.Tx) iter.Seq[*player.Player] {
	return func(yield func(*player.Player) bool) {}
}
func (a executeDimensionAdapter) World() *world.World           { return a.world }
func (a executeDimensionAdapter) Nether() *world.World          { return a.nether }
func (a executeDimensionAdapter) End() *world.World             { return a.end }
func (executeDimensionAdapter) PlayerIsOperator(uuid.UUID) bool { return true }

type deferWaitSource struct {
	executeTestSource
	mu      sync.Mutex
	pending int
	done    chan struct{}
}

func newDeferWaitSource(pos mgl64.Vec3) *deferWaitSource {
	return &deferWaitSource{
		executeTestSource: executeTestSource{position: pos},
		done:              make(chan struct{}),
	}
}

func (s *deferWaitSource) Defer() func() {
	s.mu.Lock()
	s.pending++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.pending--
			finished := s.pending == 0
			s.mu.Unlock()
			if finished {
				close(s.done)
			}
		})
	}
}

func (s *deferWaitSource) wait(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	p := s.pending
	s.mu.Unlock()
	if p == 0 {
		return
	}
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for cross-world execute command")
	}
}

func TestExecuteInTeleportMovesPlayerAcrossDimensions(t *testing.T) {
	overworld := world.Config{RandomTickSpeed: -1}.New()
	nether := world.Config{Dim: world.Nether, RandomTickSpeed: -1}.New()
	end := world.Config{Dim: world.End, RandomTickSpeed: -1}.New()
	defer overworld.Close()
	defer nether.Close()
	defer end.Close()
	adapter := executeDimensionAdapter{world: overworld, nether: nether, end: end}
	previousServer := commandServer
	previousTP, hadTP := cmd.ByAlias("tp")
	previousEffect, hadEffect := cmd.ByAlias("effect")
	previousGive, hadGive := cmd.ByAlias("give")
	defer func() {
		commandServer = previousServer
		if hadTP {
			cmd.Register(previousTP)
		}
		if hadEffect {
			cmd.Register(previousEffect)
		}
		if hadGive {
			cmd.Register(previousGive)
		}
	}()
	commandServer = adapter
	cmd.Register(cmd.New("effect", "", nil, effectCommand{}, clearEffectCommand{}))
	cmd.Register(newGiveCommand())
	cmd.Register(cmd.New("tp", "", nil, teleportPositionCommand{srv: adapter}, teleportSelfPositionCommand{srv: adapter}))
	execute := cmd.New("execute", "", nil, executeCommand{})
	for _, test := range []struct {
		name string
		from *world.World
		into string
		to   *world.World
	}{
		{name: "overworld to nether", from: overworld, into: "minecraft:the_nether", to: nether},
		{name: "overworld to end", from: overworld, into: "minecraft:the_end", to: end},
		{name: "nether to overworld", from: nether, into: "minecraft:overworld", to: overworld},
	} {
		t.Run(test.name, func(t *testing.T) {
			pos := mgl64.Vec3{300000, 70, 300000}
			var handle *world.EntityHandle
			<-test.from.Exec(func(tx *world.Tx) {
				p := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(player.Type, player.Config{Name: "Executor", Position: mgl64.Vec3{0, 64, 0}, GameMode: world.GameModeCreative})).(*player.Player)
				handle = p.H()
			})
			runExec := func(line string) {
				waitSrc := newDeferWaitSource(mgl64.Vec3{0, 64, 0})
				<-test.from.Exec(func(tx *world.Tx) {
					p, _ := handle.Entity(tx)
					src := cmd.WithContext(waitSrc, cmd.ExecutionContext{Executor: p, ExecutorHandle: handle, Position: p.Position(), Rotation: p.Rotation(), World: tx.World(), Anchor: cmd.Feet})
					execute.Execute(line, src, tx)
				})
				waitSrc.wait(t)
			}
			runExec("in " + test.into + " run effect give @s speed 10")
			runExec("in " + test.into + " run give @s minecraft:stone 1")
			<-test.from.Exec(func(tx *world.Tx) {
				p, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("player missing from origin world")
				}
				pl := p.(*player.Player)
				if _, ok := pl.Effect(effect.Speed); !ok {
					t.Fatal("execute in effect did not apply speed to executor")
				}
				stack, _ := pl.Inventory().Item(0)
				if stack.Count() != 1 {
					t.Fatalf("execute in give inventory count = %d, want 1", stack.Count())
				}
			})
			runExec("in " + test.into + " run tp @s 300000 70 300000")
			if got := handle.World(); got != test.to {
				t.Fatalf("player world = %v, want %v", got, test.to)
			}
			<-test.to.Exec(func(tx *world.Tx) {
				p, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("player missing from destination world")
				}
				if p.Position() != pos {
					t.Fatalf("player position = %v, want %v", p.Position(), pos)
				}
			})
		})
	}
}
